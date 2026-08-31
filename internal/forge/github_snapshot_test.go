package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitHubCompleteSnapshot(t *testing.T) {
	previous := githubGraphQLCall
	githubGraphQLCall = func(_ context.Context, query string, variables map[string]string) ([]byte, error) {
		assert.Contains(t, query, "ref0: ref(qualifiedName: $ref0)")
		assert.Contains(t, query, "ref1: ref(qualifiedName: $ref1)")
		assert.Contains(t, query, "pullRequests")
		assert.Equal(t, map[string]string{"owner": "owner", "name": "repo", "base": "main", "ref0": "refs/heads/main", "ref1": "refs/heads/feature/with spaces", "branch0": "feature/with spaces"}, variables)
		return []byte(`{"data":{"repository":{"ref0":{"target":{"oid":"main111"}},"ref1":null,"prs0":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`), nil
	}
	t.Cleanup(func() { githubGraphQLCall = previous })
	snapshot, err := GitHubCompleteSnapshot("owner/repo", "main", []SnapshotCandidate{{Branch: "feature/with spaces", SHA: "aaa111"}})
	require.NoError(t, err)
	assert.Equal(t, "main111", snapshot.DefaultSHA)
	assert.Equal(t, []SnapshotBranch{{Candidate: SnapshotCandidate{Branch: "feature/with spaces", SHA: "aaa111"}}}, snapshot.Branches)
}

func TestParseGitHubEvidenceExactMatching(t *testing.T) {
	candidates := []SnapshotCandidate{{Branch: "feature", SHA: "merged111"}, {Branch: "reused", SHA: "new222"}}
	plan := newGitHubCompleteSnapshotPlan(candidates)
	payload := []byte(`{"data":{"repository":{"ref0":{"target":{"oid":"main111"}},"ref1":null,"ref2":null,"prs0":{"nodes":[{"headRefOid":"merged111"}],"pageInfo":{"hasNextPage":false}},"prs1":{"nodes":[{"headRefOid":"old111"}],"pageInfo":{"hasNextPage":false}}}}}`)
	snapshot, err := parseGitHubCompleteSnapshot(payload, "main", plan)
	require.NoError(t, err)
	assert.Equal(t, []SnapshotBranch{{Candidate: candidates[0], Verification: SnapshotMerged, MergedHeads: []string{"merged111"}}, {Candidate: candidates[1], Verification: SnapshotNotMerged, MergedHeads: []string{"old111"}}}, snapshot.Branches)
}

// TestParseGitHubEvidenceNotMergedIntoDefault verifies that a PR merged into
// a non-default base is not treated as merged.
func TestParseGitHubEvidenceNotMergedIntoDefault(t *testing.T) {
	candidates := []SnapshotCandidate{{Branch: "feature", SHA: "aaa111"}}
	plan := newGitHubCompleteSnapshotPlan(candidates)
	// The filtered query returns no PR when all merged PRs target "other".
	payload := []byte(`{"data":{"repository":{"ref0":{"target":{"oid":"main111"}},"ref1":null,"prs0":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`)
	snapshot, err := parseGitHubCompleteSnapshot(payload, "main", plan)
	require.NoError(t, err)
	assert.Equal(t, []SnapshotBranch{{Candidate: candidates[0], Verification: SnapshotNotMerged}}, snapshot.Branches)
}

func TestParseGitHubEvidenceMarksIncompleteCandidatesForFallback(t *testing.T) {
	candidates := []SnapshotCandidate{{Branch: "missing", SHA: "aaa111"}, {Branch: "paginated", SHA: "bbb222"}, {Branch: "null-node", SHA: "ccc333"}, {Branch: "missing-field", SHA: "ddd444"}}
	plan := newGitHubCompleteSnapshotPlan(candidates)
	payload := []byte(`{"data":{"repository":{"ref0":{"target":{"oid":"main111"}},"ref1":null,"ref2":null,"ref3":null,"ref4":null,"prs0":null,"prs1":{"nodes":[],"pageInfo":{"hasNextPage":true}},"prs2":{"nodes":[null],"pageInfo":{"hasNextPage":false}},"prs3":{"nodes":[{"headRefOid":null}],"pageInfo":{"hasNextPage":false}}}}}`)
	snapshot, err := parseGitHubCompleteSnapshot(payload, "main", plan)
	require.NoError(t, err)
	assert.Equal(t, []SnapshotBranch{{Candidate: candidates[0], Verification: SnapshotNeedsFallback}, {Candidate: candidates[1], Verification: SnapshotNeedsFallback}, {Candidate: candidates[2], Verification: SnapshotNeedsFallback}, {Candidate: candidates[3], Verification: SnapshotNeedsFallback}}, snapshot.Branches)
}

