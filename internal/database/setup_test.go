package database

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testResolver struct {
	target ContainerTarget
	err    error
	calls  int
}

func (r *testResolver) Resolve(string, string, string) (ContainerTarget, error) {
	r.calls++
	return r.target, r.err
}

func newDatabaseWorktree(t *testing.T) (string, string) {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "README"), []byte("test\n"), 0o644))
	runGit(t, repo, "add", "README")
	runGit(t, repo, "commit", "-m", "initial")
	worktree := filepath.Join(t.TempDir(), "feature")
	runGit(t, repo, "worktree", "add", "-b", "feature/test", worktree)
	return repo, worktree
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := command.CombinedOutput()
	require.NoErrorf(t, err, "git %v: %s", args, output)
}

func installDatabaseStubs(t *testing.T, resolver ContainerResolver, create func(string, string, string) error, drop func(string, string, []string) error) {
	t.Helper()
	originalResolver := newContainerResolverFn
	originalCreate := createDatabaseFn
	originalDrop := dropDatabasesFn
	newContainerResolverFn = func() (ContainerResolver, error) { return resolver, nil }
	createDatabaseFn = create
	dropDatabasesFn = drop
	t.Cleanup(func() {
		newContainerResolverFn = originalResolver
		createDatabaseFn = originalCreate
		dropDatabasesFn = originalDrop
	})
}

func TestSetupPersistsOwnershipWithoutCredentials(t *testing.T) {
	_, worktree := newDatabaseWorktree(t)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte("DATABASE_URL=postgres://app:secret@127.0.0.1:5432/myapp\n"), 0o640))
	resolver := &testResolver{target: ContainerTarget{ID: "id-1", Name: "project-db"}}
	var created string
	installDatabaseStubs(t, resolver, func(_, _, name string) error { created = name; return nil }, func(string, string, []string) error { return nil })

	result, err := SetupBranchDB(worktree, "feature/test", "DATABASE_URL", "")
	require.NoError(t, err)
	assert.Equal(t, created, result.DBName)
	assert.Equal(t, 1, resolver.calls)
	value, err := ReadEnvValue(worktree, "DATABASE_URL")
	require.NoError(t, err)
	assert.Contains(t, value, result.DBName)
	info, err := os.Stat(filepath.Join(worktree, ".env"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())

	store, worktreeID, err := databaseStoreForWorktree(worktree)
	require.NoError(t, err)
	record, err := store.load(worktreeID)
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.Equal(t, databaseStatusActive, record.Status)
	assert.Equal(t, result.DBName, record.Database)
	assert.Empty(t, record.Container)
	assert.NotContains(t, mustMarshalRecord(t, record), "secret")
}

func TestSetupRollsBackWhenEnvironmentRewriteFails(t *testing.T) {
	_, worktree := newDatabaseWorktree(t)
	target := filepath.Join(worktree, "outside-env")
	require.NoError(t, os.WriteFile(target, []byte("DATABASE_URL=postgres://app@127.0.0.1/myapp\n"), 0o600))
	require.NoError(t, os.Symlink(target, filepath.Join(worktree, ".env")))
	resolver := &testResolver{target: ContainerTarget{ID: "id-1", Name: "project-db"}}
	dropped := 0
	installDatabaseStubs(t, resolver, func(string, string, string) error { return nil }, func(string, string, []string) error { dropped++; return nil })

	_, err := SetupBranchDB(worktree, "feature/test", "DATABASE_URL", "")
	require.ErrorContains(t, err, "refusing to rewrite symlinked")
	assert.Equal(t, 1, dropped)
	store, worktreeID, storeErr := databaseStoreForWorktree(worktree)
	require.NoError(t, storeErr)
	record, loadErr := store.load(worktreeID)
	require.NoError(t, loadErr)
	require.NotNil(t, record)
	assert.Equal(t, databaseStatusSetupPending, record.Status)
}

func TestSetupRetryRecreatesDatabaseAfterRewriteRollback(t *testing.T) {
	_, worktree := newDatabaseWorktree(t)
	target := filepath.Join(worktree, "outside-env")
	require.NoError(t, os.WriteFile(target, []byte("DATABASE_URL=postgres://app@127.0.0.1/myapp\n"), 0o600))
	require.NoError(t, os.Symlink(target, filepath.Join(worktree, ".env")))
	resolver := &testResolver{target: ContainerTarget{ID: "id-1", Name: "project-db"}}
	creates := 0
	drops := 0
	installDatabaseStubs(t, resolver, func(string, string, string) error { creates++; return nil }, func(string, string, []string) error { drops++; return nil })

	_, err := SetupBranchDB(worktree, "feature/test", "DATABASE_URL", "")
	require.Error(t, err)
	require.NoError(t, os.Remove(filepath.Join(worktree, ".env")))
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte("DATABASE_URL=postgres://app@127.0.0.1/myapp\n"), 0o600))

	result, err := SetupBranchDB(worktree, "feature/test", "DATABASE_URL", "")
	require.NoError(t, err)
	assert.Equal(t, 2, creates)
	assert.Equal(t, 1, drops)
	value, err := ReadEnvValue(worktree, "DATABASE_URL")
	require.NoError(t, err)
	assert.Contains(t, value, result.DBName)
}

