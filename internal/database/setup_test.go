package database

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testResolver struct {
	target ContainerTarget
	err    error
	calls  int
}

type multiResolver map[string]ContainerTarget

func (r multiResolver) Resolve(string, string, string) (ContainerTarget, error) {
	return ContainerTarget{}, os.ErrNotExist
}

func (r multiResolver) ResolveID(id string) (ContainerTarget, error) {
	target, ok := r[id]
	if !ok {
		return ContainerTarget{}, os.ErrNotExist
	}
	return target, nil
}

func (r *testResolver) Resolve(string, string, string) (ContainerTarget, error) {
	r.calls++
	return r.target, r.err
}

func (r *testResolver) ResolveID(id string) (ContainerTarget, error) {
	r.calls++
	if r.target.ID != id {
		return ContainerTarget{}, os.ErrNotExist
	}
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

type stubBackend struct {
	resolver ContainerResolver
	create   func(string, string, string) error
	drop     func(string, string, []string) error
}

func (b stubBackend) Snapshot() (ContainerResolver, error) { return b.resolver, nil }
func (b stubBackend) Create(container, user, database string) error {
	return b.create(container, user, database)
}
func (b stubBackend) Drop(container, user string, databases []string) error {
	return b.drop(container, user, databases)
}

func installDatabaseStubs(t *testing.T, resolver ContainerResolver, create func(string, string, string) error, drop func(string, string, []string) error) Backend {
	t.Helper()
	return stubBackend{resolver: resolver, create: create, drop: drop}
}

func TestSetupPersistsOwnershipWithoutCredentials(t *testing.T) {
	_, worktree := newDatabaseWorktree(t)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte("DATABASE_URL=postgres://app:secret@127.0.0.1:5432/myapp\n"), 0o640))
	resolver := &testResolver{target: ContainerTarget{ID: "id-1", Name: "project-db"}}
	var created string
	backend := installDatabaseStubs(t, resolver, func(_, _, name string) error { created = name; return nil }, func(string, string, []string) error { return nil })

	result, err := setupBranchDB(backend, worktree, "feature/test", "DATABASE_URL", "")
	require.NoError(t, err)
	assert.Equal(t, created, result.DBName)
	assert.Equal(t, 2, resolver.calls)
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

func TestProbeChecksTargetWithoutMutatingState(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("DATABASE_URL=postgres://app@127.0.0.1:5432/myapp\n"), 0o600))
	resolver := &testResolver{target: ContainerTarget{ID: "id-1", Name: "project-db"}}

	result, err := probe(stubBackend{resolver: resolver}, dir, "DATABASE_URL", "")

	require.NoError(t, err)
	assert.False(t, result.Skipped)
	assert.Equal(t, 1, resolver.calls)
	assert.NoDirExists(t, filepath.Join(dir, ".treeman"))
}

func TestSetupRollsBackWhenEnvironmentRewriteFails(t *testing.T) {
	_, worktree := newDatabaseWorktree(t)
	target := filepath.Join(worktree, "outside-env")
	require.NoError(t, os.WriteFile(target, []byte("DATABASE_URL=postgres://app@127.0.0.1/myapp\n"), 0o600))
	require.NoError(t, os.Symlink(target, filepath.Join(worktree, ".env")))
	resolver := &testResolver{target: ContainerTarget{ID: "id-1", Name: "project-db"}}
	dropped := 0
	backend := installDatabaseStubs(t, resolver, func(string, string, string) error { return nil }, func(string, string, []string) error { dropped++; return nil })

	_, err := setupBranchDB(backend, worktree, "feature/test", "DATABASE_URL", "")
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
	backend := installDatabaseStubs(t, resolver, func(string, string, string) error { creates++; return nil }, func(string, string, []string) error { drops++; return nil })

	_, err := setupBranchDB(backend, worktree, "feature/test", "DATABASE_URL", "")
	require.Error(t, err)
	require.NoError(t, os.Remove(filepath.Join(worktree, ".env")))
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte("DATABASE_URL=postgres://app@127.0.0.1/myapp\n"), 0o600))

	result, err := setupBranchDB(backend, worktree, "feature/test", "DATABASE_URL", "")
	require.NoError(t, err)
	assert.Equal(t, 2, creates)
	assert.Equal(t, 1, drops)
	value, err := ReadEnvValue(worktree, "DATABASE_URL")
	require.NoError(t, err)
	assert.Contains(t, value, result.DBName)
}