func TestParseGitHubEvidenceRejectsInvalidRemoteState(t *testing.T) {
	plan := newGitHubCompleteSnapshotPlan([]SnapshotCandidate{{Branch: "feature", SHA: "aaa111"}})
	for _, payload := range [][]byte{[]byte(`{"errors":[{"message":"forbidden"}]}`), []byte(`{"data":{"repository":{"ref0":null,"ref1":null,"commit0":null}}}`), []byte(`{"data":{"repository":{"ref0":{"target":{"oid":"main111"}},"ref1":null}}}`), []byte(`not-json`)} {
		_, err := parseGitHubCompleteSnapshot(payload, "main", plan)
		assert.Error(t, err)
	}
}

func TestGitHubSnapshotQueriesUseOnlyGeneratedAliases(t *testing.T) {
	query := newGitHubCompleteSnapshotPlan([]SnapshotCandidate{{Branch: "one", SHA: "one"}, {Branch: "two", SHA: "two"}}).query()
	for _, value := range []string{"ref0", "ref1", "ref2", "prs0", "prs1", "pullRequests"} {
		assert.Contains(t, query, value)
	}
	assert.False(t, strings.Contains(query, "feature/unsafe"))
}

func TestGitHubCompleteSnapshotBatchesOversizedCandidateLists(t *testing.T) {
	previous := githubGraphQLCall
	calls := 0
	githubGraphQLCall = func(context.Context, string, map[string]string) ([]byte, error) {
		calls++
		return githubSnapshotPayload(t, githubSnapshotBatchSize), nil
	}
	t.Cleanup(func() { githubGraphQLCall = previous })
	candidates := snapshotCandidates(githubSnapshotBatchSize + 1)
	snapshot, err := GitHubCompleteSnapshot("owner/repo", "main", candidates)
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
	assert.Equal(t, "main111", snapshot.DefaultSHA)
	assert.Len(t, snapshot.Branches, len(candidates))
	assert.Equal(t, candidates[githubSnapshotBatchSize], snapshot.Branches[githubSnapshotBatchSize].Candidate)
}

func TestGitHubCompleteSnapshotRejectsChangedDefaultBranchAcrossBatches(t *testing.T) {
	previous := githubGraphQLCall
	calls := 0
	githubGraphQLCall = func(context.Context, string, map[string]string) ([]byte, error) {
		calls++
		if calls == 1 {
			return githubSnapshotPayload(t, githubSnapshotBatchSize), nil
		}
		return []byte(`{"data":{"repository":{"ref0":{"target":{"oid":"changed"}},"ref1":null,"prs0":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`), nil
	}
	t.Cleanup(func() { githubGraphQLCall = previous })
	_, err := GitHubCompleteSnapshot("owner/repo", "main", snapshotCandidates(githubSnapshotBatchSize+1))
	require.ErrorIs(t, err, ErrGitHubDefaultBranchChanged)
	assert.Equal(t, 2, calls)
}

func TestGitHubCompleteSnapshotDiscardsPartialBatchesOnFailure(t *testing.T) {
	previous := githubGraphQLCall
	calls := 0
	githubGraphQLCall = func(context.Context, string, map[string]string) ([]byte, error) {
		calls++
		if calls == 1 {
			return githubSnapshotPayload(t, githubSnapshotBatchSize), nil
		}
		return nil, assert.AnError
	}
	t.Cleanup(func() { githubGraphQLCall = previous })
	snapshot, err := GitHubCompleteSnapshot("owner/repo", "main", snapshotCandidates(githubSnapshotBatchSize+1))
	require.ErrorIs(t, err, assert.AnError)
	assert.Equal(t, GitHubSnapshot{}, snapshot)
	assert.Equal(t, 2, calls)
}

func TestGitHubCompleteSnapshotRejectsInvalidCandidates(t *testing.T) {
	for _, candidates := range [][]SnapshotCandidate{{{Branch: "", SHA: "aaa111"}}, {{Branch: "feature", SHA: ""}}, {{Branch: "feature", SHA: "aaa111"}, {Branch: "feature", SHA: "bbb222"}}} {
		_, err := GitHubCompleteSnapshot("owner/repo", "main", candidates)
		assert.Error(t, err)
	}
}

func githubSnapshotPayload(t *testing.T, candidateCount int) []byte {
	t.Helper()
	repository := map[string]any{"ref0": map[string]any{"target": map[string]string{"oid": "main111"}}}
	for index := range candidateCount {
		repository[fmt.Sprintf("ref%d", index+1)] = nil
		repository[fmt.Sprintf("prs%d", index)] = map[string]any{"nodes": []any{}, "pageInfo": map[string]bool{"hasNextPage": false}}
	}
	payload, err := json.Marshal(map[string]any{"data": map[string]any{"repository": repository}})
	require.NoError(t, err)
	return payload
}

func snapshotCandidates(count int) []SnapshotCandidate {
	candidates := make([]SnapshotCandidate, count)
	for index := range candidates {
		candidates[index] = SnapshotCandidate{Branch: fmt.Sprintf("feature/%d", index), SHA: fmt.Sprintf("sha%d", index)}
	}
	return candidates
}
