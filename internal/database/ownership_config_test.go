package database

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shoutcape/treeman/internal/git"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupOwnershipPolicyPrecedesRequiredPostgresConfig(t *testing.T) {
	states := []databaseStatus{databaseStatusActive, databaseStatusSetupPending, databaseStatusPendingCleanup}
	configs := map[string]string{
		"missing file":  "",
		"missing key":   "OTHER=1\n",
		"non-postgres":  "DATABASE_URL=mysql://app@127.0.0.1/myapp\n",
		"empty env key": "DATABASE_URL=postgres://app@127.0.0.1/myapp\n",
	}

	for _, status := range states {
		for name, config := range configs {
			t.Run(string(status)+"/"+name, func(t *testing.T) {
				worktree, _ := activateWorktreeDatabase(t, "")
				if name == "missing file" {
					require.NoError(t, os.Remove(filepath.Join(worktree, ".env")))
				} else {
					require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte(config), 0o600))
				}
				if status != databaseStatusActive {
					store, id, err := databaseStoreForWorktree(worktree)
					require.NoError(t, err)
					record, err := store.load(id)
					require.NoError(t, err)
					record.Status = status
					require.NoError(t, store.save(record))
				}
				beforeRecord := loadTestRecord(t, worktree)
				store, worktreeID, err := databaseStoreForWorktree(worktree)
				require.NoError(t, err)
				beforeState, err := os.ReadFile(store.recordPath(worktreeID))
				require.NoError(t, err)
				beforeEnv, err := os.ReadFile(filepath.Join(worktree, ".env"))
				if name == "missing file" {
					require.ErrorIs(t, err, os.ErrNotExist)
				} else {
					require.NoError(t, err)
				}

				calls := 0
				backend := countingBackend{calls: &calls}
				opts := rerun(worktree)
				if name == "empty env key" {
					opts.EnvKey = ""
				}
				result, err := setupDatabase(backend, opts)
				require.Error(t, err)
				assert.False(t, result.Skipped)
				if status == databaseStatusPendingCleanup {
					assert.ErrorContains(t, err, "staged for cleanup")
				} else {
					assert.ErrorContains(t, err, "requires a PostgreSQL")
				}
				assert.Equal(t, 0, calls)
				afterRecord := loadTestRecord(t, worktree)
				assert.Equal(t, beforeRecord.UpdatedAt, afterRecord.UpdatedAt)
				afterState, readErr := os.ReadFile(store.recordPath(worktreeID))
				require.NoError(t, readErr)
				assert.Equal(t, beforeState, afterState)
				if name == "missing file" {
					assert.NoFileExists(t, filepath.Join(worktree, ".env"))
				} else {
					afterEnv, readErr := os.ReadFile(filepath.Join(worktree, ".env"))
					require.NoError(t, readErr)
					assert.Equal(t, beforeEnv, afterEnv)
				}
			})
		}
	}
}

func TestSetupSkipsUnownedMissingPostgresConfigWithoutCreatingState(t *testing.T) {
	for _, name := range []string{"missing file", "missing key", "non-postgres", "empty env key"} {
		t.Run(name, func(t *testing.T) {
			_, worktree := newDatabaseWorktree(t)
			if name == "missing key" {
				require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte("OTHER=1\n"), 0o600))
			} else if name == "non-postgres" {
				require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte("DATABASE_URL=mysql://app@host/db\n"), 0o600))
			}
			calls := 0
			backend := countingBackend{calls: &calls}
			opts := SetupOptions{WorktreePath: worktree, Branch: "feature/test", EnvKey: "DATABASE_URL"}
			if name == "empty env key" {
				opts.EnvKey = ""
			}
			result, err := setupDatabase(backend, opts)
			require.NoError(t, err)
			assert.True(t, result.Skipped)
			assert.Equal(t, 0, calls)
			commonDir, err := git.CommonDir(worktree)
			require.NoError(t, err)
			assert.NoDirExists(t, filepath.Join(commonDir, databaseStateDirectory))
		})
	}
}

func TestSetupCreationRefusesActiveOwnershipBeforeReadingConfig(t *testing.T) {
	worktree, _ := activateWorktreeDatabase(t, "")
	require.NoError(t, os.Remove(filepath.Join(worktree, ".env")))
	record := loadTestRecord(t, worktree)
	calls := 0

	_, err := setupDatabase(countingBackend{calls: &calls}, SetupOptions{
		WorktreePath: worktree,
		Branch:       "feature/test",
	})

	require.ErrorContains(t, err, "already exists")
	assert.Equal(t, 0, calls)
	after := loadTestRecord(t, worktree)
	assert.Equal(t, record.UpdatedAt, after.UpdatedAt)
}

type countingResolver struct{ calls *int }

type countingBackend struct{ calls *int }

func (b countingBackend) Snapshot() (ContainerResolver, error) {
	(*b.calls)++
	return countingResolver{calls: b.calls}, nil
}

func (b countingBackend) Create(string, string, string) error {
	(*b.calls)++
	return nil
}

func (b countingBackend) Drop(string, string, []string) error {
	(*b.calls)++
	return nil
}

func (b countingBackend) Exists(string, string, string) (bool, error) {
	(*b.calls)++
	return true, nil
}

func (r countingResolver) Resolve(string, string, string) (ContainerTarget, error) {
	(*r.calls)++
	return ContainerTarget{}, nil
}

func (r countingResolver) ResolveID(string) (ContainerTarget, error) {
	(*r.calls)++
	return ContainerTarget{}, nil
}