func TestSetupRetryUsesRecordedTargetWhenEnvironmentDrifts(t *testing.T) {
	_, worktree := newDatabaseWorktree(t)
	target := filepath.Join(worktree, "outside-env")
	require.NoError(t, os.WriteFile(target, []byte("DATABASE_URL=postgres://app@127.0.0.1/myapp\n"), 0o600))
	require.NoError(t, os.Symlink(target, filepath.Join(worktree, ".env")))
	resolver := &testResolver{target: ContainerTarget{ID: "id-1", Name: "original-db"}}
	creates := 0
	backend := installDatabaseStubs(t, resolver, func(string, string, string) error { creates++; return nil }, func(string, string, []string) error { return nil })
	_, err := setupBranchDB(backend, worktree, "feature/test", "DATABASE_URL", "original-db")
	require.Error(t, err)
	require.NoError(t, os.Remove(filepath.Join(worktree, ".env")))
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte("DATABASE_URL=postgres://other@localhost:5544/other\n"), 0o600))

	_, err = setupBranchDB(backend, worktree, "feature/test", "DATABASE_URL", "replacement-db")
	require.ErrorContains(t, err, "database setup target changed")
	assert.Equal(t, 1, creates)
	contents, readErr := os.ReadFile(filepath.Join(worktree, ".env"))
	require.NoError(t, readErr)
	assert.Equal(t, "DATABASE_URL=postgres://other@localhost:5544/other\n", string(contents))
	store, worktreeID, storeErr := databaseStoreForWorktree(worktree)
	require.NoError(t, storeErr)
	record, loadErr := store.load(worktreeID)
	require.NoError(t, loadErr)
	require.NotNil(t, record)
	assert.Equal(t, databaseStatusSetupPending, record.Status)
}

func TestCleanupRejectsReplacementContainer(t *testing.T) {
	for _, configuredContainer := range []string{"configured-db", ""} {
		t.Run(configuredContainer, func(t *testing.T) {
			_, worktree := newDatabaseWorktree(t)
			require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte("DATABASE_URL=postgres://app@127.0.0.1:5432/myapp\n"), 0o600))
			resolver := &testResolver{target: ContainerTarget{ID: "old-id", Name: "configured-db"}}
			drops := 0
			backend := installDatabaseStubs(t, resolver, func(string, string, string) error { return nil }, func(string, string, []string) error { drops++; return nil })
			_, err := setupBranchDB(backend, worktree, "feature/test", "DATABASE_URL", configuredContainer)
			require.NoError(t, err)
			resolver.target = ContainerTarget{ID: "new-id", Name: "configured-db"}

			_, err = newCleanupBatch(backend).Prepare(worktree)
			require.ErrorContains(t, err, "finding recorded postgres container")
			assert.Zero(t, drops)
		})
	}
}

