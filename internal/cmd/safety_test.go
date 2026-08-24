package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shoutcape/treeman/internal/database"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunDeleteDirect_RejectsDirtyWorktreeWithoutForce(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/dirty")
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "untracked.txt"), []byte("changed\n"), 0o644))
	chdirForTest(t, repo)

	err := runDeleteDirect(&cobra.Command{}, worktree, "feature/dirty", true, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "use --force")
	assert.DirExists(t, worktree)
	gitTest(t, repo, "show-ref", "--verify", "refs/heads/feature/dirty")
}

func TestRunDeleteDirect_RejectsTrackedAndStagedChangesWithoutForce(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(t *testing.T, worktree string)
	}{
		{
			name: "tracked",
			prepare: func(t *testing.T, worktree string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(worktree, "README.md"), []byte("modified\n"), 0o644))
			},
		},
		{
			name: "staged",
			prepare: func(t *testing.T, worktree string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(worktree, "staged.txt"), []byte("staged\n"), 0o644))
				gitTest(t, worktree, "add", "staged.txt")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, worktree := createTestWorktree(t, "feature/"+test.name)
			test.prepare(t, worktree)
			chdirForTest(t, repo)

			err := runDeleteDirect(&cobra.Command{}, worktree, "feature/"+test.name, true, false)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "use --force")
			assert.DirExists(t, worktree)
		})
	}
}

func TestRunDeleteDirect_RejectsInvalidDirectTargets(t *testing.T) {
	t.Run("missing flags", func(t *testing.T) {
		err := runDeleteDirect(&cobra.Command{}, "", "feature/test", true, false)
		require.EqualError(t, err, "--path and --branch are both required in non-interactive mode")
	})

	t.Run("unknown path", func(t *testing.T) {
		repo, _ := createTestWorktree(t, "feature/known")
		chdirForTest(t, repo)

		err := runDeleteDirect(&cobra.Command{}, filepath.Join(repo, "missing"), "feature/known", true, true)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "is not a linked worktree")
	})

	t.Run("main worktree", func(t *testing.T) {
		repo, _ := createTestWorktree(t, "feature/linked")
		chdirForTest(t, repo)

		err := runDeleteDirect(&cobra.Command{}, repo, "main", true, true)

		require.EqualError(t, err, "cannot delete the main worktree")
		assert.DirExists(t, repo)
	})
}

func TestRunDeleteDirect_ForceRemovesDirtyWorktreeAndBranch(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/remove")
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "untracked.txt"), []byte("changed\n"), 0o644))
	chdirForTest(t, repo)

	require.NoError(t, runDeleteDirect(&cobra.Command{}, worktree, "feature/remove", true, true))
	assert.NoDirExists(t, worktree)
	gitTestFails(t, repo, "show-ref", "--verify", "refs/heads/feature/remove")
}

func TestRunDeleteDirect_PreservesBranchUsedByAnotherWorktree(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/shared")
	otherWorktree := filepath.Join(filepath.Dir(repo), "other-worktree")
	gitTest(t, repo, "worktree", "add", "--force", otherWorktree, "feature/shared")
	chdirForTest(t, repo)

	err := runDeleteDirect(&cobra.Command{}, worktree, "feature/shared", true, true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `branch "feature/shared" was preserved after deletion checks`)
	assert.Contains(t, err.Error(), `branch "feature/shared" is still checked out at worktree "`+otherWorktree+`"`)
	assert.NoDirExists(t, worktree)
	assert.DirExists(t, otherWorktree)
	gitTest(t, repo, "show-ref", "--verify", "refs/heads/feature/shared")
}

func TestRunDeleteDirect_ReportsWorktreeRemovalFailure(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/remove-failure")
	chdirForTest(t, repo)
	restoreRemove := removeWorktree
	removeWorktree = func(string, bool) error { return assert.AnError }
	t.Cleanup(func() { removeWorktree = restoreRemove })

	err := runDeleteDirect(&cobra.Command{}, worktree, "feature/remove-failure", true, false)

	require.Error(t, err)
	assert.ErrorIs(t, err, assert.AnError)
	assert.Contains(t, err.Error(), "Completed: none.")
	assert.Contains(t, err.Error(), `Remaining: worktree "`+worktree+`", branch "feature/remove-failure".`)
	assert.Contains(t, err.Error(), `Recovery: resolve the error, then retry: treeman delete --path "`+worktree+`" --branch "feature/remove-failure" --yes`)
	assert.DirExists(t, worktree)
	gitTest(t, repo, "show-ref", "--verify", "refs/heads/feature/remove-failure")
}

