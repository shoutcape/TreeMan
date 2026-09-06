package database

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shoutcape/treeman/internal/git"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testURI = "DATABASE_URL=postgres://app:secret@127.0.0.1:5432/myapp\n"

// activateWorktreeDatabase runs a creation setup so the worktree ends with an
// active ownership record, the state every rerun test starts from.
func activateWorktreeDatabase(t *testing.T, container string) (string, string) {
	t.Helper()
	_, worktree := newDatabaseWorktree(t)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte(testURI), 0o600))
	resolver := &testResolver{target: ContainerTarget{ID: "id-1", Name: "project-db"}}
	backend := installDatabaseStubs(t, resolver,
		func(string, string, string) error { return nil },
		func(string, string, []string) error { return nil })

	result, err := setupBranchDB(backend, worktree, "feature/test", "DATABASE_URL", container)
	require.NoError(t, err)
	return worktree, result.DBName
}

func rerun(worktree string) SetupOptions {
	return SetupOptions{WorktreePath: worktree, Branch: "feature/test", EnvKey: "DATABASE_URL", Rerun: true}
}

func loadTestRecord(t *testing.T, worktree string) *DatabaseRecord {
	t.Helper()
	store, worktreeID, err := databaseStoreForWorktree(worktree)
	require.NoError(t, err)
	record, err := store.load(worktreeID)
	require.NoError(t, err)
	require.NotNil(t, record)
	return record
}

func TestRerunReusesActiveDatabaseWithoutCreatingOrDropping(t *testing.T) {
	worktree, dbName := activateWorktreeDatabase(t, "")
	before := loadTestRecord(t, worktree)
	envBefore, err := os.ReadFile(filepath.Join(worktree, ".env"))
	require.NoError(t, err)

	creates, drops, checked := 0, 0, 0
	backend := stubBackend{
		resolver: multiResolver{"id-1": {ID: "id-1", Name: "project-db"}},
		create:   func(string, string, string) error { creates++; return nil },
		drop:     func(string, string, []string) error { drops++; return nil },
		exists:   func(string, string, string) (bool, error) { checked++; return true, nil },
	}

	result, err := setupDatabase(backend, rerun(worktree))
	require.NoError(t, err)

	assert.True(t, result.Reused)
	assert.Equal(t, dbName, result.DBName)
	assert.Equal(t, 0, creates)
	assert.Equal(t, 0, drops)
	assert.Equal(t, 1, checked)

	after := loadTestRecord(t, worktree)
	assert.Equal(t, databaseStatusActive, after.Status)
	assert.Equal(t, before.UpdatedAt, after.UpdatedAt)

	envAfter, err := os.ReadFile(filepath.Join(worktree, ".env"))
	require.NoError(t, err)
	assert.Equal(t, string(envBefore), string(envAfter))
}

func TestRerunResolvesTheRecordedContainerIDOnly(t *testing.T) {
	worktree, _ := activateWorktreeDatabase(t, "")
	record := loadTestRecord(t, worktree)

	// multiResolver answers ResolveID and fails Resolve, so a lookup by name
	// or port fails the test rather than silently choosing a container.
	backend := stubBackend{
		resolver: multiResolver{record.ContainerID: {ID: record.ContainerID, Name: "project-db"}},
		create:   func(string, string, string) error { return nil },
		drop:     func(string, string, []string) error { return nil },
		exists: func(container, _, _ string) (bool, error) {
			assert.Equal(t, record.ContainerID, container)
			return true, nil
		},
	}

	_, err := setupDatabase(backend, rerun(worktree))
	require.NoError(t, err)
}

func TestRerunRefusesReplacementContainer(t *testing.T) {
	worktree, _ := activateWorktreeDatabase(t, "")
	backend := stubBackend{
		resolver: multiResolver{"id-2": {ID: "id-2", Name: "project-db"}},
		create:   func(string, string, string) error { return nil },
		drop:     func(string, string, []string) error { return nil },
		exists:   func(string, string, string) (bool, error) { return true, nil },
	}

	_, err := setupDatabase(backend, rerun(worktree))
	assert.ErrorContains(t, err, "finding recorded postgres container")
}

func TestRerunReportsMissingDatabaseWithoutRecreatingIt(t *testing.T) {
	worktree, dbName := activateWorktreeDatabase(t, "")
	creates := 0
	backend := stubBackend{
		resolver: multiResolver{"id-1": {ID: "id-1", Name: "project-db"}},
		create:   func(string, string, string) error { creates++; return nil },
		drop:     func(string, string, []string) error { return nil },
		exists:   func(string, string, string) (bool, error) { return false, nil },
	}

	_, err := setupDatabase(backend, rerun(worktree))

	assert.ErrorContains(t, err, dbName)
	assert.ErrorContains(t, err, "does not recreate an owned database")
	assert.Equal(t, 0, creates)
	assert.Equal(t, databaseStatusActive, loadTestRecord(t, worktree).Status)
}

