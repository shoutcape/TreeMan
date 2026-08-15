package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

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

	require.EqualError(t, err, "not inside a git repository")
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
