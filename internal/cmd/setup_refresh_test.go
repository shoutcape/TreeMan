package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shoutcape/treeman/internal/git"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ownedDatabase = "myapp_feature_refresh"

// seedDatabaseOwnership writes the durable ownership record a worktree would
// have after its branch database was created, so a refresh test can run
// without Docker.
func seedDatabaseOwnership(t *testing.T, repo, worktree, branch string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"),
		[]byte("[database]\nenv_key = \"DATABASE_URL\"\n"), 0o600))

	commonDir, err := git.CommonDir(worktree)
	require.NoError(t, err)
	worktreeID, err := git.WorktreeID(worktree)
	require.NoError(t, err)
	stateDir := filepath.Join(commonDir, "treeman", "databases")
	require.NoError(t, os.MkdirAll(stateDir, 0o700))

	repositoryID := "0123456789abcdef0123456789abcdef"
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, "repository-id"), []byte(repositoryID), 0o600))

	record := map[string]any{
		"version": 1, "repository_id": repositoryID,
		"worktree_id": worktreeID, "worktree_path": worktree, "branch": branch,
		"database": ownedDatabase, "container_id": "container-1",
		"host": "127.0.0.1", "port": "5432", "user": "app",
		"status": "active", "updated_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	data, err := json.Marshal(record)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, worktreeID+".json"), append(data, '\n'), 0o600))
}

func refreshOnly() rerunSetupOptions {
	return rerunSetupOptions{refreshEnv: true, skipDatabase: true, skipDeps: true, skipHooks: true}
}

func TestSetupRefreshKeepsTheOwnedDatabaseName(t *testing.T) {
	repo, worktree := setupTestRepo(t, "feature/refresh-owned")
	seedDatabaseOwnership(t, repo, worktree, "feature/refresh-owned")
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env"),
		[]byte("DATABASE_URL=postgres://app@127.0.0.1:5432/myapp\nADDED=main\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"),
		[]byte("DATABASE_URL=postgres://app@127.0.0.1:5432/"+ownedDatabase+"\n"), 0o600))
	chdirForTest(t, worktree)

	_, stderr, err := runSetupIn(t, "", refreshOnly())
	require.NoError(t, err)

	refreshed := readTestFile(t, filepath.Join(worktree, ".env"))
	assert.Contains(t, refreshed, "ADDED=main", "the refresh must bring in the new key")
	assert.Contains(t, refreshed, "/"+ownedDatabase, "the owned database must survive the refresh")
	assert.NotContains(t, refreshed, "/myapp\n")
	assert.Contains(t, stderr, "Kept database "+ownedDatabase)
}

func TestSetupRefreshPreservesTheProtectedFileWhenOwnershipCannotBeProven(t *testing.T) {
	repo, worktree := setupTestRepo(t, "feature/refresh-drift")
	seedDatabaseOwnership(t, repo, worktree, "feature/refresh-drift")
	// The main worktree now points at a different port, so restoring the
	// owned name into it would name a database on another server.
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env"),
		[]byte("DATABASE_URL=postgres://app@127.0.0.1:5544/myapp\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env.local"), []byte("LOCAL=main\n"), 0o600))
	branchEnv := "DATABASE_URL=postgres://app@127.0.0.1:5432/" + ownedDatabase + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte(branchEnv), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env.local"), []byte("LOCAL=branch\n"), 0o600))
	chdirForTest(t, worktree)

	_, stderr, err := runSetupIn(t, "", refreshOnly())
	require.NoError(t, err)

	assert.Equal(t, branchEnv, readTestFile(t, filepath.Join(worktree, ".env")))
	assert.Equal(t, "LOCAL=main\n", readTestFile(t, filepath.Join(worktree, ".env.local")),
		"one protected file must not hold back the others")
	assert.Contains(t, stderr, "could not copy .env")
	assert.Contains(t, stderr, "failed 1")
}

func TestSetupNormalRerunNeverOverwritesAnEditedDatabaseTarget(t *testing.T) {
	repo, worktree := setupTestRepo(t, "feature/no-refresh")
	seedDatabaseOwnership(t, repo, worktree, "feature/no-refresh")
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env"),
		[]byte("DATABASE_URL=postgres://app@127.0.0.1:5432/myapp\n"), 0o600))
	edited := "DATABASE_URL=postgres://app@127.0.0.1:5432/hand_edited\n"
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte(edited), 0o600))
	chdirForTest(t, worktree)

	_, _, err := runSetupIn(t, "", rerunSetupOptions{skipDatabase: true, skipDeps: true, skipHooks: true})
	require.NoError(t, err)

	assert.Equal(t, edited, readTestFile(t, filepath.Join(worktree, ".env")))
}

func TestSetupRefreshNeverCreatesAnUnprovenDatabaseEnvironment(t *testing.T) {
	for _, port := range []string{"5432", "5544"} {
		t.Run(port, func(t *testing.T) {
			repo, worktree := setupTestRepo(t, "feature/refresh-absent")
			seedDatabaseOwnership(t, repo, worktree, "feature/refresh-absent")
			require.NoError(t, os.WriteFile(filepath.Join(repo, ".env"),
				[]byte("DATABASE_URL=postgres://app@127.0.0.1:"+port+"/myapp\n"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(repo, ".env.local"), []byte("LOCAL=main\n"), 0o600))
			chdirForTest(t, worktree)

			_, stderr, err := runSetupIn(t, "", refreshOnly())
			require.NoError(t, err)

			assert.NoFileExists(t, filepath.Join(worktree, ".env"))
			assert.Equal(t, "LOCAL=main\n", readTestFile(t, filepath.Join(worktree, ".env.local")))
			assert.Contains(t, stderr, "current .env has no PostgreSQL")
			assert.NotContains(t, stderr, "Kept database")
			assert.NotContains(t, stderr, "Preserved existing .env.")
		})
	}
}

func TestSetupRefreshSkipsEnvironmentWhenOwnershipStateIsInvalid(t *testing.T) {
	for _, present := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "existing"}[present], func(t *testing.T) {
			repo, worktree := setupTestRepo(t, "feature/refresh-invalid")
			seedDatabaseOwnership(t, repo, worktree, "feature/refresh-invalid")
			commonDir, err := git.CommonDir(worktree)
			require.NoError(t, err)
			worktreeID, err := git.WorktreeID(worktree)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(commonDir, "treeman", "databases", worktreeID+".json"), []byte("invalid"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(repo, ".env"), []byte("DATABASE_URL=postgres://app@127.0.0.1:5432/myapp\n"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(repo, ".env.local"), []byte("LOCAL=main\n"), 0o600))
			if present {
				require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte("LOCAL=branch\n"), 0o600))
			}
			chdirForTest(t, worktree)

			_, stderr, err := runSetupIn(t, "", refreshOnly())
			require.NoError(t, err)

			if present {
				assert.Equal(t, "LOCAL=branch\n", readTestFile(t, filepath.Join(worktree, ".env")))
			} else {
				assert.NoFileExists(t, filepath.Join(worktree, ".env"))
			}
			assert.Equal(t, "LOCAL=main\n", readTestFile(t, filepath.Join(worktree, ".env.local")))
			assert.Contains(t, stderr, "could not copy .env")
			assert.NotContains(t, stderr, "Kept database")
			assert.NotContains(t, stderr, "Preserved existing .env.")
		})
	}
}