func TestRerunRefusesConnectionDrift(t *testing.T) {
	drifts := map[string]string{
		"host":      "DATABASE_URL=postgres://app:secret@db.example.com:5432/",
		"port":      "DATABASE_URL=postgres://app:secret@127.0.0.1:5544/",
		"user":      "DATABASE_URL=postgres://other:secret@127.0.0.1:5432/",
		"container": "",
	}
	for name, uri := range drifts {
		t.Run(name, func(t *testing.T) {
			worktree, dbName := activateWorktreeDatabase(t, "")
			options := rerun(worktree)
			if name == "container" {
				options.ConfiguredContainer = "some-other-container"
			} else {
				require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte(uri+dbName+"\n"), 0o600))
			}
			backend := stubBackend{
				resolver: multiResolver{"id-1": {ID: "id-1", Name: "project-db"}},
				create:   func(string, string, string) error { return nil },
				drop:     func(string, string, []string) error { return nil },
				exists:   func(string, string, string) (bool, error) { return true, nil },
			}

			_, err := setupDatabase(backend, options)
			assert.ErrorContains(t, err, "database setup target changed")
		})
	}
}

func TestRerunPreservesEnvironmentNamingADifferentDatabase(t *testing.T) {
	worktree, dbName := activateWorktreeDatabase(t, "")
	edited := "DATABASE_URL=postgres://app:secret@127.0.0.1:5432/hand_edited\n"
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte(edited), 0o600))
	backend := stubBackend{
		resolver: multiResolver{"id-1": {ID: "id-1", Name: "project-db"}},
		create:   func(string, string, string) error { return nil },
		drop:     func(string, string, []string) error { return nil },
		exists:   func(string, string, string) (bool, error) { return true, nil },
	}

	_, err := setupDatabase(backend, rerun(worktree))

	assert.ErrorContains(t, err, "hand_edited")
	assert.ErrorContains(t, err, dbName)
	contents, readErr := os.ReadFile(filepath.Join(worktree, ".env"))
	require.NoError(t, readErr)
	assert.Equal(t, edited, string(contents))
}

func TestRerunNeverDropsAnActiveDatabaseWhenTheEnvironmentCannotBeWritten(t *testing.T) {
	worktree, _ := activateWorktreeDatabase(t, "")
	// Replace .env with a symlink, which every rewrite path refuses. A flow
	// that reached the rewrite would then take the rollback drop.
	outside := filepath.Join(t.TempDir(), "outside-env")
	require.NoError(t, os.WriteFile(outside, []byte(testURI), 0o600))
	require.NoError(t, os.Remove(filepath.Join(worktree, ".env")))
	require.NoError(t, os.Symlink(outside, filepath.Join(worktree, ".env")))

	record := loadTestRecord(t, worktree)
	require.NoError(t, os.WriteFile(outside, []byte("DATABASE_URL=postgres://app:secret@127.0.0.1:5432/"+record.Database+"\n"), 0o600))

	drops := 0
	backend := stubBackend{
		resolver: multiResolver{"id-1": {ID: "id-1", Name: "project-db"}},
		create:   func(string, string, string) error { return nil },
		drop:     func(string, string, []string) error { drops++; return nil },
		exists:   func(string, string, string) (bool, error) { return true, nil },
	}

	result, err := setupDatabase(backend, rerun(worktree))
	require.NoError(t, err)

	assert.True(t, result.Reused)
	assert.Equal(t, 0, drops)
}

func TestRerunRefusesRecordStagedForCleanup(t *testing.T) {
	worktree, _ := activateWorktreeDatabase(t, "")
	store, worktreeID, err := databaseStoreForWorktree(worktree)
	require.NoError(t, err)
	record := loadTestRecord(t, worktree)
	_, err = store.markPendingCleanup(worktreeID, *record)
	require.NoError(t, err)

	backend := stubBackend{
		resolver: multiResolver{"id-1": {ID: "id-1", Name: "project-db"}},
		create:   func(string, string, string) error { return nil },
		drop:     func(string, string, []string) error { return nil },
	}

	_, err = setupDatabase(backend, rerun(worktree))

	assert.ErrorContains(t, err, "staged for cleanup")
	assert.Equal(t, databaseStatusPendingCleanup, loadTestRecord(t, worktree).Status)
}

func TestRerunRefusesRecordForAnotherBranch(t *testing.T) {
	worktree, _ := activateWorktreeDatabase(t, "")
	options := rerun(worktree)
	options.Branch = "feature/other"
	backend := stubBackend{
		resolver: multiResolver{"id-1": {ID: "id-1", Name: "project-db"}},
		create:   func(string, string, string) error { return nil },
		drop:     func(string, string, []string) error { return nil },
	}

	_, err := setupDatabase(backend, options)
	assert.ErrorContains(t, err, "names branch")
}

