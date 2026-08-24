package database

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

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
	Version       int            `json:"version"`
	RepositoryID  string         `json:"repository_id"`
	WorktreeID    string         `json:"worktree_id"`
	WorktreePath  string         `json:"worktree_path"`
	Branch        string         `json:"branch"`
	Database      string         `json:"database"`
	Container     string         `json:"container,omitempty"`
	ContainerID   string         `json:"container_id"`
	ContainerName string         `json:"container_name"`
	Host          string         `json:"host"`
	Port          string         `json:"port"`
	User          string         `json:"user"`
	Status        databaseStatus `json:"status"`
	UpdatedAt     time.Time      `json:"updated_at"`
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
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("locking database state: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return operation()
}

func (s *databaseStore) repositoryID() (string, error) {
	path := filepath.Join(s.stateDir(), "repository-id")
	data, err := os.ReadFile(path)
	if err == nil {
		if id := string(data); len(id) == 32 {
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
	if err := writePrivateFile(path, []byte(id), 0o600); err != nil {
		return "", fmt.Errorf("writing database repository ID: %w", err)
	}
	return id, nil
}

func (s *databaseStore) load(worktreeID string) (*DatabaseRecord, error) {
	data, err := os.ReadFile(s.recordPath(worktreeID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading database ownership record: %w", err)
	}
	var record DatabaseRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("parsing database ownership record: %w", err)
	}
	if record.Version != 1 || record.RepositoryID != s.repoID || record.WorktreeID != worktreeID {
		return nil, fmt.Errorf("invalid database ownership record for worktree %q", worktreeID)
	}
	return &record, nil
}

func (s *databaseStore) save(record *DatabaseRecord) error {
	record.Version = 1
	record.RepositoryID = s.repoID
	record.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encoding database ownership record: %w", err)
	}
	if err := writePrivateFile(s.recordPath(record.WorktreeID), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing database ownership record: %w", err)
	}
	return nil
}

func (s *databaseStore) remove(worktreeID string) error {
	if err := os.Remove(s.recordPath(worktreeID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing database ownership record: %w", err)
	}
	return nil
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

func writePrivateFile(path string, data []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