func TestCleanupUsesRecordAfterEnvironmentRemoval(t *testing.T) {
	_, worktree := newDatabaseWorktree(t)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte("DATABASE_URL=postgres://app@127.0.0.1/myapp\n"), 0o600))
	resolver := &testResolver{target: ContainerTarget{ID: "id-1", Name: "project-db"}}
	var dropped []string
	installDatabaseStubs(t, resolver, func(string, string, string) error { return nil }, func(_ string, _ string, names []string) error { dropped = append(dropped, names...); return nil })
	result, err := SetupBranchDB(worktree, "feature/test", "DATABASE_URL", "")
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(worktree, ".env")))

	session := NewCleanupSession()
	session.resolver = resolver
	ticket, err := session.Prepare(worktree)
	require.NoError(t, err)
	require.NotNil(t, ticket)
	require.NoError(t, session.MarkDeleted(ticket))
	require.NoError(t, session.Flush())
	assert.Equal(t, []string{result.DBName}, dropped)
	store, worktreeID, storeErr := databaseStoreForWorktree(worktree)
	require.NoError(t, storeErr)
	record, loadErr := store.load(worktreeID)
	require.NoError(t, loadErr)
	assert.Nil(t, record)
}

func TestCleanupDoesNotAuthorizeLegacyEnvironmentName(t *testing.T) {
	_, worktree := newDatabaseWorktree(t)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte("DATABASE_URL=postgres://app@127.0.0.1/myapp__feature\n"), 0o600))
	plan, err := NewCleanupSession().Prepare(worktree)
	require.NoError(t, err)
	assert.Nil(t, plan)
	legacy, err := LegacyBranchDatabase(worktree, "DATABASE_URL")
	require.NoError(t, err)
	assert.Equal(t, "myapp__feature", legacy)
}

func TestCleanRetriesPendingCleanupAfterWorktreeIsGone(t *testing.T) {
	repo, worktree := newDatabaseWorktree(t)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte("DATABASE_URL=postgres://app@127.0.0.1/myapp\n"), 0o600))
	resolver := &testResolver{target: ContainerTarget{ID: "id-1", Name: "project-db"}}
	var drops int
	installDatabaseStubs(t, resolver, func(string, string, string) error { return nil }, func(string, string, []string) error { drops++; return nil })
	_, err := SetupBranchDB(worktree, "feature/test", "DATABASE_URL", "")
	require.NoError(t, err)
	session := NewCleanupSession()
	session.resolver = resolver
	plan, err := session.Prepare(worktree)
	require.NoError(t, err)
	require.NoError(t, session.MarkDeleted(plan))
	require.NoError(t, os.RemoveAll(worktree))

	retrySession := NewCleanupSession()
	retrySession.resolver = resolver
	retried, err := retrySession.RetryPending(repo)
	require.NoError(t, err)
	assert.Equal(t, 1, retried)
	assert.Equal(t, 1, drops)
}

func TestCleanRetriesOrphanedActiveCleanup(t *testing.T) {
	repo, worktree := newDatabaseWorktree(t)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte("DATABASE_URL=postgres://app@127.0.0.1/myapp\n"), 0o600))
	resolver := &testResolver{target: ContainerTarget{ID: "id-1", Name: "project-db"}}
	drops := 0
	installDatabaseStubs(t, resolver, func(string, string, string) error { return nil }, func(string, string, []string) error { drops++; return nil })
	_, err := SetupBranchDB(worktree, "feature/test", "DATABASE_URL", "")
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(worktree))

	session := NewCleanupSession()
	session.resolver = resolver
	retried, err := session.RetryPending(repo)
	require.NoError(t, err)
	assert.Equal(t, 1, retried)
	assert.Equal(t, 1, drops)
}

func TestCleanupNeverDropsUnmarkedRecord(t *testing.T) {
	_, worktree := newDatabaseWorktree(t)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte("DATABASE_URL=postgres://app@127.0.0.1/myapp\n"), 0o600))
	resolver := &testResolver{target: ContainerTarget{ID: "id-1", Name: "project-db"}}
	drops := 0
	installDatabaseStubs(t, resolver, func(string, string, string) error { return nil }, func(string, string, []string) error { drops++; return nil })
	_, err := SetupBranchDB(worktree, "feature/test", "DATABASE_URL", "")
	require.NoError(t, err)

	session := NewCleanupSession()
	session.resolver = resolver
	ticket, err := session.Prepare(worktree)
	require.NoError(t, err)
	require.NotNil(t, ticket)
	err = executeCleanupBatch([]*cleanupPlan{ticket.plan})
	require.ErrorContains(t, err, "changed before cleanup")
	assert.Zero(t, drops)
}

func TestWorktreeMissingFailsClosed(t *testing.T) {
	missing, err := worktreeMissing(filepath.Join(t.TempDir(), "missing"))
	require.NoError(t, err)
	assert.True(t, missing)

	missing, err = worktreeMissing(t.TempDir())
	require.NoError(t, err)
	assert.False(t, missing)
}

func mustMarshalRecord(t *testing.T, record *DatabaseRecord) string {
	t.Helper()
	data, err := json.Marshal(record)
	require.NoError(t, err)
	return string(data)
}
