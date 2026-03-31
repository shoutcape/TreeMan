package database

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubDocker replaces the docker-dependent function variables with test stubs
// and returns a cleanup function that restores the originals. The stubs record
// calls and return configurable results.
type dockerStubs struct {
	// FindContainer controls what FindPostgresContainer returns.
	FindContainer    string
	FindContainerErr error

	// CreateDB controls what CreateDatabase returns.
	CreateDBErr error

	// DropDB controls what DropDatabase returns.
	DropDBErr error

	// Recorded calls for assertions.
	FindCalls  []string // port arguments
	CreateArgs []createCall
	DropArgs   []dropCall
}

type createCall struct {
	Container string
	BaseURI   string
	DBName    string
}

type dropCall struct {
	Container string
	BaseURI   string
	DBName    string
}

func (s *dockerStubs) install(t *testing.T) {
	t.Helper()
	origFind := findPostgresContainerFn
	origCreate := createDatabaseFn
	origDrop := dropDatabaseFn

	findPostgresContainerFn = func(port string) (string, error) {
		s.FindCalls = append(s.FindCalls, port)
		return s.FindContainer, s.FindContainerErr
	}
	createDatabaseFn = func(container, baseURI, dbName string) error {
		s.CreateArgs = append(s.CreateArgs, createCall{container, baseURI, dbName})
		return s.CreateDBErr
	}
	dropDatabaseFn = func(container, baseURI, dbName string) error {
		s.DropArgs = append(s.DropArgs, dropCall{container, baseURI, dbName})
		return s.DropDBErr
	}

	t.Cleanup(func() {
		findPostgresContainerFn = origFind
		createDatabaseFn = origCreate
		dropDatabaseFn = origDrop
	})
}

// writeEnv is a test helper that creates a .env file in dir.
func writeEnv(t *testing.T, dir, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0600))
}

// --- SetupBranchDB tests ---

func TestSetupBranchDB_EmptyEnvKey(t *testing.T) {
	result, err := SetupBranchDB("/any/path", "feature/branch", "")
	require.NoError(t, err)
	assert.True(t, result.Skipped)
	assert.Equal(t, "", result.DBName)
}

func TestSetupBranchDB_NoEnvFile(t *testing.T) {
	dir := t.TempDir()
	// No .env file -- ReadDatabaseURI returns "" with no error.
	result, err := SetupBranchDB(dir, "feature/branch", "DATABASE_URI")
	require.NoError(t, err)
	assert.True(t, result.Skipped)
}

func TestSetupBranchDB_EnvKeyNotInFile(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "OTHER_VAR=some_value\n")

	result, err := SetupBranchDB(dir, "feature/branch", "DATABASE_URI")
	require.NoError(t, err)
	assert.True(t, result.Skipped)
}

func TestSetupBranchDB_NonPostgresURI(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "DATABASE_URI=mysql://user:pass@host:3306/mydb\n")

	result, err := SetupBranchDB(dir, "feature/branch", "DATABASE_URI")
	require.NoError(t, err)
	assert.True(t, result.Skipped)
}

func TestSetupBranchDB_InvalidPostgresURI(t *testing.T) {
	dir := t.TempDir()
	// postgres:// scheme but no database in path.
	writeEnv(t, dir, "DATABASE_URI=postgres://user:pass@host:5432\n")

	_, err := SetupBranchDB(dir, "feature/branch", "DATABASE_URI")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing DATABASE_URI")
}

func TestSetupBranchDB_FindContainerFails(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "DATABASE_URI=postgres://postgres:postgres@127.0.0.1:5432/myapp\n")

	stubs := &dockerStubs{
		FindContainerErr: fmt.Errorf("docker not available"),
	}
	stubs.install(t)

	_, err := SetupBranchDB(dir, "feat/add-users", "DATABASE_URI")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "finding postgres container")
	assert.Contains(t, err.Error(), "docker not available")

	// Should have tried to find a container with the port from the URI.
	require.Len(t, stubs.FindCalls, 1)
	assert.Equal(t, "5432", stubs.FindCalls[0])
}

