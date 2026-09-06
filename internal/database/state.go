package database

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/shoutcape/treeman/internal/fsutil"
	"github.com/shoutcape/treeman/internal/git"
)

const databaseStateDirectory = "treeman/databases"

type databaseStatus string

const (
	databaseStatusSetupPending   databaseStatus = "setup_pending"
	databaseStatusActive         databaseStatus = "active"
	databaseStatusPendingCleanup databaseStatus = "pending_cleanup"
)

// DatabaseRecord is TreeMan's durable ownership record for one branch database.
// It intentionally excludes connection URIs and passwords.
type DatabaseRecord struct {
	Version      int            `json:"version"`
	RepositoryID string         `json:"repository_id"`
	WorktreeID   string         `json:"worktree_id"`
	WorktreePath string         `json:"worktree_path"`
	Branch       string         `json:"branch"`
	Database     string         `json:"database"`
	Container    string         `json:"container,omitempty"`
	ContainerID  string         `json:"container_id"`
	Host         string         `json:"host"`
	Port         string         `json:"port"`
	User         string         `json:"user"`
	Status       databaseStatus `json:"status"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type databaseStore struct {
	commonDir string
	repoID    string
}

func newDatabaseStore(dir string) (*databaseStore, error) {
	commonDir, err := git.CommonDir(dir)
	if err != nil {
		return nil, err
	}
	store := &databaseStore{commonDir: commonDir}
	if err := store.withLock(func() error {
		repoID, err := store.repositoryID()
		if err != nil {
			return err
		}
		store.repoID = repoID
		return nil
	}); err != nil {
		return nil, err
	}
	return store, nil
}

// LookupDatabaseName returns the recorded database for a linked worktree
// without creating or modifying ownership state.
func LookupDatabaseName(worktreePath string) (string, bool, error) {
	_, _, record, err := readOnlyDatabaseOwnership(worktreePath)
	if err != nil {
		return "", false, err
	}
	if record == nil {
		return "", false, nil
	}
	return record.Database, true, nil
}

// readOnlyDatabaseOwnership discovers an existing record without creating the
// state directory or repository ID. An absent record is not ownership.
func readOnlyDatabaseOwnership(worktreePath string) (*databaseStore, string, *DatabaseRecord, error) {
	commonDir, err := git.CommonDir(worktreePath)
	if err != nil {
		return nil, "", nil, fmt.Errorf("opening database ownership state: %w", err)
	}
	worktreeID, err := git.WorktreeID(worktreePath)
	if err != nil {
		return nil, "", nil, fmt.Errorf("identifying linked worktree: %w", err)
	}
	store := &databaseStore{commonDir: commonDir}
	if _, err := os.Lstat(store.recordPath(worktreeID)); os.IsNotExist(err) {
		return store, worktreeID, nil, nil
	} else if err != nil {
		return nil, "", nil, fmt.Errorf("reading database ownership record: %w", err)
	}

	repositoryID, err := readRepositoryID(store.stateDir())
	if err != nil {
		return nil, "", nil, err
	}
	store.repoID = repositoryID
	record, err := store.load(worktreeID)
	if err != nil {
		return nil, "", nil, err
	}
	return store, worktreeID, record, nil
}

func (s *databaseStore) stateDir() string {
	return filepath.Join(s.commonDir, databaseStateDirectory)
}

func (s *databaseStore) recordPath(worktreeID string) string {
	return filepath.Join(s.stateDir(), worktreeID+".json")
}

func (s *databaseStore) withLock(operation func() error) error {
	if err := os.MkdirAll(s.stateDir(), 0o700); err != nil {
		return fmt.Errorf("creating database state directory: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(s.stateDir(), ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("opening database state lock: %w", err)
	}
	defer lock.Close()
	return fsutil.WithFileLock(lock, operation)
}

func (s *databaseStore) repositoryID() (string, error) {
	path := filepath.Join(s.stateDir(), "repository-id")
	data, err := os.ReadFile(path)
	if err == nil {
		if id := string(data); validRepositoryID(id) {
			return id, nil
		}
		return "", fmt.Errorf("invalid database repository ID in %s", path)
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("reading database repository ID: %w", err)
	}
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generating database repository ID: %w", err)
	}
	id := hex.EncodeToString(bytes)
	if err := fsutil.AtomicWriteFile(path, []byte(id), 0o600); err != nil {
		return "", fmt.Errorf("writing database repository ID: %w", err)
	}
	return id, nil
}

func readRepositoryID(stateDir string) (string, error) {
	path := filepath.Join(stateDir, "repository-id")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading database repository ID: %w", err)
	}
	id := string(data)
	if !validRepositoryID(id) {
		return "", fmt.Errorf("invalid database repository ID in %s", path)
	}
	return id, nil
}

func validRepositoryID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func (s *databaseStore) load(worktreeID string) (*DatabaseRecord, error) {
	if !validWorktreeID(worktreeID) {
		return nil, fmt.Errorf("invalid database ownership record ID %q", worktreeID)
	}
	path := s.recordPath(worktreeID)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading database ownership record: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("database ownership record is not a regular file: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading database ownership record: %w", err)
	}
	var record DatabaseRecord
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return nil, fmt.Errorf("parsing database ownership record: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("invalid trailing data in database ownership record for worktree %q", worktreeID)
	}
	if err := validateRecord(record, s.repoID, worktreeID); err != nil {
		return nil, fmt.Errorf("invalid database ownership record for worktree %q: %w", worktreeID, err)
	}
	return &record, nil
}

func validWorktreeID(id string) bool {
	return id != "" && id != "." && id != ".." && filepath.Base(id) == id
}

func validateRecord(record DatabaseRecord, repositoryID, worktreeID string) error {
	if record.Version != 1 {
		return fmt.Errorf("unsupported version")
	}
	if record.RepositoryID != repositoryID {
		return fmt.Errorf("repository ID mismatch")
	}
	if record.WorktreeID != worktreeID || !validWorktreeID(worktreeID) {
		return fmt.Errorf("worktree ID mismatch")
	}
	if record.WorktreePath == "" || !filepath.IsAbs(record.WorktreePath) || filepath.Clean(record.WorktreePath) != record.WorktreePath {
		return fmt.Errorf("invalid worktree path")
	}
	if record.Branch == "" || record.Database == "" || record.ContainerID == "" || record.User == "" || record.Port == "" {
		return fmt.Errorf("required field is empty")
	}
	switch record.Status {
	case databaseStatusSetupPending, databaseStatusActive, databaseStatusPendingCleanup:
	default:
		return fmt.Errorf("illegal status %q", record.Status)
	}
	if record.UpdatedAt.IsZero() {
		return fmt.Errorf("missing update timestamp")
	}
	return nil
}

func (s *databaseStore) save(record *DatabaseRecord) error {
	record.Version = 1
	record.RepositoryID = s.repoID
	record.UpdatedAt = time.Now().UTC()
	if err := validateRecord(*record, s.repoID, record.WorktreeID); err != nil {
		return fmt.Errorf("refusing to write invalid database ownership record: %w", err)
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encoding database ownership record: %w", err)
	}
	if err := fsutil.AtomicWriteFile(s.recordPath(record.WorktreeID), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing database ownership record: %w", err)
	}
	return nil
}

func (s *databaseStore) remove(worktreeID string) error {
	if err := os.Remove(s.recordPath(worktreeID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing database ownership record: %w", err)
	}
	return fsutil.SyncDirectory(s.stateDir())
}

func (s *databaseStore) list() ([]DatabaseRecord, error) {
	entries, err := os.ReadDir(s.stateDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading database ownership records: %w", err)
	}
	records := make([]DatabaseRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := entry.Name()[:len(entry.Name())-len(".json")]
		record, err := s.load(id)
		if err != nil {
			return nil, err
		}
		if record != nil {
			records = append(records, *record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].WorktreeID < records[j].WorktreeID })
	return records, nil
}

func (s *databaseStore) records() ([]DatabaseRecord, error) {
	var records []DatabaseRecord
	err := s.withLock(func() error {
		var err error
		records, err = s.list()
		return err
	})
	return records, err
}

// ownership returns the record on disk without judging its status. Setup
// decides what each state permits; the store only reads.
func (s *databaseStore) ownership(worktreeID string) (*DatabaseRecord, error) {
	var result *DatabaseRecord
	err := s.withLock(func() error {
		record, err := s.load(worktreeID)
		result = record
		return err
	})
	return result, err
}

// beginSetup records a newly chosen database target. A physical database is
// owned by at most one record, even when callers race under different worktree
// IDs. Repeating the same pending setup returns its existing record.
func (s *databaseStore) beginSetup(candidate *DatabaseRecord) (*DatabaseRecord, error) {
	var result *DatabaseRecord
	err := s.withLock(func() error {
		existing, err := s.load(candidate.WorktreeID)
		if err != nil {
			return err
		}
		if existing != nil {
			if existing.Branch != candidate.Branch || existing.Status != databaseStatusSetupPending || !sameSetupTarget(existing, candidate) {
				return fmt.Errorf("database ownership record already exists for worktree %q", candidate.WorktreePath)
			}
			result = existing
			return nil
		}
		records, err := s.list()
		if err != nil {
			return err
		}
		for _, record := range records {
			if sameDatabaseResource(&record, candidate) {
				return fmt.Errorf("database %q is already owned by worktree %q", candidate.Database, record.WorktreePath)
			}
		}
		if err := s.save(candidate); err != nil {
			return err
		}
		result = candidate
		return nil
	})
	return result, err
}

func (s *databaseStore) activateSetup(worktreeID string, expected DatabaseRecord) error {
	return s.withLock(func() error {
		record, err := s.load(worktreeID)
		if err != nil {
			return err
		}
		if record == nil || record.Status != databaseStatusSetupPending || !sameDatabaseResource(record, &expected) {
			return fmt.Errorf("database ownership record changed during setup")
		}
		record.Status = databaseStatusActive
		return s.save(record)
	})
}

// cleanupRecord returns an authorized record before worktree deletion.
func (s *databaseStore) cleanupRecord(worktreeID string) (*DatabaseRecord, error) {
	var result *DatabaseRecord
	err := s.withLock(func() error {
		record, err := s.load(worktreeID)
		if err != nil || record == nil {
			result = record
			return err
		}
		if record.Status != databaseStatusActive && record.Status != databaseStatusSetupPending {
			return nil
		}
		result = record
		return nil
	})
	return result, err
}

// markPendingCleanup advances an owned record to the durable cleanup state.
func (s *databaseStore) markPendingCleanup(worktreeID string, expected DatabaseRecord) (*DatabaseRecord, error) {
	var result *DatabaseRecord
	err := s.withLock(func() error {
		record, err := s.load(worktreeID)
		if err != nil {
			return err
		}
		if record == nil || !sameDatabaseResource(record, &expected) || (record.Status != databaseStatusActive && record.Status != databaseStatusSetupPending) {
			return fmt.Errorf("database ownership record changed before cleanup")
		}
		record.Status = databaseStatusPendingCleanup
		if err := s.save(record); err != nil {
			return err
		}
		result = record
		return nil
	})
	return result, err
}

func (s *databaseStore) pendingCleanupRecord(worktreeID string, expected DatabaseRecord) (*DatabaseRecord, error) {
	var result *DatabaseRecord
	err := s.withLock(func() error {
		record, err := s.load(worktreeID)
		if err != nil {
			return err
		}
		if record == nil || record.Status != databaseStatusPendingCleanup || !sameDatabaseResource(record, &expected) {
			return fmt.Errorf("database ownership record changed before cleanup")
		}
		result = record
		return nil
	})
	return result, err
}

func (s *databaseStore) removePendingCleanup(worktreeID string, expected DatabaseRecord) error {
	return s.withLock(func() error {
		record, err := s.load(worktreeID)
		if err != nil {
			return err
		}
		if record == nil || record.Status != databaseStatusPendingCleanup || !sameDatabaseResource(record, &expected) {
			return fmt.Errorf("database ownership record changed during cleanup")
		}
		return s.remove(worktreeID)
	})
}

func sameDatabaseResource(a, b *DatabaseRecord) bool {
	return a.Database == b.Database && a.ContainerID == b.ContainerID
}

func sameSetupTarget(a, b *DatabaseRecord) bool {
	return sameDatabaseResource(a, b) && a.Host == b.Host && a.Port == b.Port && a.User == b.User && a.Container == b.Container
}