func TestCleanupUsesRecordAfterEnvironmentRemoval(t *testing.T) {
	_, worktree := newDatabaseWorktree(t)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte("DATABASE_URL=postgres://app@127.0.0.1/myapp\n"), 0o600))
	resolver := &testResolver{target: ContainerTarget{ID: "id-1", Name: "project-db"}}
	var dropped []string
	backend := installDatabaseStubs(t, resolver, func(string, string, string) error { return nil }, func(_ string, _ string, names []string) error { dropped = append(dropped, names...); return nil })
	result, err := setupBranchDB(backend, worktree, "feature/test", "DATABASE_URL", "")
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(worktree, ".env")))

	batch := newCleanupBatch(backend)
	commit, err := batch.Prepare(worktree)
	require.NoError(t, err)
	require.NotNil(t, commit)
	require.NoError(t, commit())
	require.NoError(t, batch.Flush())
	assert.Equal(t, []string{result.DBName}, dropped)
	store, worktreeID, storeErr := databaseStoreForWorktree(worktree)
	require.NoError(t, storeErr)
	record, loadErr := store.load(worktreeID)
	require.NoError(t, loadErr)
	assert.Nil(t, record)
}

func TestCleanRetriesPendingCleanupAfterWorktreeIsGone(t *testing.T) {
	repo, worktree := newDatabaseWorktree(t)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte("DATABASE_URL=postgres://app@127.0.0.1/myapp\n"), 0o600))
	resolver := &testResolver{target: ContainerTarget{ID: "id-1", Name: "project-db"}}
	var drops int
	backend := installDatabaseStubs(t, resolver, func(string, string, string) error { return nil }, func(string, string, []string) error { drops++; return nil })
	_, err := setupBranchDB(backend, worktree, "feature/test", "DATABASE_URL", "")
	require.NoError(t, err)
	batch := newCleanupBatch(backend)
	commit, err := batch.Prepare(worktree)
	require.NoError(t, err)
	require.NoError(t, commit())
	require.NoError(t, os.RemoveAll(worktree))

	retryBatch := newCleanupBatch(backend)
	retried, err := retryBatch.RetryPending(repo)
	require.NoError(t, err)
	assert.Equal(t, 1, retried)
	assert.Equal(t, 1, drops)
}

func TestCleanRetriesOrphanedActiveCleanup(t *testing.T) {
	repo, worktree := newDatabaseWorktree(t)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte("DATABASE_URL=postgres://app@127.0.0.1/myapp\n"), 0o600))
	resolver := &testResolver{target: ContainerTarget{ID: "id-1", Name: "project-db"}}
	drops := 0
	backend := installDatabaseStubs(t, resolver, func(string, string, string) error { return nil }, func(string, string, []string) error { drops++; return nil })
	_, err := setupBranchDB(backend, worktree, "feature/test", "DATABASE_URL", "")
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(worktree))
	runGit(t, repo, "worktree", "prune")
	runGit(t, repo, "branch", "-D", "feature/test")

	batch := newCleanupBatch(backend)
	retried, err := batch.RetryPending(repo)
	require.NoError(t, err)
	assert.Equal(t, 1, retried)
	assert.Equal(t, 1, drops)
}

func TestCleanDoesNotRetryOrphanedDatabaseWhileBranchExists(t *testing.T) {
	repo, worktree := newDatabaseWorktree(t)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte("DATABASE_URL=postgres://app@127.0.0.1/myapp\n"), 0o600))
	resolver := &testResolver{target: ContainerTarget{ID: "id-1", Name: "project-db"}}
	drops := 0
	backend := installDatabaseStubs(t, resolver, func(string, string, string) error { return nil }, func(string, string, []string) error { drops++; return nil })
	require.NoError(t, func() error {
		_, err := setupBranchDB(backend, worktree, "feature/test", "DATABASE_URL", "")
		return err
	}())
	require.NoError(t, os.RemoveAll(worktree))

	batch := newCleanupBatch(backend)
	retried, err := batch.RetryPending(repo)
	require.NoError(t, err)
	assert.Zero(t, retried)
	assert.Zero(t, drops)
}

