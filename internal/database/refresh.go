package database

import (
	"bytes"
	"fmt"
)

// PrepareRefresh validates local ownership and returns the complete replacement
// contents, with any owned database name already substituted. The caller must
// publish these bytes rather than reread the source. No files or resources are
// changed, and no Docker call is needed while the container is stopped.
func PrepareRefresh(worktreePath, branch, envKey, configuredContainer string, source []byte) ([]byte, string, error) {
	_, _, record, err := readOnlyDatabaseOwnership(worktreePath)
	if err != nil {
		return nil, "", err
	}
	if record == nil {
		return source, "", nil
	}
	if record.Branch != branch {
		return nil, "", fmt.Errorf("database ownership record for worktree %q names branch %q, not %q", record.WorktreePath, record.Branch, branch)
	}
	if err := matchesRecordedWorktree(record, worktreePath); err != nil {
		return nil, "", err
	}
	// The current target must not hide drift behind the refresh. Validate the
	// replacement from the same snapshot that will be rewritten and published.
	current, err := loadSetupTarget(worktreePath, envKey)
	if err != nil {
		return nil, "", fmt.Errorf("reading current %s: %w", EnvFileName, err)
	}
	if err := guardedTarget(current, envKey, "current", record, configuredContainer); err != nil {
		return nil, "", err
	}
	uri, err := readEnvValue(bytes.NewReader(source), envKey)
	if err != nil {
		return nil, "", fmt.Errorf("reading replacement %s: %w", EnvFileName, err)
	}
	replacement, err := parseSetupTarget(uri, envKey)
	if err != nil {
		return nil, "", fmt.Errorf("reading replacement %s: %w", EnvFileName, err)
	}
	if err := guardedTarget(replacement, envKey, "replacement", record, configuredContainer); err != nil {
		return nil, "", err
	}
	ownedURI, err := ReplaceDatabase(uri, record.Database)
	if err != nil {
		return nil, "", err
	}
	prepared, err := rewriteEnvValue(source, envKey, ownedURI)
	if err != nil {
		return nil, "", err
	}
	return prepared, record.Database, nil
}

func guardedTarget(target setupTarget, envKey, role string, record *DatabaseRecord, configuredContainer string) error {
	if target.skipped {
		return fmt.Errorf("%s %s has no PostgreSQL %s, so preserving database %q would have nowhere to go", role, EnvFileName, envKey, record.Database)
	}
	if err := matchesRecordedTarget(record, target.parsed, configuredContainer); err != nil {
		return fmt.Errorf("%s %s: %w", role, EnvFileName, err)
	}
	return nil
}