func TestSetupBranchDB_CreateDatabaseFails(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "DATABASE_URI=postgres://postgres:postgres@127.0.0.1:5432/myapp\n")

	stubs := &dockerStubs{
		FindContainer: "my-postgres-1",
		CreateDBErr:   fmt.Errorf("permission denied"),
	}
	stubs.install(t)

	_, err := SetupBranchDB(dir, "feat/add-users", "DATABASE_URI")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating database")
	assert.Contains(t, err.Error(), "permission denied")

	// Should have called create with the correct args.
	require.Len(t, stubs.CreateArgs, 1)
	assert.Equal(t, "my-postgres-1", stubs.CreateArgs[0].Container)
	assert.Equal(t, "myapp__feat_add_users", stubs.CreateArgs[0].DBName)
}

func TestSetupBranchDB_Success(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "NODE_ENV=development\nDATABASE_URI=postgres://postgres:postgres@127.0.0.1:5432/myapp\nSECRET=abc\n")

	stubs := &dockerStubs{
		FindContainer: "my-postgres-1",
	}
	stubs.install(t)

	result, err := SetupBranchDB(dir, "jd/fix-123/add-user-auth", "DATABASE_URI")
	require.NoError(t, err)
	assert.False(t, result.Skipped)
	assert.Equal(t, "myapp__jd_fix_123_add_user_auth", result.DBName)

	// Verify docker interactions.
	require.Len(t, stubs.FindCalls, 1)
	assert.Equal(t, "5432", stubs.FindCalls[0])

	require.Len(t, stubs.CreateArgs, 1)
	assert.Equal(t, "my-postgres-1", stubs.CreateArgs[0].Container)
	assert.Equal(t, "postgres://postgres:postgres@127.0.0.1:5432", stubs.CreateArgs[0].BaseURI)
	assert.Equal(t, "myapp__jd_fix_123_add_user_auth", stubs.CreateArgs[0].DBName)

	// Verify .env was rewritten with the branch-specific database.
	got, err := ReadEnvValue(dir, "DATABASE_URI")
	require.NoError(t, err)
	assert.Equal(t, "postgres://postgres:postgres@127.0.0.1:5432/myapp__jd_fix_123_add_user_auth", got)

	// Verify other env vars are untouched.
	nodeEnv, err := ReadEnvValue(dir, "NODE_ENV")
	require.NoError(t, err)
	assert.Equal(t, "development", nodeEnv)
}

func TestSetupBranchDB_SuccessWithQueryParams(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "DATABASE_URI=postgres://user:pass@host:5432/mydb?sslmode=verify-full\n")

	stubs := &dockerStubs{
		FindContainer: "pg-1",
	}
	stubs.install(t)

	result, err := SetupBranchDB(dir, "hotfix", "DATABASE_URI")
	require.NoError(t, err)
	assert.Equal(t, "mydb__hotfix", result.DBName)

	// Verify query params are preserved in the rewritten URI.
	got, err := ReadEnvValue(dir, "DATABASE_URI")
	require.NoError(t, err)
	assert.Equal(t, "postgres://user:pass@host:5432/mydb__hotfix?sslmode=verify-full", got)
}

func TestSetupBranchDB_PostgresqlScheme(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "DATABASE_URL=postgresql://user:pass@localhost:5432/testdb\n")

	stubs := &dockerStubs{
		FindContainer: "pg-1",
	}
	stubs.install(t)

	result, err := SetupBranchDB(dir, "feat/v2.0-support", "DATABASE_URL")
	require.NoError(t, err)
	assert.False(t, result.Skipped)
	assert.Equal(t, "testdb__feat_v2_0_support", result.DBName)
}

func TestSetupBranchDB_QuotedEnvValue(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "DATABASE_URI=\"postgres://postgres:postgres@127.0.0.1:5432/myapp\"\n")

	stubs := &dockerStubs{
		FindContainer: "pg-1",
	}
	stubs.install(t)

	result, err := SetupBranchDB(dir, "fix/bug", "DATABASE_URI")
	require.NoError(t, err)
	assert.Equal(t, "myapp__fix_bug", result.DBName)
}