func TestCleanupNeverDropsUnmarkedRecord(t *testing.T) {
	_, worktree := newDatabaseWorktree(t)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte("DATABASE_URL=postgres://app@127.0.0.1/myapp\n"), 0o600))
	resolver := &testResolver{target: ContainerTarget{ID: "id-1", Name: "project-db"}}
	drops := 0
	backend := installDatabaseStubs(t, resolver, func(string, string, string) error { return nil }, func(string, string, []string) error { drops++; return nil })
	_, err := setupBranchDB(backend, worktree, "feature/test", "DATABASE_URL", "")
	require.NoError(t, err)

	batch := newCleanupBatch(backend)
	commit, err := batch.Prepare(worktree)
	require.NoError(t, err)
	require.NotNil(t, commit)
	require.NoError(t, batch.Flush())
	assert.Zero(t, drops)
}

func TestCleanupRetainsFailedDropForRetry(t *testing.T) {
	repo, worktree := newDatabaseWorktree(t)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte("DATABASE_URL=postgres://app@127.0.0.1/myapp\n"), 0o600))
	resolver := &testResolver{target: ContainerTarget{ID: "id-1", Name: "project-db"}}
	failDrop := true
	backend := installDatabaseStubs(t, resolver, func(string, string, string) error { return nil }, func(string, string, []string) error {
		if failDrop {
			return assert.AnError
		}
		return nil
	})
	require.NoError(t, func() error {
		_, err := setupBranchDB(backend, worktree, "feature/test", "DATABASE_URL", "")
		return err
	}())

	batch := newCleanupBatch(backend)
	commit, err := batch.Prepare(worktree)
	require.NoError(t, err)
	require.NoError(t, commit())
	require.ErrorIs(t, batch.Flush(), assert.AnError)
	store, worktreeID, err := databaseStoreForWorktree(worktree)
	require.NoError(t, err)
	record, err := store.load(worktreeID)
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.Equal(t, databaseStatusPendingCleanup, record.Status)

	failDrop = false
	retry := newCleanupBatch(backend)
	retried, err := retry.RetryPending(repo)
	require.NoError(t, err)
	assert.Equal(t, 1, retried)
	record, err = store.load(worktreeID)
	require.NoError(t, err)
	assert.Nil(t, record)
}

func TestRetryPendingCleansHealthyContainersWhenOneIsUnavailable(t *testing.T) {
	repo, worktree := newDatabaseWorktree(t)
	store, _, err := databaseStoreForWorktree(worktree)
	require.NoError(t, err)
	for _, record := range []*DatabaseRecord{
		{WorktreeID: "healthy", WorktreePath: filepath.Join(repo, "gone-healthy"), Branch: "healthy", Database: "database_healthy", ContainerID: "healthy-id", Host: "127.0.0.1", Port: "5432", User: "app", Status: databaseStatusSetupPending},
		{WorktreeID: "unavailable", WorktreePath: filepath.Join(repo, "gone-unavailable"), Branch: "unavailable", Database: "database_unavailable", ContainerID: "unavailable-id", Host: "127.0.0.1", Port: "5432", User: "app", Status: databaseStatusSetupPending},
	} {
		stored, err := store.beginSetup(record)
		require.NoError(t, err)
		_, err = store.markPendingCleanup(stored.WorktreeID, *stored)
		require.NoError(t, err)
	}
	resolver := multiResolver{"healthy-id": {ID: "healthy-id", Name: "healthy-db"}}
	var dropped []string
	backend := installDatabaseStubs(t, resolver, func(string, string, string) error { return nil }, func(_ string, _ string, names []string) error {
		dropped = append(dropped, names...)
		return nil
	})
	batch := newCleanupBatch(backend)
	retried, err := batch.RetryPending(repo)
	require.ErrorContains(t, err, "unavailable")
	assert.Equal(t, 1, retried)
	assert.Equal(t, []string{"database_healthy"}, dropped)

	healthy, err := store.load("healthy")
	require.NoError(t, err)
	assert.Nil(t, healthy)
	unavailable, err := store.load("unavailable")
	require.NoError(t, err)
	require.NotNil(t, unavailable)
	assert.Equal(t, databaseStatusPendingCleanup, unavailable.Status)
}

