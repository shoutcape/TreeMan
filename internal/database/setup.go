package database

import (
	"fmt"
	"strings"
)

// Function variables for docker-dependent operations.
// Defaults point to real implementations; tests override them.
var (
	findPostgresContainerFn = FindPostgresContainer
	createDatabaseFn        = CreateDatabase
	dropDatabaseFn          = DropDatabase
)

// SetupResult holds the outcome of SetupBranchDB.
type SetupResult struct {
	// DBName is the branch-specific database name that was created.
	// Empty if no database setup was needed or possible.
	DBName string
	// Skipped is true if the env key was not found or not a postgres URI.
	Skipped bool
}

// SetupBranchDB reads the database URI from the .env in worktreePath using
// the given envKey, derives a branch-specific database name, creates it on
// the running Postgres container, and rewrites the .env with the new URI.
//
// If envKey is "", the function skips immediately (database management not
// configured).
//
// This is best-effort: missing .env, missing env key, non-postgres URIs,
// and missing Docker result in a skip or warning, not a hard failure. Callers
// should treat errors as non-fatal warnings.
func SetupBranchDB(worktreePath, branch, envKey string) (SetupResult, error) {
	if envKey == "" {
		return SetupResult{Skipped: true}, nil
	}

	uri, err := ReadDatabaseURI(worktreePath, envKey)
	if err != nil {
		return SetupResult{}, fmt.Errorf("reading %s: %w", envKey, err)
	}
	if uri == "" {
		return SetupResult{Skipped: true}, nil
	}

	// Only handle postgres URIs.
	lower := strings.ToLower(uri)
	if !strings.HasPrefix(lower, "postgres://") && !strings.HasPrefix(lower, "postgresql://") {
		return SetupResult{Skipped: true}, nil
	}

	parsed, err := ParseURI(uri)
	if err != nil {
		return SetupResult{}, fmt.Errorf("parsing %s: %w", envKey, err)
	}

	dbName := BranchDBName(parsed.Database, branch)

	// Find the running postgres container.
	container, err := findPostgresContainerFn(parsed.Port)
	if err != nil {
		return SetupResult{}, fmt.Errorf("finding postgres container: %w", err)
	}

	// Create the database.
	if err := createDatabaseFn(container, parsed.BaseURI, dbName); err != nil {
		return SetupResult{}, fmt.Errorf("creating database %q: %w", dbName, err)
	}

	// Rewrite the .env with the new URI.
	newURI, err := ReplaceDatabase(uri, dbName)
	if err != nil {
		return SetupResult{}, fmt.Errorf("building new URI: %w", err)
	}

	if err := RewriteDatabaseURI(worktreePath, envKey, newURI); err != nil {
		return SetupResult{}, fmt.Errorf("rewriting .env: %w", err)
	}

	return SetupResult{DBName: dbName}, nil
}

// CleanupPlan is an opaque, validated branch database target prepared before
// its worktree is removed.
type CleanupPlan struct {
	container string
	baseURI   string
	dbName    string
	valid     bool
}

// PrepareBranchDBCleanup reads the worktree environment and snapshots the
// database target. It returns nil when no eligible PostgreSQL database exists.
// It does not delete data.
func PrepareBranchDBCleanup(worktreePath, envKey string) (*CleanupPlan, error) {
	if envKey == "" {
		return nil, nil
	}

	uri, err := ReadDatabaseURI(worktreePath, envKey)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", envKey, err)
	}
	if uri == "" {
		return nil, nil
	}

	lower := strings.ToLower(uri)
	if !strings.HasPrefix(lower, "postgres://") && !strings.HasPrefix(lower, "postgresql://") {
		return nil, nil
	}

	parsed, err := ParseURI(uri)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", envKey, err)
	}

	// Safety: never drop a database that doesn't look like a branch database.
	if !strings.Contains(parsed.Database, "__") {
		return nil, nil
	}

	container, err := findPostgresContainerFn(parsed.Port)
	if err != nil {
		return nil, fmt.Errorf("finding postgres container: %w", err)
	}
	return &CleanupPlan{
		container: container,
		baseURI:   parsed.BaseURI,
		dbName:    parsed.Database,
		valid:     true,
	}, nil
}

// ExecuteBranchDBCleanup drops a database captured by PrepareBranchDBCleanup.
func ExecuteBranchDBCleanup(plan *CleanupPlan) error {
	if plan == nil {
		return nil
	}
	if !plan.valid {
		return fmt.Errorf("invalid database cleanup plan")
	}
	if err := dropDatabaseFn(plan.container, plan.baseURI, plan.dbName); err != nil {
		return fmt.Errorf("dropping database %q: %w", plan.dbName, err)
	}
	return nil
}
