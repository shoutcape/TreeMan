package cmd

import (
	"fmt"
	"testing"

	"github.com/shoutcape/treeman/internal/forge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubForgeVerifier overrides forge verification for the feature branch.
func stubForgeVerifier(t *testing.T, headSHAs []string, verifyErr error) {
	t.Helper()
	previous := forgeMergedLookup
	forgeMergedLookup = func(string) (forgeMergeVerifier, error) {
		if verifyErr != nil {
			return nil, fmt.Errorf("merge verification failed: %w", verifyErr)
		}
		return func(branch, sha string) (bool, error) {
			if branch != "feature" {
				return false, nil
			}
			for _, headSHA := range headSHAs {
				if sha == headSHA {
					return true, nil
				}
			}
			return false, nil
		}, nil
	}
	t.Cleanup(func() { forgeMergedLookup = previous })
}

func TestJoinWarning(t *testing.T) {
	assert.Equal(t, "first", joinWarning("", "first"))
	assert.Equal(t, "first; second", joinWarning("first", "second"))
}

func TestClassifyCleanableUsesGitLabBatchForRemoteGoneBranches(t *testing.T) {
	repo, _ := createSquashMergedCleanWorktree(t)
	featureSHA := gitRevParse(t, repo, "refs/heads/feature")
	previousBatchLookup := gitlabMergedHeadsLookup
	gitlabMergedHeadsLookup = func(defaultBranch string, candidates []forge.SnapshotCandidate) (map[string]bool, bool) {
		assert.Equal(t, "main", defaultBranch)
		assert.Equal(t, []forge.SnapshotCandidate{{Branch: "feature", SHA: featureSHA}}, candidates)
		return map[string]bool{"feature": true}, true
	}
	t.Cleanup(func() { gitlabMergedHeadsLookup = previousBatchLookup })
	previousVerifier := forgeMergedLookup
	forgeMergedLookup = func(string) (forgeMergeVerifier, error) {
		t.Fatal("per-branch verifier must not run after a GitLab batch result")
		return nil, nil
	}
	t.Cleanup(func() { forgeMergedLookup = previousVerifier })
	changeToDir(t, repo)

	verified, warning, err := classifyCleanable("origin/main", "main", []string{"feature"}, mergeState{
		branches: map[string]branchMergeState{
			"feature": {sha: featureSHA, remoteExists: false},
		},
	})
	require.NoError(t, err)
	assert.Empty(t, warning)
	assert.Equal(t, map[string]string{"feature": featureSHA}, verified)
}
