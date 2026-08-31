package forge

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitLabMergedHeadsBatchesAndPaginatesExactMatches(t *testing.T) {
	previous := gitlabGraphQLCall
	calls := 0
	gitlabGraphQLCall = func(_ context.Context, host, query string, variables map[string]any) ([]byte, error) {
		assert.Equal(t, "gitlab.example", host)
		assert.Contains(t, query, "sourceBranches: $sourceBranches")
		assert.Contains(t, query, "diffHeadSha")
		assert.Equal(t, "group/project", variables["fullPath"])
		assert.Equal(t, []string{"feature", "reused"}, variables["sourceBranches"])
		assert.Equal(t, []string{"main"}, variables["targetBranches"])
		calls++
		if calls == 1 {
			assert.Nil(t, variables["endCursor"])
			return []byte(`{"data":{"project":{"mergeRequests":{"nodes":[{"sourceBranch":"feature","targetBranch":"main","diffHeadSha":"aaa111"},{"sourceBranch":"reused","targetBranch":"main","diffHeadSha":"old111"},{"sourceBranch":"reused","targetBranch":"other","diffHeadSha":"bbb222"}],"pageInfo":{"endCursor":"next","hasNextPage":true}}}}}`), nil
		}
		assert.Equal(t, "next", variables["endCursor"])
		return []byte(`{"data":{"project":{"mergeRequests":{"nodes":[{"sourceBranch":"reused","targetBranch":"main","diffHeadSha":"bbb222"}],"pageInfo":{"endCursor":null,"hasNextPage":false}}}}}`), nil
	}
	t.Cleanup(func() { gitlabGraphQLCall = previous })
	matched, err := GitLabMergedHeads("group/project", "gitlab.example", "main", []SnapshotCandidate{{Branch: "feature", SHA: "aaa111"}, {Branch: "reused", SHA: "bbb222"}})
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"feature": true, "reused": true}, matched)
	assert.Equal(t, 2, calls)
}

func TestGitLabMergedHeadsBatchesCandidateInputAndMatchesAcrossBatches(t *testing.T) {
	previous := gitlabGraphQLCall
	calls := 0
	gitlabGraphQLCall = func(_ context.Context, host, query string, variables map[string]any) ([]byte, error) {
		assert.Equal(t, "gitlab.example", host)
		assert.Contains(t, query, "sourceBranches: $sourceBranches")
		branches := variables["sourceBranches"].([]string)
		calls++
		if calls == 1 {
			assert.Len(t, branches, gitlabMergedHeadsBatchSize)
			assert.Equal(t, "feature/0", branches[0])
			assert.Equal(t, fmt.Sprintf("feature/%d", gitlabMergedHeadsBatchSize-1), branches[len(branches)-1])
			return []byte(`{"data":{"project":{"mergeRequests":{"nodes":[{"sourceBranch":"feature/0","targetBranch":"main","diffHeadSha":"sha0"}],"pageInfo":{"endCursor":null,"hasNextPage":false}}}}}`), nil
		}
		assert.Equal(t, []string{fmt.Sprintf("feature/%d", gitlabMergedHeadsBatchSize)}, branches)
		return []byte(fmt.Sprintf(`{"data":{"project":{"mergeRequests":{"nodes":[{"sourceBranch":"feature/%d","targetBranch":"main","diffHeadSha":"sha%d"}],"pageInfo":{"endCursor":null,"hasNextPage":false}}}}}`, gitlabMergedHeadsBatchSize, gitlabMergedHeadsBatchSize)), nil
	}
	t.Cleanup(func() { gitlabGraphQLCall = previous })
	candidates := gitlabTestCandidates(gitlabMergedHeadsBatchSize + 1)
	matched, err := GitLabMergedHeads("group/project", "gitlab.example", "main", candidates)
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
	assert.Equal(t, map[string]bool{"feature/0": true, fmt.Sprintf("feature/%d", gitlabMergedHeadsBatchSize): true}, matched)
}

