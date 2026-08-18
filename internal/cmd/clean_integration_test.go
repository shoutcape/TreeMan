package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
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

	changeToDir(t, repo)
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	require.NoError(t, runClean(cmd, false, false))

	_, err := os.Stat(worktree)
	require.NoError(t, err)
	runGitInDir(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/feature")
}

func TestCleanRemovesCurrentMergedWorktreeAndPrintsMainRoot(t *testing.T) {
	repo, worktree := createMergedCleanWorktree(t)
	changeToDir(t, worktree)

	output := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(output)
	require.NoError(t, runClean(cmd, false, true))

	cleanOutput := ui.StripANSI(output.String())
	assert.Contains(t, cleanOutput, "feature")
	assert.Contains(t, cleanOutput, worktree)
	assert.Contains(t, cleanOutput, repo+"\n")
	_, err := os.Stat(worktree)
	require.ErrorIs(t, err, os.ErrNotExist)
	checkBranch := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/feature")
	checkBranch.Dir = repo
	require.Error(t, checkBranch.Run())
}

func TestCleanDeclineLeavesCandidatesUntouched(t *testing.T) {
	repo, worktree := createMergedCleanWorktree(t)
	changeToDir(t, repo)

	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("n\n"))
	cmd.SetOut(&bytes.Buffer{})
	require.NoError(t, runClean(cmd, false, false))

	_, err := os.Stat(worktree)
	require.NoError(t, err)
	runGitInDir(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/feature")
}

func TestCleanYesRemovesCandidates(t *testing.T) {
	repo, worktree := createMergedCleanWorktree(t)
	changeToDir(t, repo)

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	require.NoError(t, runClean(cmd, false, true))

	_, err := os.Stat(worktree)
	require.True(t, os.IsNotExist(err))
	command := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/feature")
	command.Dir = repo
	require.Error(t, command.Run())
}

func TestCleanRemovesEmptyBranchCreatedFromNewerOriginMain(t *testing.T) {
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

	updater := filepath.Join(t.TempDir(), "updater")
	runGit(t, "clone", origin, updater)
	runGitInDir(t, updater, "config", "user.email", "test@example.com")
	runGitInDir(t, updater, "config", "user.name", "Test User")
	require.NoError(t, os.WriteFile(filepath.Join(updater, "file"), []byte("origin update\n"), 0o644))
	runGitInDir(t, updater, "commit", "-am", "origin update")
	runGitInDir(t, updater, "push", "origin", "main")

	// Keep local main stale while creating an empty branch from the refreshed remote.
	runGitInDir(t, repo, "fetch", "origin", "refs/heads/main:refs/remotes/origin/main")
	worktree := filepath.Join(t.TempDir(), "empty-worktree")
	runGitInDir(t, repo, "worktree", "add", "--no-track", "-b", "empty", worktree, "origin/main")
	changeToDir(t, repo)

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	require.NoError(t, runClean(cmd, false, true))

	_, err := os.Stat(worktree)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCleanDryRunPreviewsCandidatesWithoutRemoving(t *testing.T) {
	repo, worktree := createMergedCleanWorktree(t)
	changeToDir(t, repo)

	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&output)
	require.NoError(t, runClean(cmd, true, false))

	cleanOutput := ui.StripANSI(output.String())
	assert.Contains(t, cleanOutput, "Cleanup candidates")
	assert.Contains(t, cleanOutput, "Merged, clean worktrees and branches to remove")
	assert.Contains(t, cleanOutput, "BRANCH")
	assert.Contains(t, cleanOutput, "WORKTREE")
	assert.Contains(t, cleanOutput, "feature")
	assert.Contains(t, cleanOutput, worktree)
	_, err := os.Stat(worktree)
	require.NoError(t, err)
	runGitInDir(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/feature")
}

func TestCleanRemovesSquashMergedCleanWorktree(t *testing.T) {
	repo, worktree := createSquashMergedCleanWorktree(t)
	changeToDir(t, repo)

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	require.NoError(t, runClean(cmd, false, true))

	_, err := os.Stat(worktree)
	require.True(t, os.IsNotExist(err), "squash-merged worktree should have been removed")
	command := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/feature")
	command.Dir = repo
	require.Error(t, command.Run(), "local branch should have been deleted")
}

func createSquashMergedCleanWorktree(t *testing.T) (string, string) {
	t.Helper()
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
	require.NoError(t, os.WriteFile(filepath.Join(repo, "file"), []byte("feature\n"), 0o644))
	runGitInDir(t, repo, "commit", "-am", "feature work")
	runGitInDir(t, repo, "push", "-u", "origin", "feature")
	runGitInDir(t, repo, "checkout", "main")
	worktree := filepath.Join(t.TempDir(), "feature-worktree")
	runGitInDir(t, repo, "worktree", "add", worktree, "feature")

	// Squash-merge via a second clone: new commit on main, feature tip not an ancestor.
	squasher := filepath.Join(t.TempDir(), "squasher")
	runGit(t, "clone", origin, squasher)
	runGitInDir(t, squasher, "config", "user.email", "test@example.com")
	runGitInDir(t, squasher, "config", "user.name", "Test User")
	runGitInDir(t, squasher, "merge", "--squash", "origin/feature")
	runGitInDir(t, squasher, "commit", "-m", "squash merge feature")
	runGitInDir(t, squasher, "push", "origin", "main")
	// Delete the remote branch (GitLab does this automatically after merge).
	runGitInDir(t, squasher, "push", "origin", "--delete", "feature")

	return repo, worktree
}

func createMergedCleanWorktree(t *testing.T) (string, string) {
	t.Helper()
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
	return repo, worktree
}

func changeToDir(t *testing.T, dir string) {
	t.Helper()
	previousDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { require.NoError(t, os.Chdir(previousDir)) })
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