func TestRunDeleteDirect_ReportsBranchDeletionFailure(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/branch-failure")
	chdirForTest(t, repo)
	restoreDelete := deleteBranchAtSHA
	deleteBranchAtSHA = func(string, string, string) error { return assert.AnError }
	t.Cleanup(func() { deleteBranchAtSHA = restoreDelete })

	err := runDeleteDirect(&cobra.Command{}, worktree, "feature/branch-failure", true, true)

	require.Error(t, err)
	assert.ErrorIs(t, err, assert.AnError)
	assert.Contains(t, err.Error(), `Completed: removed worktree "`+worktree+`".`)
	assert.Contains(t, err.Error(), `Remaining: branch "feature/branch-failure".`)
	assert.Contains(t, err.Error(), `branch "feature/branch-failure" was preserved after deletion checks`)
	assert.Contains(t, err.Error(), `Recovery: inspect branch "feature/branch-failure"`)
	assert.NoDirExists(t, worktree)
	gitTest(t, repo, "show-ref", "--verify", "refs/heads/feature/branch-failure")
}

func TestRunDeleteDirect_DoesNotDropDatabaseWhenBranchDeletionFails(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/database-branch-failure")
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte("[database]\nenv_key = \"DATABASE_URL\"\n"), 0o644))
	chdirForTest(t, repo)

	previousPrepare := prepareBranchDBCleanup
	previousExecute := executeBranchDBCleanup
	previousDelete := deleteBranchAtSHA
	prepared := 0
	executed := 0
	prepareBranchDBCleanup = func(string, string) (*database.CleanupPlan, error) {
		prepared++
		return &database.CleanupPlan{}, nil
	}
	executeBranchDBCleanup = func(*database.CleanupPlan) error {
		executed++
		return nil
	}
	deleteBranchAtSHA = func(string, string, string) error { return assert.AnError }
	t.Cleanup(func() {
		prepareBranchDBCleanup = previousPrepare
		executeBranchDBCleanup = previousExecute
		deleteBranchAtSHA = previousDelete
	})

	err := runDeleteDirect(&cobra.Command{}, worktree, "feature/database-branch-failure", true, true)

	require.ErrorIs(t, err, assert.AnError)
	assert.Equal(t, 1, prepared)
	assert.Zero(t, executed)
}

func TestRunDeleteDirect_DropsDatabaseOnlyAfterBranchDeletion(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/database-success")
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte("[database]\nenv_key = \"DATABASE_URL\"\n"), 0o644))
	chdirForTest(t, repo)

	previousPrepare := prepareBranchDBCleanup
	previousExecute := executeBranchDBCleanup
	previousDelete := deleteBranchAtSHA
	executed := false
	prepareBranchDBCleanup = func(string, string) (*database.CleanupPlan, error) {
		return &database.CleanupPlan{}, nil
	}
	deleteBranchAtSHA = func(dir, branch, sha string) error {
		assert.False(t, executed, "database cleanup must follow branch deletion")
		return git.DeleteBranchAtSHA(dir, branch, sha)
	}
	executeBranchDBCleanup = func(*database.CleanupPlan) error {
		executed = true
		return nil
	}
	t.Cleanup(func() {
		prepareBranchDBCleanup = previousPrepare
		executeBranchDBCleanup = previousExecute
		deleteBranchAtSHA = previousDelete
	})

	require.NoError(t, runDeleteDirect(&cobra.Command{}, worktree, "feature/database-success", true, true))
	assert.True(t, executed)
}

func TestDeleteWorktreeAtSHARetainsWorktreeWhenBranchMovedBeforeRemoval(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/verified")
	expectedSHA := gitRevParse(t, repo, "refs/heads/feature/verified")
	advanceMainForTest(t, repo)
	gitTest(t, repo, "update-ref", "refs/heads/feature/verified", "refs/heads/main")
	chdirForTest(t, repo)

	err := deleteWorktreeAtSHA(&cobra.Command{}, worktree, "feature/verified", repo, true, true, expectedSHA)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "moved after merge verification")
	assert.DirExists(t, worktree)
	assert.Equal(t, gitRevParse(t, repo, "refs/heads/main"), gitRevParse(t, repo, "refs/heads/feature/verified"))
}

