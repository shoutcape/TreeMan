package database

import "fmt"

// RefreshGuard reports the database name a refreshed environment file must
// keep.
type RefreshGuard struct {
	// Database is the name TreeMan owns for this worktree.
	Database string
	// Required reports that TreeMan owns a database here, so the environment
	// file is protected. A caller that could not establish the guard learns
	// from this that the file is protected rather than free.
	Required bool
}

// GuardRefresh decides whether the database-bearing environment file in
// worktreePath may be replaced by the one in sourcePath.
//
// It reads local state only, with no Docker call, so a refresh still works
// while the container is stopped. An error means the caller must preserve the
// file: TreeMan could not prove that replacing it would still reach the
// database it owns. On success with Required set, the caller restores
// Database into the copied file.
func GuardRefresh(worktreePath, sourcePath, branch, envKey, configuredContainer string) (RefreshGuard, error) {
	if envKey == "" {
		return RefreshGuard{}, nil
	}
	store, worktreeID, err := databaseStoreForWorktree(worktreePath)
	if err != nil {
		return RefreshGuard{}, err
	}
	record, err := store.ownership(worktreeID)
	if err != nil {
		return RefreshGuard{}, err
	}
	if record == nil {
		// Nothing is owned here, so the file carries no database TreeMan has
		// to preserve.
		return RefreshGuard{}, nil
	}

	guard := RefreshGuard{Database: record.Database, Required: true}
	if record.Branch != branch {
		return guard, fmt.Errorf("database ownership record for worktree %q names branch %q, not %q", record.WorktreePath, record.Branch, branch)
	}
	if err := matchesRecordedWorktree(record, worktreePath); err != nil {
		return guard, err
	}
	// Both sides are checked. The current file must not hide an edited target
	// behind the copy, and the replacement must reach the same server as the
	// record, or restoring the database name would point it somewhere else.
	if err := guardedTarget(worktreePath, envKey, "current", record, configuredContainer); err != nil {
		return guard, err
	}
	if err := guardedTarget(sourcePath, envKey, "replacement", record, configuredContainer); err != nil {
		return guard, err
	}
	return guard, nil
}

// guardedTarget checks one side of a refresh against the ownership record.
func guardedTarget(dir, envKey, role string, record *DatabaseRecord, configuredContainer string) error {
	target, err := loadSetupTarget(dir, envKey)
	if err != nil {
		return fmt.Errorf("reading %s %s: %w", role, EnvFileName, err)
	}
	if target.skipped {
		return fmt.Errorf("%s %s has no PostgreSQL %s, so restoring database %q would have nowhere to go", role, EnvFileName, envKey, record.Database)
	}
	if err := matchesRecordedTarget(record, target.parsed, configuredContainer); err != nil {
		return fmt.Errorf("%s %s: %w", role, EnvFileName, err)
	}
	return nil
}
