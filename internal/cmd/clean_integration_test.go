package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestCleanRetainsMergedBranchWithUnpushedCommit(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	runGit(t, "init", "--bare", "--initial-branch=main", origin)

	repo := filepath.Join(t.TempDir(), "repo")
	runGit(t, "clone", origin, repo)
	runGitInDir(t, repo, "config", "user.email", "test@example.com")
	runGitInDir(t, repo, "config", "user.name", "Test User")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "file"), []byte("base\n"), 0o644))
	runGitInDir(t, repo, "add", "file")
	runGitInDir(t, repo, "commit", "-m", "base")
	runGitInDir(t, repo, "push", "origin", "main")

	runGitInDir(t, repo, "checkout", "-b", "feature")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "file"), []byte("merged\n"), 0o644))
	runGitInDir(t, repo, "commit", "-am", "feature")
	runGitInDir(t, repo, "push", "-u", "origin", "feature")
	runGitInDir(t, repo, "checkout", "main")
	runGitInDir(t, repo, "merge", "--ff-only", "feature")
	runGitInDir(t, repo, "push", "origin", "main")

	worktree := filepath.Join(t.TempDir(), "feature-worktree")
	runGitInDir(t, repo, "worktree", "add", worktree, "feature")
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "file"), []byte("local-only\n"), 0o644))
	runGitInDir(t, worktree, "commit", "-am", "local-only")

	previousDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repo))
	t.Cleanup(func() { require.NoError(t, os.Chdir(previousDir)) })

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	require.NoError(t, runClean(cmd, false))

	_, err = os.Stat(worktree)
	require.NoError(t, err)
	runGitInDir(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/feature")
}

func runGit(t *testing.T, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	output, err := command.CombinedOutput()
	require.NoErrorf(t, err, "git %v failed: %s", args, output)
}

func runGitInDir(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	require.NoErrorf(t, err, "git %v failed: %s", args, output)
}
