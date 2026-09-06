package database

import (
	"fmt"
	"strings"

	"github.com/shoutcape/treeman/internal/fsutil"
	"github.com/shoutcape/treeman/internal/git"
)

func isPostgresURI(uri string) bool {
	lower := strings.ToLower(uri)
	return strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://")
}

// SetupOptions describes one database setup run.
type SetupOptions struct {
	WorktreePath        string
	Branch              string
	EnvKey              string
	ConfiguredContainer string
	// Rerun permits verifying and reusing an active record instead of
	// refusing it. Creation leaves it unset: a new worktree has nothing to
	// reuse, so an existing record there means something is wrong.
	Rerun bool
}

type SetupResult struct {
	DBName  string
	Skipped bool
	// Reused reports that an active database was verified and left alone.
	// Nothing was created, dropped, or rewritten.
	Reused bool
}

// Setup provisions or verifies the branch database for one worktree.
func Setup(opts SetupOptions) (SetupResult, error) {
	return setupDatabase(defaultBackend(), opts)
}

// SetupBranchDB provisions the branch database for a newly created worktree.
func SetupBranchDB(worktreePath, branch, envKey, configuredContainer string) (SetupResult, error) {
	return Setup(SetupOptions{
		WorktreePath:        worktreePath,
		Branch:              branch,
		EnvKey:              envKey,
		ConfiguredContainer: configuredContainer,
	})
}

func setupBranchDB(backend Backend, worktreePath, branch, envKey, configuredContainer string) (SetupResult, error) {
	return setupDatabase(backend, SetupOptions{
		WorktreePath:        worktreePath,
		Branch:              branch,
		EnvKey:              envKey,
		ConfiguredContainer: configuredContainer,
	})
}

func setupDatabase(backend Backend, opts SetupOptions) (SetupResult, error) {
	targetInput, err := loadSetupTarget(opts.WorktreePath, opts.EnvKey)
	if err != nil {
		return SetupResult{}, err
	}
	if targetInput.skipped {
		return SetupResult{Skipped: true}, nil
	}
	uri := targetInput.uri
	parsed := targetInput.parsed
	store, worktreeID, err := databaseStoreForWorktree(opts.WorktreePath)
	if err != nil {
		return SetupResult{}, err
	}
	record, err := setupOwnership(store, worktreeID, opts)
	if err != nil {
		return SetupResult{}, err
	}
	if record != nil {
		if err := matchesRecordedTarget(record, parsed, opts.ConfiguredContainer); err != nil {
			return SetupResult{}, err
		}
	}
	resolver, err := backend.Snapshot()
	if err != nil {
		return SetupResult{}, fmt.Errorf("listing PostgreSQL containers: %w", err)
	}
	// An active record returns here, before the provisioning block below.
	// That placement is the guarantee: the rollback drop further down cannot
	// reach a database that already holds the user's work.
	if record != nil && record.Status == databaseStatusActive {
		return reuseActiveDatabase(backend, resolver, record, opts.WorktreePath, parsed)
	}
	if record == nil {
		target, err := resolveTarget(resolver, parsed, opts.ConfiguredContainer)
		if err != nil {
			return SetupResult{}, err
		}
		candidate := &DatabaseRecord{WorktreeID: worktreeID, WorktreePath: opts.WorktreePath, Branch: opts.Branch, Database: BranchDBNameForRepository(parsed.Database, opts.Branch, store.repoID), Container: opts.ConfiguredContainer, ContainerID: target.ID, Host: parsed.Host, Port: parsed.Port, User: parsed.User, Status: databaseStatusSetupPending}
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
	if err := RewriteEnvValue(opts.WorktreePath, opts.EnvKey, newURI); err != nil {
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

// setupOwnership reads the ownership record and decides whether setup may
// proceed against it. Creation accepts only a pending retry. A rerun also
// accepts an active record, which the caller then verifies and reuses.
func setupOwnership(store *databaseStore, worktreeID string, opts SetupOptions) (*DatabaseRecord, error) {
	if !opts.Rerun {
		return store.setupRecord(worktreeID, opts.Branch)
	}
	record, err := store.ownership(worktreeID)
	if err != nil || record == nil {
		return record, err
	}
	if record.Branch != opts.Branch {
		return nil, fmt.Errorf("database ownership record for worktree %q names branch %q, not %q", record.WorktreePath, record.Branch, opts.Branch)
	}
	switch record.Status {
	case databaseStatusSetupPending, databaseStatusActive:
		return record, nil
	case databaseStatusPendingCleanup:
		// Never beginSetup from here: the record still owns its resource, and
		// the cleanup it is staged for has to finish first.
		return nil, fmt.Errorf("database %q is staged for cleanup; run treeman clean before setting it up again", record.Database)
	default:
		return nil, fmt.Errorf("database ownership record for worktree %q has unexpected status %q", record.WorktreePath, record.Status)
	}
}

// matchesRecordedTarget rejects a URI that no longer names the recorded
// connection. TreeMan owns one database, on one container, as one user; if any
// of those changed, the record no longer describes what the URI reaches.
func matchesRecordedTarget(record *DatabaseRecord, parsed ParsedURI, configuredContainer string) error {
	if parsed.Host == record.Host && parsed.Port == record.Port && parsed.User == record.User && configuredContainer == record.Container {
		return nil
	}
	return fmt.Errorf("database setup target changed since the recorded setup; restore host, port, user, and container configuration before retrying")
}

// reuseActiveDatabase verifies a record that is already active and returns
// without changing anything. Repair must not recreate a database the user has
// data in, and must not rewrite the line that reaches it.
func reuseActiveDatabase(backend Backend, resolver ContainerResolver, record *DatabaseRecord, worktreePath string, parsed ParsedURI) (SetupResult, error) {
	if err := matchesRecordedWorktree(record, worktreePath); err != nil {
		return SetupResult{}, err
	}
	// The recorded ID, never a lookup by name or port: a container that later
	// took the name is a different container with different data.
	target, err := resolver.ResolveID(record.ContainerID)
	if err != nil {
		return SetupResult{}, fmt.Errorf("finding recorded postgres container: %w", err)
	}
	exists, err := backend.Exists(target.ID, record.User, record.Database)
	if err != nil {
		return SetupResult{}, fmt.Errorf("verifying database %q: %w", record.Database, err)
	}
	if !exists {
		return SetupResult{}, fmt.Errorf("owned database %q is missing from container %q; TreeMan does not recreate an owned database, so restore it or delete the worktree to release the record", record.Database, target.Name)
	}
	if parsed.Database != record.Database {
		return SetupResult{}, fmt.Errorf("%s names database %q but TreeMan owns %q for this worktree; the environment file was left unchanged", EnvFileName, parsed.Database, record.Database)
	}
	return SetupResult{DBName: record.Database, Reused: true}, nil
}

// matchesRecordedWorktree confirms the record describes the directory being
// set up. Both sides are canonicalized, so a symlinked path is not drift.
func matchesRecordedWorktree(record *DatabaseRecord, worktreePath string) error {
	recorded, err := fsutil.CanonicalPath(record.WorktreePath)
	if err != nil {
		return fmt.Errorf("resolving recorded worktree path: %w", err)
	}
	current, err := fsutil.CanonicalPath(worktreePath)
	if err != nil {
		return fmt.Errorf("resolving worktree path: %w", err)
	}
	if recorded != current {
		return fmt.Errorf("database ownership record names worktree %q, not %q", record.WorktreePath, worktreePath)
	}
	return nil
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