func TestGitLabMergedHeadsRejectsOutOfBatchMatches(t *testing.T) {
	previous := gitlabGraphQLCall
	calls := 0
	gitlabGraphQLCall = func(_ context.Context, host, query string, variables map[string]any) ([]byte, error) {
		assert.Equal(t, "gitlab.example", host)
		assert.Contains(t, query, "sourceBranches: $sourceBranches")
		calls++
		if calls == 1 {
			assert.Len(t, variables["sourceBranches"], gitlabMergedHeadsBatchSize)
			return []byte(fmt.Sprintf(`{"data":{"project":{"mergeRequests":{"nodes":[{"sourceBranch":"feature/0","targetBranch":"main","diffHeadSha":"sha0"},{"sourceBranch":"feature/%d","targetBranch":"main","diffHeadSha":"sha%d"}],"pageInfo":{"endCursor":null,"hasNextPage":false}}}}}`, gitlabMergedHeadsBatchSize, gitlabMergedHeadsBatchSize)), nil
		}
		return []byte(`{"data":{"project":{"mergeRequests":{"nodes":[],"pageInfo":{"endCursor":null,"hasNextPage":false}}}}}`), nil
	}
	t.Cleanup(func() { gitlabGraphQLCall = previous })
	matched, err := GitLabMergedHeads("group/project", "gitlab.example", "main", gitlabTestCandidates(gitlabMergedHeadsBatchSize+1))
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"feature/0": true}, matched)
	assert.Equal(t, 2, calls)
}

func TestGitLabMergedHeadsDiscardsPartialMatchesWhenABatchFails(t *testing.T) {
	previous := gitlabGraphQLCall
	calls := 0
	gitlabGraphQLCall = func(context.Context, string, string, map[string]any) ([]byte, error) {
		calls++
		if calls == 1 {
			return []byte(`{"data":{"project":{"mergeRequests":{"nodes":[{"sourceBranch":"feature/0","targetBranch":"main","diffHeadSha":"sha0"}],"pageInfo":{"endCursor":null,"hasNextPage":false}}}}}`), nil
		}
		return nil, assert.AnError
	}
	t.Cleanup(func() { gitlabGraphQLCall = previous })
	matched, err := GitLabMergedHeads("group/project", "gitlab.example", "main", gitlabTestCandidates(gitlabMergedHeadsBatchSize+1))
	require.ErrorIs(t, err, assert.AnError)
	assert.Nil(t, matched)
	assert.Equal(t, 2, calls)
}

func TestGitLabMergedHeadsRejectsIncompletePage(t *testing.T) {
	previous := gitlabGraphQLCall
	gitlabGraphQLCall = func(context.Context, string, string, map[string]any) ([]byte, error) {
		return []byte(`{"data":{"project":{"mergeRequests":{"nodes":[],"pageInfo":{"endCursor":null,"hasNextPage":true}}}}}`), nil
	}
	t.Cleanup(func() { gitlabGraphQLCall = previous })
	_, err := GitLabMergedHeads("group/project", "gitlab.example", "main", []SnapshotCandidate{{Branch: "feature", SHA: "aaa111"}})
	assert.Error(t, err)
}

func TestGitLabMergedHeadsRejectsUnchangedCursor(t *testing.T) {
	previous := gitlabGraphQLCall
	calls := 0
	gitlabGraphQLCall = func(context.Context, string, string, map[string]any) ([]byte, error) {
		calls++
		return []byte(`{"data":{"project":{"mergeRequests":{"nodes":[],"pageInfo":{"endCursor":"stuck","hasNextPage":true}}}}}`), nil
	}
	t.Cleanup(func() { gitlabGraphQLCall = previous })
	_, err := GitLabMergedHeads("group/project", "gitlab.example", "main", []SnapshotCandidate{{Branch: "feature", SHA: "aaa111"}})
	require.EqualError(t, err, `glab: GitLab merged-MR query cursor did not advance from "stuck"`)
	assert.Equal(t, 2, calls)
}

func gitlabTestCandidates(count int) []SnapshotCandidate {
	candidates := make([]SnapshotCandidate, count)
	for index := range candidates {
		candidates[index] = SnapshotCandidate{Branch: fmt.Sprintf("feature/%d", index), SHA: fmt.Sprintf("sha%d", index)}
	}
	return candidates
}