// --- CleanupBranchDB tests ---

func TestCleanupBranchDB_EmptyEnvKey(t *testing.T) {
	err := CleanupBranchDB("/any/path", "")
	assert.NoError(t, err)
}

func TestCleanupBranchDB_NoEnvFile(t *testing.T) {
	dir := t.TempDir()
	err := CleanupBranchDB(dir, "DATABASE_URI")
	assert.NoError(t, err)
}

func TestCleanupBranchDB_EnvKeyNotInFile(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "OTHER_VAR=some_value\n")

	err := CleanupBranchDB(dir, "DATABASE_URI")
	assert.NoError(t, err)
}

func TestCleanupBranchDB_NonPostgresURI(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "DATABASE_URI=mysql://user:pass@host:3306/mydb\n")

	err := CleanupBranchDB(dir, "DATABASE_URI")
	assert.NoError(t, err)
}

func TestCleanupBranchDB_SafetyRefusesMainDB(t *testing.T) {
	dir := t.TempDir()
	// URI points to "myapp" (no "__" separator) -- should NOT be dropped.
	writeEnv(t, dir, "DATABASE_URI=postgres://postgres:postgres@127.0.0.1:5432/myapp\n")

	stubs := &dockerStubs{
		FindContainer: "pg-1",
	}
	stubs.install(t)

	err := CleanupBranchDB(dir, "DATABASE_URI")
	assert.NoError(t, err)

	// Docker functions should NOT have been called.
	assert.Empty(t, stubs.FindCalls)
	assert.Empty(t, stubs.DropArgs)
}

func TestCleanupBranchDB_InvalidPostgresURI(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "DATABASE_URI=postgres://user:pass@host:5432\n")

	_, err := SetupBranchDB(dir, "feature/branch", "DATABASE_URI")
	require.Error(t, err)
}

func TestCleanupBranchDB_FindContainerFails(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "DATABASE_URI=postgres://postgres:postgres@127.0.0.1:5432/myapp__feat_branch\n")

	stubs := &dockerStubs{
		FindContainerErr: fmt.Errorf("docker not running"),
	}
	stubs.install(t)

	err := CleanupBranchDB(dir, "DATABASE_URI")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "finding postgres container")
}

func TestCleanupBranchDB_DropFails(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "DATABASE_URI=postgres://postgres:postgres@127.0.0.1:5432/myapp__feat_branch\n")

	stubs := &dockerStubs{
		FindContainer: "pg-1",
		DropDBErr:     fmt.Errorf("database in use"),
	}
	stubs.install(t)

	err := CleanupBranchDB(dir, "DATABASE_URI")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dropping database")
	assert.Contains(t, err.Error(), "database in use")
}

func TestCleanupBranchDB_Success(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "DATABASE_URI=postgres://postgres:postgres@127.0.0.1:5432/myapp__jd_fix_123\n")

	stubs := &dockerStubs{
		FindContainer: "my-postgres-1",
	}
	stubs.install(t)

	err := CleanupBranchDB(dir, "DATABASE_URI")
	require.NoError(t, err)

	// Verify drop was called with the correct args.
	require.Len(t, stubs.DropArgs, 1)
	assert.Equal(t, "my-postgres-1", stubs.DropArgs[0].Container)
	assert.Equal(t, "postgres://postgres:postgres@127.0.0.1:5432", stubs.DropArgs[0].BaseURI)
	assert.Equal(t, "myapp__jd_fix_123", stubs.DropArgs[0].DBName)
}

func TestCleanupBranchDB_PostgresqlScheme(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "DATABASE_URL=postgresql://user:pass@host:5432/testdb__feat_v2\n")

	stubs := &dockerStubs{
		FindContainer: "pg-1",
	}
	stubs.install(t)

	err := CleanupBranchDB(dir, "DATABASE_URL")
	require.NoError(t, err)

	require.Len(t, stubs.DropArgs, 1)
	assert.Equal(t, "testdb__feat_v2", stubs.DropArgs[0].DBName)
}