func TestRerunRetriesPendingSetupWithItsRecordedTarget(t *testing.T) {
	_, worktree := newDatabaseWorktree(t)
	outside := filepath.Join(worktree, "outside-env")
	require.NoError(t, os.WriteFile(outside, []byte(testURI), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(worktree, ".env")))
	resolver := &testResolver{target: ContainerTarget{ID: "id-1", Name: "project-db"}}
	creates := 0
	backend := stubBackend{
		resolver: resolver,
		create:   func(string, string, string) error { creates++; return nil },
		drop:     func(string, string, []string) error { return nil },
	}
	// The symlink refuses the rewrite, so setup stops at setup_pending.
	_, err := setupDatabase(backend, rerun(worktree))
	require.Error(t, err)
	pending := loadTestRecord(t, worktree)
	require.Equal(t, databaseStatusSetupPending, pending.Status)

	require.NoError(t, os.Remove(filepath.Join(worktree, ".env")))
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte(testURI), 0o600))

	result, err := setupDatabase(backend, rerun(worktree))
	require.NoError(t, err)

	assert.False(t, result.Reused)
	assert.Equal(t, pending.Database, result.DBName)
	assert.Equal(t, 2, creates)
	assert.Equal(t, databaseStatusActive, loadTestRecord(t, worktree).Status)
}

func TestRerunWithoutRecordProvisionsNormally(t *testing.T) {
	_, worktree := newDatabaseWorktree(t)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte(testURI), 0o600))
	resolver := &testResolver{target: ContainerTarget{ID: "id-1", Name: "project-db"}}
	var created string
	backend := stubBackend{
		resolver: resolver,
		create:   func(_, _, name string) error { created = name; return nil },
		drop:     func(string, string, []string) error { return nil },
	}

	result, err := setupDatabase(backend, rerun(worktree))
	require.NoError(t, err)

	assert.False(t, result.Reused)
	assert.Equal(t, created, result.DBName)
	assert.Equal(t, databaseStatusActive, loadTestRecord(t, worktree).Status)
}

func TestCreationStillRefusesAnActiveRecord(t *testing.T) {
	worktree, _ := activateWorktreeDatabase(t, "")
	backend := installDatabaseStubs(t, &testResolver{target: ContainerTarget{ID: "id-1", Name: "project-db"}},
		func(string, string, string) error { return nil },
		func(string, string, []string) error { return nil })

	_, err := setupBranchDB(backend, worktree, "feature/test", "DATABASE_URL", "")
	assert.ErrorContains(t, err, "already exists")
}

func TestPrepareRefreshReturnsPreparedBytesAndDoesNotWrite(t *testing.T) {
	worktree, dbName := activateWorktreeDatabase(t, "")
	envPath := filepath.Join(worktree, ".env")
	current := "# destination comment\nDATABASE_URL=postgres://app:secret@127.0.0.1:5432/" + dbName + "\nOTHER=unchanged\n"
	require.NoError(t, os.WriteFile(envPath, []byte(current), 0o600))

	source := []byte("# source comment\n export  DATABASE_URL = 'postgres://app:secret@127.0.0.1:5432/myapp'  \nOTHER=source\n\n")
	sourceBefore := append([]byte(nil), source...)
	prepared, gotDBName, err := PrepareRefresh(worktree, "feature/test", "DATABASE_URL", "", source)

	require.NoError(t, err)
	assert.Equal(t, dbName, gotDBName)
	assert.Equal(t, []byte("# source comment\n export  DATABASE_URL = 'postgres://app:secret@127.0.0.1:5432/"+dbName+"'  \nOTHER=source\n\n"), prepared)
	assert.Equal(t, sourceBefore, source)
	gotCurrent, err := os.ReadFile(envPath)
	require.NoError(t, err)
	assert.Equal(t, current, string(gotCurrent))
}

func TestPrepareRefreshWithoutOwnershipReturnsSourceWithoutWritingState(t *testing.T) {
	_, worktree := newDatabaseWorktree(t)
	envPath := filepath.Join(worktree, ".env")
	destination := []byte(testURI)
	require.NoError(t, os.WriteFile(envPath, destination, 0o600))
	source := []byte("# source\nDATABASE_URL=postgres://app:secret@127.0.0.1:5432/replacement\n")
	commonDir, err := git.CommonDir(worktree)
	require.NoError(t, err)

	prepared, dbName, err := PrepareRefresh(worktree, "feature/test", "DATABASE_URL", "", source)

	require.NoError(t, err)
	assert.Empty(t, dbName)
	assert.Equal(t, source, prepared)
	gotDestination, err := os.ReadFile(envPath)
	require.NoError(t, err)
	assert.Equal(t, destination, gotDestination)
	assert.NoDirExists(t, filepath.Join(commonDir, databaseStateDirectory))
}

