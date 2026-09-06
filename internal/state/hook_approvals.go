package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/shoutcape/treeman/internal/fsutil"
	"github.com/shoutcape/treeman/internal/hooks"
)

const hookApprovalVersion = 1

// HookApproval is a persisted approval and its complete immutable scope.
type HookApproval struct {
	ID         string              `json:"id"`
	Scope      hooks.ApprovalScope `json:"scope"`
	ApprovedAt time.Time           `json:"approved_at"`
}

type hookApprovalDocument struct {
	Version   int            `json:"version"`
	Approvals []HookApproval `json:"approvals"`
}

// HookApprovalStore stores approvals in a private local state directory.
type HookApprovalStore struct {
	dir, path, lockPath string
}

// NewHookApprovalStore creates the private state location.
//
// Approvals outlive the repository they were given in and decide whether that
// repository's own configuration may run commands, so state inside it is
// rejected against the supplied Git common directory. An empty commonDir skips
// repository containment checks, allowing listing and revoking from anywhere.
// Directory and file validation, permissions, and locking apply in both cases.
// The constructor neither discovers a repository nor runs Git.
func NewHookApprovalStore(commonDir string) (*HookApprovalStore, error) {
	dir, err := stateDir()
	if err != nil {
		return nil, err
	}
	if err := rejectStateInsideRepository(dir, commonDir); err != nil {
		return nil, err
	}
	if err := checkApprovalDirectory(dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create approval state directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("protect approval state directory: %w", err)
	}
	store := &HookApprovalStore{
		dir:      dir,
		path:     filepath.Join(dir, "hook-approvals.json"),
		lockPath: filepath.Join(dir, "hook-approvals.lock"),
	}
	// Reject anything that is not a regular file up front, so a command that
	// would only have read state still refuses to run alongside it, and
	// nothing is created next to it first.
	for _, path := range []string{store.path, store.lockPath} {
		if err := checkApprovalFile(path); err != nil {
			return nil, err
		}
	}
	return store, nil
}

// rejectStateInsideRepository fails when dir is inside the supplied repository.
// An empty commonDir skips repository containment checks.
func rejectStateInsideRepository(dir, commonDir string) error {
	if commonDir == "" {
		return nil
	}
	protected := []string{commonDir}
	// A repository's Git directory sits at <root>/.git, so its parent is the
	// tree whose contents -- worktrees placed inside it included -- must not
	// hold the consent state that decides what they may run.
	if filepath.Base(commonDir) == ".git" {
		protected = append(protected, filepath.Dir(commonDir))
	}
	for _, path := range protected {
		canonical, err := fsutil.CanonicalPath(path)
		if err != nil {
			return fmt.Errorf("canonicalize repository path %q: %w", path, err)
		}
		if dir == canonical || fsutil.Contains(canonical, dir) {
			return fmt.Errorf("approval state directory %q is inside repository path %q", dir, path)
		}
	}
	return nil
}

func (s *HookApprovalStore) Lookup(scope hooks.ApprovalScope) (bool, error) {
	if err := scope.Validate(); err != nil {
		return false, err
	}
	var found bool
	err := s.withLock(false, func(doc *hookApprovalDocument) error {
		for _, approval := range doc.Approvals {
			if approval.ID == scope.ID() {
				found = true
				break
			}
		}
		return nil
	})
	return found, err
}

func (s *HookApprovalStore) Approve(scope hooks.ApprovalScope) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	return s.withLock(true, func(doc *hookApprovalDocument) error {
		id := scope.ID()
		for _, approval := range doc.Approvals {
			if approval.ID == id {
				return nil
			}
		}
		doc.Approvals = append(doc.Approvals, HookApproval{ID: id, Scope: scope, ApprovedAt: time.Now().UTC()})
		return nil
	})
}

func (s *HookApprovalStore) List() ([]HookApproval, error) {
	var result []HookApproval
	err := s.withLock(false, func(doc *hookApprovalDocument) error {
		result = append([]HookApproval(nil), doc.Approvals...)
		return nil
	})
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, err
}

