package database

import "fmt"

// GuardRefresh decides whether the database-bearing environment file in
// worktreePath may be replaced by the one in sourcePath.
//
// It reads local state only, with no Docker call, so a refresh still works
// while the container is stopped. An error means the caller must neither create
// nor replace the file. On success, a non-empty name must be restored into the
// copied file. An empty name means no database is owned here.
func GuardRefresh(worktreePath, sourcePath, branch, envKey, configuredContainer string) (string, error) {
	if envKey == "" {
		return "", nil
	}
	store, worktreeID, err := databaseStoreForWorktree(worktreePath)
	if err != nil {
		return "", err
	}
	record, err := store.ownership(worktreeID)
	if err != nil {
		return "", err
	}
	if record == nil {
		// Nothing is owned here, so the file carries no database TreeMan has
		// to preserve.
		return "", nil
	}

	if record.Branch != branch {
		return "", fmt.Errorf("database ownership record for worktree %q names branch %q, not %q", record.WorktreePath, record.Branch, branch)
	}
	if err := matchesRecordedWorktree(record, worktreePath); err != nil {
		return "", err
	}
	// Both sides are checked. The current file must not hide an edited target
	// behind the copy, and the replacement must reach the same server as the
	// record, or restoring the database name would point it somewhere else.
	if err := guardedTarget(worktreePath, envKey, "current", record, configuredContainer); err != nil {
		return "", err
	}
	if err := guardedTarget(sourcePath, envKey, "replacement", record, configuredContainer); err != nil {
		return "", err
	}
	return record.Database, nil
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
