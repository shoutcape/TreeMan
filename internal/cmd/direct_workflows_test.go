package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunBranchDirectDoesNotRequireForgeCLI(t *testing.T) {
	parent := t.TempDir()
	remote := filepath.Join(parent, "remote.git")
	repo := filepath.Join(parent, "repo")
	gitTest(t, parent, "init", "--bare", remote)
	gitTest(t, parent, "init", "-b", "main", repo)
	gitTest(t, repo, "config", "user.name", "TreeMan Test")
	gitTest(t, repo, "config", "user.email", "test@example.com")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "README.md"), []byte("initial\n"), 0o644))
	gitTest(t, repo, "add", "README.md")
	gitTest(t, repo, "commit", "-m", "initial")
	gitTest(t, repo, "remote", "add", "origin", remote)
	gitTest(t, repo, "push", "-u", "origin", "main")
	gitTest(t, repo, "checkout", "-b", "feature/direct")
	gitTest(t, repo, "push", "-u", "origin", "feature/direct")
	gitTest(t, repo, "checkout", "main")
	gitTest(t, repo, "branch", "-D", "feature/direct")
	chdirForTest(t, repo)
	pathWithOnlyGit(t)

	buf := &bytes.Buffer{}
	command := &cobra.Command{}
	command.SetOut(buf)
	require.NoError(t, runBranch(command, "feature/direct"))

	worktree := filepath.Join(repo, ".worktrees", "feature-direct")
	assert.Equal(t, worktree+"\n", buf.String())
	assert.DirExists(t, worktree)
}

func TestRunSwitchDirectMatchesBranchAndPathWithoutFzf(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/direct")
	chdirForTest(t, repo)
	pathWithOnlyGit(t)

	for _, query := range []string{"feature/direct", worktree} {
		t.Run(query, func(t *testing.T) {
			buf := &bytes.Buffer{}
			command := &cobra.Command{}
			command.SetOut(buf)

			require.NoError(t, runSwitch(command, query))
			assert.Equal(t, worktree+"\n", buf.String())
		})
	}
}

func pathWithOnlyGit(t *testing.T) {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	require.NoError(t, err)
	binDir := t.TempDir()
	require.NoError(t, os.Symlink(gitPath, filepath.Join(binDir, "git")))
	previous := os.Getenv("PATH")
	require.NoError(t, os.Setenv("PATH", binDir))
	t.Cleanup(func() { require.NoError(t, os.Setenv("PATH", previous)) })
}