func (s *HookApprovalStore) Revoke(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("approval ID is required")
	}
	return s.withLock(true, func(doc *hookApprovalDocument) error {
		for i, approval := range doc.Approvals {
			if approval.ID == id {
				doc.Approvals = append(doc.Approvals[:i], doc.Approvals[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("approval %q not found", id)
	})
}

// openGuarded revalidates the canonical directory path before each open as a
// best-effort check for observable symlink changes. This does not prevent races
// with replacement of the directory or its ancestors, or detect replacement
// with another ordinary directory at the same canonical path.
//
// O_NOFOLLOW protects the final file component, O_NONBLOCK keeps a FIFO from
// blocking, and regular-file validation uses the opened descriptor rather than
// a separate pathname check.
func (s *HookApprovalStore) openGuarded(path string, flag int) (*os.File, error) {
	if err := checkApprovalDirectory(s.dir); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, flag|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		return nil, fmt.Errorf("open approval state path %q: %w", path, err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("inspect approval state path %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("approval state path is not a regular file (symlinks are not allowed): %q", path)
	}
	return file, nil
}

// withLock runs fn against the stored document while holding the exclusive
// state lock. Both the lock file and the document are reached through
// openGuarded, so the document is revalidated after the wait for the lock
// rather than before it.
func (s *HookApprovalStore) withLock(persist bool, fn func(*hookApprovalDocument) error) error {
	lock, err := s.openGuarded(s.lockPath, os.O_CREATE|os.O_RDWR)
	if err != nil {
		return err
	}
	defer lock.Close()
	return fsutil.WithFileLock(lock, func() error {
		doc, err := s.read()
		if err != nil {
			return err
		}
		if err := fn(&doc); err != nil {
			return err
		}
		if persist {
			return s.write(doc)
		}
		return nil
	})
}

func (s *HookApprovalStore) read() (hookApprovalDocument, error) {
	file, err := s.openGuarded(s.path, os.O_RDONLY)
	if errors.Is(err, fs.ErrNotExist) {
		return hookApprovalDocument{Version: hookApprovalVersion}, nil
	}
	if err != nil {
		return hookApprovalDocument{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return hookApprovalDocument{}, fmt.Errorf("read approvals: %w", err)
	}
	var doc hookApprovalDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return doc, fmt.Errorf("decode approvals: %w", err)
	}
	if doc.Version != hookApprovalVersion {
		return doc, fmt.Errorf("unsupported approval state version %d", doc.Version)
	}
	seen := make(map[string]bool, len(doc.Approvals))
	for _, approval := range doc.Approvals {
		if approval.ID == "" || seen[approval.ID] || approval.ID != approval.Scope.ID() {
			return doc, errors.New("approval state contains an inconsistent record")
		}
		if err := approval.Scope.Validate(); err != nil {
			return doc, fmt.Errorf("invalid approval scope: %w", err)
		}
		if approval.ApprovedAt.IsZero() {
			return doc, errors.New("approval state contains a missing timestamp")
		}
		seen[approval.ID] = true
	}
	return doc, nil
}

func (s *HookApprovalStore) write(doc hookApprovalDocument) error {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode approvals: %w", err)
	}
	if err := fsutil.AtomicWriteFile(s.path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("durably write approvals: %w", err)
	}
	return nil
}

func checkApprovalDirectory(path string) error {
	canonical, err := fsutil.CanonicalPath(path)
	if err != nil {
		return fmt.Errorf("inspect approval state directory: %w", err)
	}
	if canonical != path {
		return fmt.Errorf("approval state directory changed or is a symlink: %q", path)
	}
	info, err := os.Lstat(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect approval state directory: %w", err)
	}
	if err == nil && !info.IsDir() {
		return fmt.Errorf("approval state path is not a directory: %q", path)
	}
	return nil
}

func checkApprovalFile(path string) error {
	info, err := os.Lstat(path)
	if err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("approval state path is not a regular file (symlinks are not allowed): %q", path)
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect approval state path %q: %w", path, err)
	}
	return nil
}