func TestDeleteWorktreeAtSHARequiresExpectedSHAWhenSkippingMergeCheck(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/verified")
	chdirForTest(t, repo)

	err := deleteWorktreeAtSHA(&cobra.Command{}, worktree, "feature/verified", repo, true, true, "")

	require.EqualError(t, err, "cannot skip merge check for branch \"feature/verified\" without an expected SHA")
	assert.DirExists(t, worktree)
	runGitInDir(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/feature/verified")
}

func TestDeleteWorktreeAtSHAPreservesBranchOnCompareAndDeleteMismatch(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/verified")
	expectedSHA := gitRevParse(t, repo, "refs/heads/feature/verified")
	movedSHA := advanceMainForTest(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte("[database]\nenv_key = \"DATABASE_URL\"\n"), 0o644))
	chdirForTest(t, repo)

	originalRemove := removeWorktree
	originalPrepare := prepareBranchDBCleanup
	originalExecute := executeBranchDBCleanup
	databaseCleaned := false
	removeWorktree = func(path string, force bool) error {
		if err := originalRemove(path, force); err != nil {
			return err
		}
		gitTest(t, repo, "update-ref", "refs/heads/feature/verified", movedSHA)
		return nil
	}
	prepareBranchDBCleanup = func(string, string) (*database.CleanupPlan, error) {
		return &database.CleanupPlan{}, nil
	}
	executeBranchDBCleanup = func(*database.CleanupPlan) error {
		databaseCleaned = true
		return nil
	}
	t.Cleanup(func() {
		removeWorktree = originalRemove
		prepareBranchDBCleanup = originalPrepare
		executeBranchDBCleanup = originalExecute
	})

	err := deleteWorktreeAtSHA(&cobra.Command{}, worktree, "feature/verified", repo, false, true, expectedSHA)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Completed: removed worktree")
	assert.Contains(t, err.Error(), "branch \"feature/verified\" was preserved after deletion checks")
	assert.Contains(t, err.Error(), "Recovery: inspect branch")
	assert.NoDirExists(t, worktree)
	assert.Equal(t, movedSHA, gitRevParse(t, repo, "refs/heads/feature/verified"))
	assert.False(t, databaseCleaned)
}

func TestRunDeleteDirect_PreservesBranchMovedDuringDeletion(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(fmt.Sprintf("force=%t", force), func(t *testing.T) {
			repo, worktree := createTestWorktree(t, "feature/moved")
			gitTest(t, repo, "checkout", "-b", "replacement")
			require.NoError(t, os.WriteFile(filepath.Join(repo, "replacement.txt"), []byte("replacement\n"), 0o644))
			gitTest(t, repo, "add", "replacement.txt")
			gitTest(t, repo, "commit", "-m", "replacement work")
			replacementSHA := gitRevParse(t, repo, "refs/heads/replacement")
			gitTest(t, repo, "checkout", "main")
			chdirForTest(t, repo)

			originalRemove := removeWorktree
			removeWorktree = func(path string, removeForce bool) error {
				if err := originalRemove(path, removeForce); err != nil {
					return err
				}
				gitTest(t, repo, "update-ref", "refs/heads/feature/moved", replacementSHA)
				return nil
			}
			t.Cleanup(func() { removeWorktree = originalRemove })

			err := runDeleteDirect(&cobra.Command{}, worktree, "feature/moved", true, force)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "was preserved after deletion checks")
			assert.NoDirExists(t, worktree)
			assert.Equal(t, replacementSHA, gitRevParse(t, repo, "refs/heads/feature/moved"))
		})
	}
}

func TestRunDeleteDirect_PrintsMainWorktreeWhenDeletingCurrentWorktree(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/current")
	chdirForTest(t, worktree)
	buf := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(buf)

	require.NoError(t, runDeleteDirect(cmd, worktree, "feature/current", true, true))
	assert.Equal(t, repo+"\n", buf.String())
	require.NoError(t, os.Chdir(repo))
}

func TestRunDeleteDirect_RejectsMismatchedBranch(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/identity")
	chdirForTest(t, repo)

	err := runDeleteDirect(&cobra.Command{}, worktree, "wrong-branch", true, true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "checked out on branch")
	assert.DirExists(t, worktree)
}