func TestStoreRejectsDuplicateDatabaseOwnership(t *testing.T) {
	_, worktree := newDatabaseWorktree(t)
	store, _, err := databaseStoreForWorktree(worktree)
	require.NoError(t, err)
	first := &DatabaseRecord{WorktreeID: "first", WorktreePath: filepath.Join(t.TempDir(), "first"), Branch: "first", Database: "shared", ContainerID: "container", Host: "127.0.0.1", Port: "5432", User: "app", Status: databaseStatusSetupPending}
	_, err = store.beginSetup(first)
	require.NoError(t, err)
	second := &DatabaseRecord{WorktreeID: "second", WorktreePath: filepath.Join(t.TempDir(), "second"), Branch: "second", Database: "shared", ContainerID: "container", Host: "127.0.0.1", Port: "5432", User: "app", Status: databaseStatusSetupPending}
	_, err = store.beginSetup(second)
	require.ErrorContains(t, err, `database "shared" is already owned`)
}

func TestWorktreeMissingFailsClosed(t *testing.T) {
	missing, err := worktreeMissing(filepath.Join(t.TempDir(), "missing"))
	require.NoError(t, err)
	assert.True(t, missing)

	missing, err = worktreeMissing(t.TempDir())
	require.NoError(t, err)
	assert.False(t, missing)
}

func TestValidateRecordFailsClosed(t *testing.T) {
	valid := DatabaseRecord{Version: 1, RepositoryID: "repo", WorktreeID: "worktree", WorktreePath: filepath.Join(t.TempDir(), "worktree"), Branch: "feature/test", Database: "app_feature", ContainerID: "abc123", Host: "127.0.0.1", Port: "5432", User: "app", Status: databaseStatusActive, UpdatedAt: time.Now().UTC()}
	tests := map[string]func(*DatabaseRecord){
		"repository tampering": func(r *DatabaseRecord) { r.RepositoryID = "other" },
		"worktree tampering":   func(r *DatabaseRecord) { r.WorktreeID = "../escape" },
		"relative path":        func(r *DatabaseRecord) { r.WorktreePath = "relative" },
		"unclean path":         func(r *DatabaseRecord) { r.WorktreePath += "/../other" },
		"illegal state":        func(r *DatabaseRecord) { r.Status = "deleted" },
		"missing container":    func(r *DatabaseRecord) { r.ContainerID = "" },
		"missing timestamp":    func(r *DatabaseRecord) { r.UpdatedAt = time.Time{} },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			record := valid
			mutate(&record)
			require.Error(t, validateRecord(record, "repo", "worktree"))
		})
	}
}

func TestStoreLoadRejectsCorruptAndUnknownState(t *testing.T) {
	_, worktree := newDatabaseWorktree(t)
	store, worktreeID, err := databaseStoreForWorktree(worktree)
	require.NoError(t, err)
	path := store.recordPath(worktreeID)
	require.NoError(t, os.WriteFile(path, []byte("{broken\n"), 0o600))
	_, err = store.load(worktreeID)
	require.ErrorContains(t, err, "parsing database ownership record")

	record := &DatabaseRecord{WorktreeID: worktreeID, WorktreePath: worktree, Branch: "feature/test", Database: "app_feature", ContainerID: "abc123", Host: "127.0.0.1", Port: "5432", User: "app", Status: databaseStatusActive}
	require.NoError(t, store.save(record))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	data = append(bytes.TrimSuffix(data, []byte("}\n")), []byte(",\"unexpected\":true}\n")...)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	_, err = store.load(worktreeID)
	require.ErrorContains(t, err, "unknown field")
}

func mustMarshalRecord(t *testing.T, record *DatabaseRecord) string {
	t.Helper()
	data, err := json.Marshal(record)
	require.NoError(t, err)
	return string(data)
}
