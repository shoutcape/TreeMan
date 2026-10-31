// Package agentworktree records which worktrees and branches TreeMan created
// on behalf of a coding agent, such as Claude Code.
//
// An agent removes the worktrees it asked for without asking the user, so
// what it may remove is decided by this record rather than by names or paths:
// a branch that merely shares a name with one TreeMan created earlier is not
// TreeMan's to delete. `treeman delete` and `treeman clean` update the record
// for what they remove, so no stale entry can claim a later branch.
package agentworktree

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/shoutcape/treeman/internal/fsutil"
)

const (
	recordVersion = 1
	// stateDir is relative to the Git common directory.
	stateDir   = "treeman"
	recordFile = "agent-worktrees.json"
	lockFile   = "agent-worktrees.lock"
)

// Record is one worktree TreeMan created for an agent, or a branch it created
// whose worktree has since been removed.
type Record struct {
	// Agent names the agent the worktree was created for, such as "claude".
	Agent string `json:"agent"`
	// Name is the name the agent asked for. Asking again for the same name
	// returns the same worktree.
	Name   string `json:"name"`
	Branch string `json:"branch"`
	// BranchCreated says TreeMan created the branch, rather than attaching
	// one that already existed. Only a created branch may be deleted.
	BranchCreated bool `json:"branch_created"`
	// CreatedSHA and CreatedAt identify the branch TreeMan created. A branch
	// deleted and re-created under the same name has a different history,
	// which is how a stale record is told apart from a live one.
	CreatedSHA string    `json:"created_sha,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	// WorktreePath is empty once the worktree is removed and only the
	// branch is kept.
	WorktreePath string `json:"worktree_path,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
}

type document struct {
	Version int      `json:"version"`
	Records []Record `json:"records"`
}

// Store reads and updates the records of one repository.
type Store struct {
	dir string
}

// Open returns the store for the repository whose Git common directory is
// commonDir. Nothing is created until the first update.
func Open(commonDir string) *Store {
	return &Store{dir: filepath.Join(commonDir, stateDir)}
}

// Path is the file the records are kept in.
func (s *Store) Path() string {
	return filepath.Join(s.dir, recordFile)
}

// List returns every record. A store that was never written holds none.
func (s *Store) List() ([]Record, error) {
	data, err := os.ReadFile(s.Path())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read agent worktree records: %w", err)
	}
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("read agent worktree records %q: %w", s.Path(), err)
	}
	if doc.Version != recordVersion {
		return nil, fmt.Errorf("agent worktree records %q have unsupported version %d", s.Path(), doc.Version)
	}
	return doc.Records, nil
}

// Update replaces the records with what fn returns, holding the store's lock
// from the read to the write so concurrent updates are not lost. fn returning
// an error leaves the records unchanged.
func (s *Store) Update(fn func([]Record) ([]Record, error)) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create agent worktree state directory: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(s.dir, lockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open agent worktree record lock: %w", err)
	}
	defer lock.Close()
	return fsutil.WithFileLock(lock, func() error {
		records, err := s.List()
		if err != nil {
			return err
		}
		updated, err := fn(records)
		if err != nil {
			return err
		}
		if updated == nil {
			updated = []Record{}
		}
		data, err := json.MarshalIndent(document{Version: recordVersion, Records: updated}, "", "  ")
		if err != nil {
			return err
		}
		if err := fsutil.AtomicWriteFile(s.Path(), append(data, '\n'), 0o600); err != nil {
			return fmt.Errorf("write agent worktree records: %w", err)
		}
		return nil
	})
}

// Put stores record, replacing any record for the same agent and name.
func (s *Store) Put(record Record) error {
	return s.Update(func(records []Record) ([]Record, error) {
		kept := WithoutName(records, record.Agent, record.Name)
		return append(kept, record), nil
	})
}

// ForgetBranch drops every record for branch. Callers use it once the branch
// is deleted, so a later branch of the same name is never taken for this one.
func (s *Store) ForgetBranch(branch string) error {
	return s.Update(func(records []Record) ([]Record, error) {
		kept := records[:0:0]
		for _, record := range records {
			if record.Branch != branch {
				kept = append(kept, record)
			}
		}
		return kept, nil
	})
}

// ForgetWorktree records that the worktree at path is gone. The record is kept
// without a worktree while its branch exists: a branch TreeMan created can
// then still be recognized and cleaned up once it is merged, and asking for
// the name again re-attaches the branch.
func (s *Store) ForgetWorktree(path string, samePath func(a, b string) bool) error {
	return s.Update(func(records []Record) ([]Record, error) {
		kept := records[:0:0]
		for _, record := range records {
			if record.WorktreePath != "" && samePath(record.WorktreePath, path) {
				record.WorktreePath = ""
			}
			kept = append(kept, record)
		}
		return kept, nil
	})
}

// WithoutName returns records without the one for agent and name.
func WithoutName(records []Record, agent, name string) []Record {
	kept := records[:0:0]
	for _, record := range records {
		if record.Agent != agent || record.Name != name {
			kept = append(kept, record)
		}
	}
	return kept
}

// Find returns the record for agent and name.
func Find(records []Record, agent, name string) (Record, bool) {
	for _, record := range records {
		if record.Agent == agent && record.Name == name {
			return record, true
		}
	}
	return Record{}, false
}

// FindByPath returns the record whose worktree is at path.
func FindByPath(records []Record, path string, samePath func(a, b string) bool) (Record, bool) {
	for _, record := range records {
		if record.WorktreePath != "" && samePath(record.WorktreePath, path) {
			return record, true
		}
	}
	return Record{}, false
}
