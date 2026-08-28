package database

import (
	"fmt"
	"strings"

	"github.com/shoutcape/treeman/internal/git"
)

func isPostgresURI(uri string) bool {
	lower := strings.ToLower(uri)
	return strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://")
}

type SetupResult struct {
	DBName  string
	Skipped bool
}

func SetupBranchDB(worktreePath, branch, envKey, configuredContainer string) (SetupResult, error) {
	return setupBranchDB(defaultBackend(), worktreePath, branch, envKey, configuredContainer)
}

func setupBranchDB(backend Backend, worktreePath, branch, envKey, configuredContainer string) (SetupResult, error) {
	targetInput, err := loadSetupTarget(worktreePath, envKey)
	if err != nil {
		return SetupResult{}, err
	}
	if targetInput.skipped {
		return SetupResult{Skipped: true}, nil
	}
	uri := targetInput.uri
	parsed := targetInput.parsed
	store, worktreeID, err := databaseStoreForWorktree(worktreePath)
	if err != nil {
		return SetupResult{}, err
	}
	record, err := store.setupRecord(worktreeID, branch)
	if err != nil {
		return SetupResult{}, err
	}
	if record != nil && (parsed.Host != record.Host || parsed.Port != record.Port || parsed.User != record.User || configuredContainer != record.Container) {
		return SetupResult{}, fmt.Errorf("database setup target changed since the pending setup; restore host, port, user, and container configuration before retrying")
	}
	resolver, err := backend.Snapshot()
	if err != nil {
		return SetupResult{}, fmt.Errorf("listing PostgreSQL containers: %w", err)
	}
	if record == nil {
		target, err := resolveTarget(resolver, parsed, configuredContainer)
		if err != nil {
			return SetupResult{}, err
		}
		candidate := &DatabaseRecord{WorktreeID: worktreeID, WorktreePath: worktreePath, Branch: branch, Database: BranchDBNameForRepository(parsed.Database, branch, store.repoID), Container: configuredContainer, ContainerID: target.ID, Host: parsed.Host, Port: parsed.Port, User: parsed.User, Status: databaseStatusSetupPending}
		record, err = store.beginSetup(candidate)
		if err != nil {
			return SetupResult{}, err
		}
	}
	target, err := resolver.ResolveID(record.ContainerID)
	if err != nil {
		return SetupResult{}, fmt.Errorf("finding recorded postgres container: %w", err)
	}
	if err := backend.Create(target.ID, record.User, record.Database); err != nil {
		return SetupResult{}, fmt.Errorf("creating database %q: %w", record.Database, err)
	}
	newURI, err := ReplaceDatabase(uri, record.Database)
	if err != nil {
		return SetupResult{}, fmt.Errorf("building new URI: %w", err)
	}
	if err := RewriteEnvValue(worktreePath, envKey, newURI); err != nil {
		if dropErr := backend.Drop(target.ID, record.User, []string{record.Database}); dropErr != nil {
			return SetupResult{}, fmt.Errorf("rewriting .env: %w; rolling back database %q: %v", err, record.Database, dropErr)
		}
		return SetupResult{}, fmt.Errorf("rewriting .env: %w", err)
	}
	if err := store.activateSetup(worktreeID, *record); err != nil {
		return SetupResult{}, err
	}
	return SetupResult{DBName: record.Database}, nil
}

func databaseStoreForWorktree(worktreePath string) (*databaseStore, string, error) {
	store, err := newDatabaseStore(worktreePath)
	if err != nil {
		return nil, "", fmt.Errorf("opening database ownership state: %w", err)
	}
	worktreeID, err := git.WorktreeID(worktreePath)
	if err != nil {
		return nil, "", fmt.Errorf("identifying linked worktree: %w", err)
	}
	return store, worktreeID, nil
}
