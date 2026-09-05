package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/merge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanRevalidationReportsDirtyPreviewCandidate(t *testing.T) {
	repo, worktree := createMergedCleanWorktree(t)
	changeToDir(t, repo)
	candidate := previewCleanCandidate(t, repo, worktree)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "dirty"), []byte("keep\n"), 0o644))

	selection, err := revalidateCleanCandidates(merge.ClassifierFunc(func(string, []string) (merge.Result, error) {
		t.Fatal("dirty preview candidate must not be classified")
		return merge.Result{}, nil
	}), "main", repo, []cleanCandidate{candidate})

	require.NoError(t, err)
	assert.Empty(t, selection.candidates)
	assert.Equal(t, []merge.Diagnostic{{Operation: `skipping "feature": dirty`}}, selection.diagnostics)
}

func TestCleanRevalidationReportsChangedPreviewTip(t *testing.T) {
	repo, worktree := createMergedCleanWorktree(t)
	changeToDir(t, repo)
	candidate := previewCleanCandidate(t, repo, worktree)
	gitTest(t, repo, "commit", "--allow-empty", "-m", "move feature")
	gitTest(t, repo, "update-ref", "refs/heads/feature", "refs/heads/main")

	selection, err := revalidateCleanCandidates(merge.ClassifierFunc(func(string, []string) (merge.Result, error) {
		t.Fatal("changed preview candidate must not be classified")
		return merge.Result{}, nil
	}), "main", repo, []cleanCandidate{candidate})

	require.NoError(t, err)
	assert.Empty(t, selection.candidates)
	assert.Equal(t, []merge.Diagnostic{{Operation: `skipping "feature": tip changed`}}, selection.diagnostics)
}

func TestCleanRevalidationReportsIneligiblePreviewCandidate(t *testing.T) {
	repo, worktree := createMergedCleanWorktree(t)
	changeToDir(t, repo)
	candidate := previewCleanCandidate(t, repo, worktree)
	runGitInDir(t, repo, "worktree", "remove", worktree)

	selection, err := revalidateCleanCandidates(merge.ClassifierFunc(func(string, []string) (merge.Result, error) {
		t.Fatal("ineligible preview candidate must not be classified")
		return merge.Result{}, nil
	}), "main", repo, []cleanCandidate{candidate})

	require.NoError(t, err)
	assert.Empty(t, selection.candidates)
	assert.Equal(t, []merge.Diagnostic{{Operation: `skipping "feature": no longer eligible`}}, selection.diagnostics)
}

func TestCleanRevalidationSilentlySkipsStalePreviewCandidate(t *testing.T) {
	repo, worktree := createMergedCleanWorktree(t)
	changeToDir(t, repo)
	candidate := previewCleanCandidate(t, repo, worktree)
	require.NoError(t, os.RemoveAll(worktree))

	selection, err := revalidateCleanCandidates(merge.ClassifierFunc(func(string, []string) (merge.Result, error) {
		t.Fatal("stale preview candidate must not be classified")
		return merge.Result{}, nil
	}), "main", repo, []cleanCandidate{candidate})

	require.NoError(t, err)
	assert.Empty(t, selection.candidates)
	assert.Empty(t, selection.diagnostics)
}

func TestCleanCandidateDiscoveryExcludesStaleWorktree(t *testing.T) {
	repo, worktree := createMergedCleanWorktree(t)
	changeToDir(t, repo)
	require.NoError(t, os.RemoveAll(worktree))

	entries, err := git.WorktreeList()
	require.NoError(t, err)
	cleanEntries, diagnostics, err := cleanLinkedWorktreeEntries(repo, entries)

	require.NoError(t, err)
	assert.Empty(t, cleanEntries)
	assert.Empty(t, diagnostics)
}

func TestCleanRevalidationKeepsOnlyStillEligiblePreviewPairs(t *testing.T) {
	repo, worktree := createMergedCleanWorktree(t)
	changeToDir(t, repo)
	candidate := previewCleanCandidate(t, repo, worktree)

	selection, err := revalidateCleanCandidates(merge.ClassifierFunc(func(defaultBranch string, branches []string) (merge.Result, error) {
		assert.Equal(t, "main", defaultBranch)
		assert.Equal(t, []string{"feature"}, branches)
		return merge.Result{}, nil
	}), "main", repo, []cleanCandidate{candidate})

	require.NoError(t, err)
	assert.Empty(t, selection.candidates)
	assert.Equal(t, []merge.Diagnostic{{Operation: `skipping "feature": no longer eligible`}}, selection.diagnostics)
}

func TestCleanRevalidationRejectsMalformedClassifierResults(t *testing.T) {
	repo, worktree := createMergedCleanWorktree(t)
	changeToDir(t, repo)
	candidate := previewCleanCandidate(t, repo, worktree)

	_, err := revalidateCleanCandidates(merge.ClassifierFunc(func(string, []string) (merge.Result, error) {
		return merge.Result{Cleanable: []merge.Candidate{{Branch: "other", SHA: "abc123"}}}, nil
	}), "main", repo, []cleanCandidate{candidate})

	require.EqualError(t, err, `classifier returned unknown cleanable branch "other"`)
}

func previewCleanCandidate(t *testing.T, repo, worktree string) cleanCandidate {
	t.Helper()
	entry, err := findWorktree(worktree)
	require.NoError(t, err)
	sha, err := git.BranchSHA(entry.Branch)
	require.NoError(t, err)
	return cleanCandidate{entry: entry, verifiedSHA: sha}
}