func TestPrepareRefreshRefusesOwnedMissingConfigurationWithoutWriting(t *testing.T) {
	for _, tc := range []struct {
		name   string
		envKey string
		config string
	}{
		{name: "missing file", envKey: "DATABASE_URL"},
		{name: "missing key", envKey: "DATABASE_URL", config: "OTHER=1\n"},
		{name: "empty env key", envKey: "", config: testURI},
	} {
		t.Run(tc.name, func(t *testing.T) {
			worktree, dbName := activateWorktreeDatabase(t, "")
			envPath := filepath.Join(worktree, ".env")
			if tc.name == "missing file" {
				require.NoError(t, os.Remove(envPath))
			}
			if tc.config != "" {
				require.NoError(t, os.WriteFile(envPath, []byte(tc.config), 0o600))
			}
			store, worktreeID, err := databaseStoreForWorktree(worktree)
			require.NoError(t, err)
			stateBefore, err := os.ReadFile(store.recordPath(worktreeID))
			require.NoError(t, err)
			source := []byte(testURI)

			_, gotDBName, err := PrepareRefresh(worktree, "feature/test", tc.envKey, "", source)

			require.Error(t, err)
			assert.Empty(t, gotDBName)
			assert.ErrorContains(t, err, "no PostgreSQL")
			stateAfter, readErr := os.ReadFile(store.recordPath(worktreeID))
			require.NoError(t, readErr)
			assert.Equal(t, stateBefore, stateAfter)
			if tc.name != "missing file" {
				gotConfig, readErr := os.ReadFile(envPath)
				require.NoError(t, readErr)
				assert.Equal(t, tc.config, string(gotConfig))
			}
			assert.Equal(t, []byte(testURI), source)
			assert.Equal(t, dbName, loadTestRecord(t, worktree).Database)
		})
	}
}

func TestPrepareRefreshRejectsCurrentAndReplacementDrift(t *testing.T) {
	drifted := "DATABASE_URL=postgres://app:secret@127.0.0.1:5544/myapp\n"
	t.Run("current file", func(t *testing.T) {
		worktree, _ := activateWorktreeDatabase(t, "")
		require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte(drifted), 0o600))
		source := []byte(testURI)
		sourceBefore := append([]byte(nil), source...)

		prepared, dbName, err := PrepareRefresh(worktree, "feature/test", "DATABASE_URL", "", source)

		assert.ErrorContains(t, err, "database setup target changed")
		assert.Nil(t, prepared)
		assert.Empty(t, dbName)
		assert.Equal(t, sourceBefore, source)
		gotCurrent, readErr := os.ReadFile(filepath.Join(worktree, ".env"))
		require.NoError(t, readErr)
		assert.Equal(t, drifted, string(gotCurrent))
	})

	t.Run("replacement file", func(t *testing.T) {
		worktree, dbName := activateWorktreeDatabase(t, "")
		envPath := filepath.Join(worktree, ".env")
		current := "DATABASE_URL=postgres://app:secret@127.0.0.1:5432/" + dbName + "\n"
		require.NoError(t, os.WriteFile(envPath, []byte(current), 0o600))
		source := []byte(drifted)
		sourceBefore := append([]byte(nil), source...)

		_, _, err := PrepareRefresh(worktree, "feature/test", "DATABASE_URL", "", source)
		assert.ErrorContains(t, err, "database setup target changed")
		assert.Equal(t, sourceBefore, source)
		gotCurrent, readErr := os.ReadFile(envPath)
		require.NoError(t, readErr)
		assert.Equal(t, current, string(gotCurrent))
	})

	t.Run("replacement without a URI", func(t *testing.T) {
		worktree, _ := activateWorktreeDatabase(t, "")
		source := []byte("OTHER=1\n")

		_, _, err := PrepareRefresh(worktree, "feature/test", "DATABASE_URL", "", source)
		assert.ErrorContains(t, err, "no PostgreSQL")
	})
}

func TestBuildExistsArgs(t *testing.T) {
	assert.Equal(t, []string{
		"exec", "container-1",
		"psql", "-U", "app", "-d", "postgres",
		"-v", "ON_ERROR_STOP=1",
		"-tAc", "SELECT 1 FROM pg_database WHERE datname = 'my_db'",
	}, buildExistsArgs("container-1", "app", "my_db"))
}

func TestBuildExistsArgsQuotesTheName(t *testing.T) {
	args := buildExistsArgs("container-1", "app", "it's")
	assert.Equal(t, "SELECT 1 FROM pg_database WHERE datname = 'it''s'", args[len(args)-1])
}
