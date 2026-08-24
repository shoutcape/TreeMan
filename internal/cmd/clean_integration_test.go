package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shoutcape/treeman/internal/merge"
	"github.com/shoutcape/treeman/internal/terminal"
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
	stderr := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(output)
	cmd.SetErr(stderr)
	require.NoError(t, runClean(cmd, false, true))

	cleanOutput := ui.StripANSI(stderr.String())
	assert.Contains(t, cleanOutput, "feature")
	assert.Contains(t, cleanOutput, worktree)
	assert.Contains(t, cleanOutput, "Removed 1 merged, clean worktree(s).")
	assert.Equal(t, repo+"\n", output.String())
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
	previousCapabilities := terminalCapabilities
	terminalCapabilities = func(io.Reader, io.Writer) terminal.Capabilities {
		return terminal.Capabilities{Interactive: true}
	}
	t.Cleanup(func() { terminalCapabilities = previousCapabilities })
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

	output := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(output)
	require.NoError(t, runClean(cmd, false, true))

	assert.Empty(t, output.String())
	_, err := os.Stat(worktree)
	require.True(t, os.IsNotExist(err))
	command := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/feature")
	command.Dir = repo
	require.Error(t, command.Run())
}

func TestCleanDeletesOnlyRevalidatedCandidates(t *testing.T) {
	repo, worktree := createMergedCleanWorktree(t)
	changeToDir(t, repo)

	calls := 0
	classifier := merge.ClassifierFunc(func(_ string, branches []string) (merge.Result, error) {
		calls++
		sha := gitRevParse(t, repo, "refs/heads/feature")
		if calls == 2 {
			return merge.Result{}, nil
		}
		return merge.Result{Cleanable: []merge.Candidate{{Branch: branches[0], SHA: sha}}}, nil
	})

	require.NoError(t, runCleanWithClassifier(&cobra.Command{}, classifier, false, true))
	assert.Equal(t, 2, calls)
	assert.DirExists(t, worktree)
	runGitInDir(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/feature")
}

func TestCleanRejectsCleanableBranchWithoutSHA(t *testing.T) {
	repo, worktree := createMergedCleanWorktree(t)
	changeToDir(t, repo)
	classifier := merge.ClassifierFunc(func(_ string, branches []string) (merge.Result, error) {
		return merge.Result{Cleanable: []merge.Candidate{{Branch: branches[0]}}}, nil
	})

	err := runCleanWithClassifier(&cobra.Command{}, classifier, false, true)

	require.EqualError(t, err, "classifier returned cleanable branch \"feature\" without a SHA")
	assert.DirExists(t, worktree)
	runGitInDir(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/feature")
}

func TestCleanRejectsUnknownCleanableBranch(t *testing.T) {
	repo, worktree := createMergedCleanWorktree(t)
	changeToDir(t, repo)
	classifier := merge.ClassifierFunc(func(_ string, _ []string) (merge.Result, error) {
		return merge.Result{Cleanable: []merge.Candidate{{Branch: "other", SHA: "abc123"}}}, nil
	})

	err := runCleanWithClassifier(&cobra.Command{}, classifier, false, true)

	require.EqualError(t, err, "classifier returned unknown cleanable branch \"other\"")
	assert.DirExists(t, worktree)
	runGitInDir(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/feature")
}

func TestCleanSkipsFetchWhenDefaultBranchIsCurrent(t *testing.T) {
	repo, worktree := createMergedCleanWorktree(t)
	runGitInDir(t, repo, "fetch", "origin", "refs/heads/main:refs/remotes/origin/main")
	changeToDir(t, repo)
	commands := traceGitCommands(t, false)

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	require.NoError(t, runClean(cmd, true, true))

	assert.DirExists(t, worktree)
	assert.Equal(t, 1, countGitCommands(commands(), "ls-remote --heads origin refs/heads/main refs/heads/feature"))
	assert.Zero(t, countGitCommands(commands(), "fetch origin refs/heads/main:refs/remotes/origin/main"))
}

func TestCleanSkipsRemoteClassificationWithoutLinkedWorktrees(t *testing.T) {
	repo, worktree := createMergedCleanWorktree(t)
	runGitInDir(t, repo, "worktree", "remove", worktree)
	changeToDir(t, repo)
	commands := traceGitCommands(t, false)

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	require.NoError(t, runClean(cmd, false, true))

	assert.Zero(t, countGitCommands(commands(), "ls-remote"))
}

func TestCleanFetchesChangedDefaultBranchBeforeDeleting(t *testing.T) {
	repo, worktree := createMergedCleanWorktree(t)
	updater := filepath.Join(t.TempDir(), "updater")
	originURL := exec.Command("git", "-C", repo, "remote", "get-url", "origin")
	originOutput, err := originURL.Output()
	require.NoError(t, err)
	runGit(t, "clone", strings.TrimSpace(string(originOutput)), updater)
	runGitInDir(t, updater, "config", "user.email", "test@example.com")
	runGitInDir(t, updater, "config", "user.name", "Test User")
	require.NoError(t, os.WriteFile(filepath.Join(updater, "next"), []byte("next\n"), 0o644))
	runGitInDir(t, updater, "add", "next")
	runGitInDir(t, updater, "commit", "-m", "advance main")
	runGitInDir(t, updater, "push", "origin", "main")

	changeToDir(t, repo)
	commands := traceGitCommands(t, false)
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	require.NoError(t, runClean(cmd, false, true))

	assert.NoDirExists(t, worktree)
	assert.Equal(t, 2, countGitCommands(commands(), "ls-remote --heads origin refs/heads/main refs/heads/feature"))
	assert.Equal(t, 1, countGitCommands(commands(), "fetch origin refs/heads/main:refs/remotes/origin/main"))
}

func TestCleanRefusesDeletionWhenRemoteStateFails(t *testing.T) {
	repo, worktree := createMergedCleanWorktree(t)
	changeToDir(t, repo)
	traceGitCommands(t, true)

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	require.Error(t, runClean(cmd, false, true))
	assert.DirExists(t, worktree)
	runGitInDir(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/feature")
}

func TestCleanRefusesDeletionWhenGitHubSnapshotAndGitFallbackFail(t *testing.T) {
	repo, worktree := createMergedCleanWorktree(t)
	traceGitHubAPI(t, `not-json`, `[]`)
	t.Setenv("_TREEMAN_REMOTE_URL", "https://github.com/owner/repo.git")
	changeToDir(t, repo)
	traceGitCommands(t, true)

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	require.Error(t, runClean(cmd, false, true))
	assert.DirExists(t, worktree)
	runGitInDir(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/feature")
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
	cmd.SetErr(&output)
	require.NoError(t, runClean(cmd, true, false))

	cleanOutput := ui.StripANSI(output.String())
	assert.NotContains(t, output.String(), "\x1b")
	assert.Contains(t, cleanOutput, "Cleanup candidates")
	assert.Contains(t, cleanOutput, "Merged, clean worktrees and branches to remove")
	assert.Contains(t, cleanOutput, "BRANCH")
	assert.Contains(t, cleanOutput, "WORKTREE")
	assert.Contains(t, cleanOutput, "feature")
	assert.Contains(t, cleanOutput, worktree)
	assert.Contains(t, cleanOutput, "  feature  "+worktree)
	assert.Contains(t, cleanOutput, "Would remove 1 merged, clean worktree(s).")
	_, err := os.Stat(worktree)
	require.NoError(t, err)
	runGitInDir(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/feature")
}

func TestCleanRetainsRemoteDeletedUnmergedWorktree(t *testing.T) {
	repo, worktree := createRemoteDeletedUnmergedCleanWorktree(t)
	changeToDir(t, repo)

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	require.NoError(t, runClean(cmd, false, true))

	_, err := os.Stat(worktree)
	require.NoError(t, err, "unmerged worktree should not be removed")
	runGitInDir(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/feature")
}

func TestCleanRemovesForgeVerifiedSquashMergedWorktree(t *testing.T) {
	repo, worktree := createSquashMergedCleanWorktree(t)
	mergedHeadSHA := gitRevParse(t, repo, "refs/heads/feature")
	classifier := stubForgeVerifier(t, []string{mergedHeadSHA}, nil)
	changeToDir(t, repo)

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	require.NoError(t, runCleanWithClassifier(cmd, classifier, false, true))

	_, err := os.Stat(worktree)
	require.True(t, os.IsNotExist(err), "forge-verified squash-merged worktree should have been removed")
	command := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/feature")
	command.Dir = repo
	require.Error(t, command.Run(), "local branch should have been deleted")
}

func TestCleanRemovesGitHubSnapshotVerifiedSquashMergedWorktree(t *testing.T) {
	repo, worktree := createSquashMergedCleanWorktree(t)
	runGitInDir(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	mainSHA := originBranchSHA(t, repo, "main")
	featureSHA := gitRevParse(t, repo, "refs/heads/feature")
	commit := fmt.Sprintf(`{"associatedPullRequests":{"nodes":[{"merged":true,"baseRefName":"main","headRefName":"feature","headRefOid":%q}],"pageInfo":{"hasNextPage":false}}}`, featureSHA)
	githubCommands := traceGitHubAPI(t, githubSnapshotResponse(mainSHA, featureSHA, commit, false), `[]`)
	t.Setenv("_TREEMAN_REMOTE_URL", "https://github.com/owner/repo.git")
	changeToDir(t, repo)
	gitCommands := traceGitCommands(t, false)

	require.NoError(t, runClean(&cobra.Command{}, false, true))

	assert.NoDirExists(t, worktree)
	assert.Equal(t, 2, countGitHubCommands(githubCommands(), "api graphql"))
	assert.Zero(t, countGitHubCommands(githubCommands(), "api repos/"))
	assert.Zero(t, countGitCommands(gitCommands(), "ls-remote"))
}

func TestCleanVerifiesEachRemoteGoneBranch(t *testing.T) {
	repo, featureWorktree := createSquashMergedCleanWorktree(t)
	featureSHA := gitRevParse(t, repo, "refs/heads/feature")

	runGitInDir(t, repo, "checkout", "-b", "feature-two")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "second-feature"), []byte("second\n"), 0o644))
	runGitInDir(t, repo, "add", "second-feature")
	runGitInDir(t, repo, "commit", "-m", "second feature")
	runGitInDir(t, repo, "push", "-u", "origin", "feature-two")
	runGitInDir(t, repo, "checkout", "main")
	secondWorktree := filepath.Join(t.TempDir(), "feature-two-worktree")
	runGitInDir(t, repo, "worktree", "add", secondWorktree, "feature-two")
	runGitInDir(t, repo, "push", "origin", "--delete", "feature-two")
	secondSHA := gitRevParse(t, repo, "refs/heads/feature-two")

	var lookupCalls atomic.Int32
	classifier := merge.ClassifierFunc(func(defaultBranch string, branches []string) (merge.Result, error) {
		assert.Equal(t, "main", defaultBranch)
		cleanable := make([]merge.Candidate, 0, len(branches))
		for _, branch := range branches {
			sha := gitRevParse(t, repo, "refs/heads/"+branch)
			lookupCalls.Add(1)
			if (branch == "feature" && sha == featureSHA) || (branch == "feature-two" && sha == secondSHA) {
				cleanable = append(cleanable, merge.Candidate{Branch: branch, SHA: sha})
			}
		}
		return merge.Result{Cleanable: cleanable}, nil
	})
	changeToDir(t, repo)

	require.NoError(t, runCleanWithClassifier(&cobra.Command{}, classifier, false, true))
	assert.Equal(t, int32(4), lookupCalls.Load())
	assert.NoDirExists(t, featureWorktree)
	assert.NoDirExists(t, secondWorktree)
}

func TestCleanRetainsRemoteGoneWhenForgeReportsUnmerged(t *testing.T) {
	repo, worktree := createSquashMergedCleanWorktree(t)
	classifier := stubForgeVerifier(t, []string{"not-the-local-tip"}, nil)
	changeToDir(t, repo)

	stderr := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(stderr)
	require.NoError(t, runCleanWithClassifier(cmd, classifier, false, true))

	_, err := os.Stat(worktree)
	require.NoError(t, err, "remote-gone branch without confirmed merge should be retained")
	runGitInDir(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/feature")
	assert.NotContains(t, ui.StripANSI(stderr.String()), "merge verification")
}

func TestCleanRetainsSquashMergedBranchWithPostMergeCommits(t *testing.T) {
	// Regression: a merged PR for the branch name exists, but the user added
	// local commits after the merge. The tip no longer equals the merged head
	// SHA, so cleanup must retain the branch instead of force-deleting it.
	repo, worktree := createSquashMergedCleanWorktree(t)
	mergedHeadSHA := gitRevParse(t, repo, "refs/heads/feature")

	require.NoError(t, os.WriteFile(filepath.Join(worktree, "post-merge"), []byte("later work\n"), 0o644))
	runGitInDir(t, worktree, "add", "post-merge")
	runGitInDir(t, worktree, "commit", "-m", "post-merge work")

	classifier := stubForgeVerifier(t, []string{mergedHeadSHA}, nil)
	changeToDir(t, repo)

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	require.NoError(t, runCleanWithClassifier(cmd, classifier, false, true))

	_, err := os.Stat(worktree)
	require.NoError(t, err, "branch with post-merge commits must be retained")
	runGitInDir(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/feature")
}

func TestCleanRetainsReusedBranchName(t *testing.T) {
	// Regression: after merge the branch name is reused for unrelated new
	// work based on origin/main. The historical merged PR head does not match
	// the new tip, so cleanup must retain it.
	repo, worktree := createSquashMergedCleanWorktree(t)
	mergedHeadSHA := gitRevParse(t, repo, "refs/heads/feature")

	// Repoint feature at origin/main and sync its worktree so status stays
	// clean, then add unrelated new work under the reused name.
	runGitInDir(t, repo, "update-ref", "refs/heads/feature", gitRevParse(t, repo, "origin/main"))
	runGitInDir(t, worktree, "reset", "--hard", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "unrelated"), []byte("new work\n"), 0o644))
	runGitInDir(t, worktree, "add", "unrelated")
	runGitInDir(t, worktree, "commit", "-m", "unrelated new work")

	classifier := stubForgeVerifier(t, []string{mergedHeadSHA}, nil)
	changeToDir(t, repo)

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	require.NoError(t, runCleanWithClassifier(cmd, classifier, false, true))

	_, err := os.Stat(worktree)
	require.NoError(t, err, "reused branch name must be retained")
	runGitInDir(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/feature")
}

func TestCleanSurfacesVerificationWarningOnce(t *testing.T) {
	repo, _ := createSquashMergedCleanWorktree(t)
	classifier := stubForgeVerifier(t, nil, assert.AnError)
	changeToDir(t, repo)

	stderr := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(stderr)
	require.NoError(t, runCleanWithClassifier(cmd, classifier, true, true))

	cleanOutput := ui.StripANSI(stderr.String())
	assert.Contains(t, cleanOutput, "merge verification failed")
	assert.Equal(t, 1, strings.Count(cleanOutput, "merge verification failed"))
}

func TestCleanSurfacesForgeParserWarning(t *testing.T) {
	repo, _ := createSquashMergedCleanWorktree(t)
	binDir := t.TempDir()
	ghPath := filepath.Join(binDir, "gh")
	require.NoError(t, os.WriteFile(ghPath, []byte("#!/bin/sh\nprintf ' \\n'\n"), 0o755))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("_TREEMAN_REMOTE_URL", "https://github.com/owner/repo.git")
	changeToDir(t, repo)

	stderr := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(stderr)
	require.NoError(t, runClean(cmd, true, true))

	cleanOutput := ui.StripANSI(stderr.String())
	assert.Contains(t, cleanOutput, "merge verification for \"feature\" failed: gh: parsing associated PR list")
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

	// Squash-merge via a second clone: new commit on main whose SHA is not an
	// ancestor of the feature tip.
	squasher := filepath.Join(t.TempDir(), "squasher")
	runGit(t, "clone", origin, squasher)
	runGitInDir(t, squasher, "config", "user.email", "test@example.com")
	runGitInDir(t, squasher, "config", "user.name", "Test User")
	runGitInDir(t, squasher, "merge", "--squash", "origin/feature")
	runGitInDir(t, squasher, "commit", "-m", "squash merge feature")
	runGitInDir(t, squasher, "push", "origin", "main")
	// Delete the remote branch (forges do this automatically after merge).
	runGitInDir(t, squasher, "push", "origin", "--delete", "feature")

	return repo, worktree
}

func createRemoteDeletedUnmergedCleanWorktree(t *testing.T) (string, string) {
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
	runGitInDir(t, repo, "push", "origin", "--delete", "feature")

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
	runGitInDir(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
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

func gitRevParse(t *testing.T, dir, ref string) string {
	t.Helper()
	command := exec.Command("git", "-C", dir, "rev-parse", ref)
	output, err := command.CombinedOutput()
	require.NoErrorf(t, err, "git rev-parse %s failed: %s", ref, output)
	return strings.TrimSpace(string(output))
}

func traceGitCommands(t *testing.T, failLSRemote bool) func() []string {
	t.Helper()
	realGit, err := exec.LookPath("git")
	require.NoError(t, err)
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "git.log")
	gitPath := filepath.Join(binDir, "git")
	script := "#!/bin/sh\nif [ \"$TREEMAN_GIT_WRAPPED\" = \"1\" ]; then\n  exec \"$TREEMAN_REAL_GIT\" \"$@\"\nfi\nprintf '%s\\n' \"$*\" >> \"$TREEMAN_GIT_LOG\"\nif [ \"$TREEMAN_FAIL_LS_REMOTE\" = \"1\" ] && [ \"$1\" = \"ls-remote\" ]; then\n  exit 1\nfi\nexec env TREEMAN_GIT_WRAPPED=1 \"$TREEMAN_REAL_GIT\" \"$@\"\n"
	require.NoError(t, os.WriteFile(gitPath, []byte(script), 0o755))
	t.Setenv("TREEMAN_REAL_GIT", realGit)
	t.Setenv("TREEMAN_GIT_LOG", logPath)
	if failLSRemote {
		t.Setenv("TREEMAN_FAIL_LS_REMOTE", "1")
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() []string {
		contents, err := os.ReadFile(logPath)
		require.NoError(t, err)
		return strings.FieldsFunc(strings.TrimSpace(string(contents)), func(r rune) bool { return r == '\n' })
	}
}

func countGitCommands(commands []string, prefix string) int {
	count := 0
	for _, command := range commands {
		if strings.HasPrefix(command, prefix) {
			count++
		}
	}
	return count
}

func traceGitHubAPI(t *testing.T, snapshotResponse, restResponse string) func() []string {
	t.Helper()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "gh.log")
	ghPath := filepath.Join(binDir, "gh")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$TREEMAN_GH_LOG\"\nif [ \"$1\" = \"api\" ] && [ \"$2\" = \"graphql\" ]; then\n  printf '%s\\n' \"$TREEMAN_GH_SNAPSHOT\"\n  exit 0\nfi\nprintf '%s\\n' \"$TREEMAN_GH_REST\"\n"
	require.NoError(t, os.WriteFile(ghPath, []byte(script), 0o755))
	t.Setenv("TREEMAN_GH_LOG", logPath)
	t.Setenv("TREEMAN_GH_SNAPSHOT", snapshotResponse)
	t.Setenv("TREEMAN_GH_REST", restResponse)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() []string {
		contents, err := os.ReadFile(logPath)
		require.NoError(t, err)
		return strings.FieldsFunc(strings.TrimSpace(string(contents)), func(r rune) bool { return r == '\n' })
	}
}

func countGitHubCommands(commands []string, prefix string) int {
	count := 0
	for _, command := range commands {
		if strings.HasPrefix(command, prefix) {
			count++
		}
	}
	return count
}

func githubSnapshotResponse(defaultSHA, branchSHA, commit string, branchPresent bool) string {
	branchRef := "null"
	if branchPresent {
		branchRef = fmt.Sprintf(`{"target":{"oid":%q}}`, branchSHA)
	}
	return fmt.Sprintf(`{"data":{"repository":{"ref0":{"target":{"oid":%q}},"ref1":%s,"commit0":%s}}}`, defaultSHA, branchRef, commit)
}

func originBranchSHA(t *testing.T, repo, branch string) string {
	t.Helper()
	command := exec.Command("git", "-C", repo, "remote", "get-url", "origin")
	output, err := command.Output()
	require.NoError(t, err)
	return gitRevParse(t, strings.TrimSpace(string(output)), "refs/heads/"+branch)
}