func TestRunDeleteDirect_RejectsDeletionWhenDefaultBranchDetectionFails(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/no-origin")
	gitTest(t, repo, "remote", "remove", "origin")
	chdirForTest(t, repo)

	err := runDeleteDirect(&cobra.Command{}, worktree, "feature/no-origin", true, true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "default branch could not be detected")
	assert.DirExists(t, worktree)
	gitTest(t, repo, "show-ref", "--verify", "refs/heads/feature/no-origin")
}

func TestRunDeleteDirect_RejectsKnownDefaultBranch(t *testing.T) {
	repo, _ := createTestWorktree(t, "feature/linked")
	worktree := filepath.Join(filepath.Dir(repo), "default-worktree")
	gitTest(t, repo, "branch", "master")
	gitTest(t, repo, "push", "origin", "master")
	gitTest(t, filepath.Join(filepath.Dir(repo), "origin.git"), "config", "receive.denyDeleteCurrent", "ignore")
	gitTest(t, repo, "push", "origin", "--delete", "main")
	gitTest(t, repo, "worktree", "add", worktree, "master")
	chdirForTest(t, repo)

	err := runDeleteDirect(&cobra.Command{}, worktree, "master", true, true)

	require.EqualError(t, err, "cannot delete the default branch \"master\"")
	assert.DirExists(t, worktree)
	gitTest(t, repo, "show-ref", "--verify", "refs/heads/master")
}

func TestRunDeleteDirect_RejectsUnmergedBranchBeforeRemovingWorktree(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/unmerged")
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "feature.txt"), []byte("feature\n"), 0o644))
	gitTest(t, worktree, "add", "feature.txt")
	gitTest(t, worktree, "commit", "-m", "feature work")
	chdirForTest(t, repo)

	err := runDeleteDirect(&cobra.Command{}, worktree, "feature/unmerged", true, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not fully merged")
	assert.DirExists(t, worktree)
	gitTest(t, repo, "show-ref", "--verify", "refs/heads/feature/unmerged")
}

func TestRunList_ReportsRepositoryState(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/list")
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "untracked.txt"), []byte("changed\n"), 0o644))
	gitTest(t, worktree, "checkout", "--detach")
	chdirForTest(t, repo)

	buf := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(buf)
	require.NoError(t, runList(cmd, true))

	var entries []listEntry
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entries))
	require.Len(t, entries, 2)
	assert.Equal(t, listEntry{Path: repo, Branch: "main", Main: true, Current: true}, entries[0])
	assert.Equal(t, listEntry{Path: worktree, Dirty: true, Detached: true}, entries[1])
}

func TestRunList_OutsideRepository(t *testing.T) {
	chdirForTest(t, t.TempDir())

	err := runList(&cobra.Command{}, true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not list worktrees")
}

func createTestWorktree(t *testing.T, branch string) (string, string) {
	t.Helper()
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	require.NoError(t, os.Mkdir(repo, 0o755))
	gitTest(t, repo, "init", "-b", "main")
	gitTest(t, repo, "config", "user.name", "TreeMan Test")
	gitTest(t, repo, "config", "user.email", "test@example.com")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "README.md"), []byte("initial\n"), 0o644))
	gitTest(t, repo, "add", "README.md")
	gitTest(t, repo, "commit", "-m", "initial")
	origin := filepath.Join(parent, "origin.git")
	gitTest(t, parent, "init", "--bare", origin)
	gitTest(t, repo, "remote", "add", "origin", origin)
	gitTest(t, repo, "push", "-u", "origin", "main")

	worktree := filepath.Join(parent, "worktree")
	gitTest(t, repo, "worktree", "add", "-b", branch, worktree)
	return repo, worktree
}

func chdirForTest(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { require.NoError(t, os.Chdir(previous)) })
}

func gitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %v failed: %s", args, output)
}

func gitTestFails(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	require.Errorf(t, err, "git %v unexpectedly succeeded: %s", args, output)
}

func advanceMainForTest(t *testing.T, repo string) string {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(repo, "moved.txt"), []byte("moved\n"), 0o644))
	gitTest(t, repo, "add", "moved.txt")
	gitTest(t, repo, "commit", "-m", "moved")
	return gitRevParse(t, repo, "refs/heads/main")
}
