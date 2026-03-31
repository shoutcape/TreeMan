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

// CleanupBranchDB reads the database URI from the .env in worktreePath using
// the given envKey, identifies the branch database, and drops it from the
// running Postgres container.
//
// If envKey is "", the function returns nil (database management not configured).
//
// Safety: only databases containing "__" (the branch separator) are eligible
// for auto-drop. This prevents accidentally dropping the main database if
// someone manually edited their .env.
//
// Best-effort: callers should treat errors as non-fatal warnings.
func CleanupBranchDB(worktreePath, envKey string) error {
	if envKey == "" {
		return nil
	}

	uri, err := ReadDatabaseURI(worktreePath, envKey)
	if err != nil {
		return fmt.Errorf("reading %s: %w", envKey, err)
	}
	if uri == "" {
		return nil // No database URI, nothing to clean up.
	}

	lower := strings.ToLower(uri)
	if !strings.HasPrefix(lower, "postgres://") && !strings.HasPrefix(lower, "postgresql://") {
		return nil
	}

	parsed, err := ParseURI(uri)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", envKey, err)
	}

	// Safety: never drop a database that doesn't look like a branch database.
	if !strings.Contains(parsed.Database, "__") {
		return nil
	}

	container, err := findPostgresContainerFn(parsed.Port)
	if err != nil {
		return fmt.Errorf("finding postgres container: %w", err)
	}

	if err := dropDatabaseFn(container, parsed.BaseURI, parsed.Database); err != nil {
		return fmt.Errorf("dropping database %q: %w", parsed.Database, err)
	}

	return nil
}
