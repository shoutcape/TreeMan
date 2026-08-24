package forge

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchRef(t *testing.T) {
	assert.Equal(t, "pull/42/head", FetchRef(GitHub, 42))
	assert.Equal(t, "merge-requests/7/head", FetchRef(GitLab, 7))
}

func TestCLITool(t *testing.T) {
	assert.Equal(t, "gh", CLITool(GitHub))
	assert.Equal(t, "glab", CLITool(GitLab))
	assert.Equal(t, "", CLITool(Type("unknown")))
}

func TestForgeAPIArgs(t *testing.T) {
	assert.Equal(t, []string{"api", "repos/owner/repo/branches?per_page=100", "--paginate"},
		ghAPIArgs("repos/owner/repo/branches?per_page=100"))
	assert.Equal(t, []string{"api", "projects/group%2Frepo/merge_requests?per_page=100", "--hostname", "gitlab.example", "--paginate"},
		glabAPIArgs("gitlab.example", "projects/group%2Frepo/merge_requests?per_page=100"))
	assert.Equal(t, []string{"api", "graphql", "-f", "query=query", "-f", "name=repo", "-f", "owner=owner"},
		ghGraphQLArgs("query", map[string]string{"owner": "owner", "name": "repo"}))
}

func TestGitHubRemoteSnapshot(t *testing.T) {
	previous := githubGraphQLCall
	githubGraphQLCall = func(query string, variables map[string]string) ([]byte, error) {
		assert.Contains(t, query, "ref0: ref(qualifiedName: $ref0)")
		assert.Contains(t, query, "ref1: ref(qualifiedName: $ref1)")
		assert.Contains(t, query, "commit0: object(expression: $sha0)")
		assert.NotContains(t, query, variables["ref1"])
		assert.Equal(t, map[string]string{
			"owner": "owner",
			"name":  "repo",
			"ref0":  "refs/heads/main",
			"ref1":  "refs/heads/feature/with spaces",
			"sha0":  "aaa111",
		}, variables)
		return []byte(`{"data":{"repository":{"ref0":{"target":{"oid":"main111"}},"ref1":null,"commit0":{"associatedPullRequests":{"nodes":[{"merged":true,"baseRefName":"main","headRefName":"feature/with spaces","headRefOid":"aaa111"}],"pageInfo":{"hasNextPage":false}}}}}}`), nil
	}
	t.Cleanup(func() { githubGraphQLCall = previous })

	snapshot, err := GitHubRemoteSnapshot("owner/repo", "main", []SnapshotCandidate{{Branch: "feature/with spaces", SHA: "aaa111"}})
	require.NoError(t, err)
	assert.Equal(t, "main111", snapshot.DefaultSHA)
	assert.Equal(t, []SnapshotBranch{{
		Candidate:    SnapshotCandidate{Branch: "feature/with spaces", SHA: "aaa111"},
		Verification: SnapshotMerged,
	}}, snapshot.Branches)
}

func TestParseGitHubSnapshotExactMatching(t *testing.T) {
	candidates := []SnapshotCandidate{
		{Branch: "feature", SHA: "merged111"},
		{Branch: "reused", SHA: "new222"},
	}
	plan := newGitHubSnapshotPlan(candidates)
	payload := []byte(`{"data":{"repository":{"ref0":{"target":{"oid":"main111"}},"ref1":null,"ref2":null,"commit0":{"associatedPullRequests":{"nodes":[{"merged":true,"baseRefName":"main","headRefName":"feature","headRefOid":"merged111"}],"pageInfo":{"hasNextPage":false}}},"commit1":{"associatedPullRequests":{"nodes":[{"merged":true,"baseRefName":"main","headRefName":"reused","headRefOid":"old111"},{"merged":true,"baseRefName":"other","headRefName":"reused","headRefOid":"new222"},{"merged":false,"baseRefName":"main","headRefName":"reused","headRefOid":"new222"}],"pageInfo":{"hasNextPage":false}}}}}}`)

	snapshot, err := parseGitHubSnapshot(payload, "main", plan)
	require.NoError(t, err)
	assert.Equal(t, []SnapshotBranch{
		{Candidate: candidates[0], Verification: SnapshotMerged},
		{Candidate: candidates[1], Verification: SnapshotNotMerged},
	}, snapshot.Branches)
}

func TestParseGitHubSnapshotMarksIncompleteCandidatesForFallback(t *testing.T) {
	candidates := []SnapshotCandidate{
		{Branch: "missing", SHA: "aaa111"},
		{Branch: "paginated", SHA: "bbb222"},
		{Branch: "null-node", SHA: "ccc333"},
		{Branch: "missing-field", SHA: "ddd444"},
	}
	plan := newGitHubSnapshotPlan(candidates)
	payload := []byte(`{"data":{"repository":{"ref0":{"target":{"oid":"main111"}},"ref1":null,"ref2":null,"ref3":null,"ref4":null,"commit0":null,"commit1":{"associatedPullRequests":{"nodes":[],"pageInfo":{"hasNextPage":true}}},"commit2":{"associatedPullRequests":{"nodes":[null],"pageInfo":{"hasNextPage":false}}},"commit3":{"associatedPullRequests":{"nodes":[{"merged":true,"baseRefName":null,"headRefName":"missing-field","headRefOid":"ddd444"}],"pageInfo":{"hasNextPage":false}}}}}}`)

	snapshot, err := parseGitHubSnapshot(payload, "main", plan)
	require.NoError(t, err)
	assert.Equal(t, []SnapshotBranch{
		{Candidate: candidates[0], Verification: SnapshotNeedsFallback},
		{Candidate: candidates[1], Verification: SnapshotNeedsFallback},
		{Candidate: candidates[2], Verification: SnapshotNeedsFallback},
		{Candidate: candidates[3], Verification: SnapshotNeedsFallback},
	}, snapshot.Branches)
}

func TestParseGitHubSnapshotRejectsInvalidRemoteState(t *testing.T) {
	candidates := []SnapshotCandidate{{Branch: "feature", SHA: "aaa111"}}
	plan := newGitHubSnapshotPlan(candidates)
	for _, payload := range [][]byte{
		[]byte(`{"errors":[{"message":"forbidden"}]}`),
		[]byte(`{"data":{"repository":{"ref0":null,"ref1":null,"commit0":null}}}`),
		[]byte(`{"data":{"repository":{"ref0":{"target":{"oid":"main111"}},"ref1":null}}}`),
		[]byte(`not-json`),
	} {
		_, err := parseGitHubSnapshot(payload, "main", plan)
		assert.Error(t, err)
	}
}

func TestGitHubSnapshotQueryUsesOnlyGeneratedAliases(t *testing.T) {
	query := newGitHubSnapshotPlan([]SnapshotCandidate{{Branch: "one", SHA: "one"}, {Branch: "two", SHA: "two"}}).query()
	assert.Contains(t, query, "ref0")
	assert.Contains(t, query, "ref1")
	assert.Contains(t, query, "ref2")
	assert.Contains(t, query, "commit0")
	assert.Contains(t, query, "commit1")
	assert.False(t, strings.Contains(query, "feature/unsafe"))
}

func TestGitHubRemoteSnapshotBatchesOversizedCandidateLists(t *testing.T) {
	previous := githubGraphQLCall
	calls := 0
	githubGraphQLCall = func(string, map[string]string) ([]byte, error) {
		calls++
		return githubSnapshotPayload(t, githubSnapshotBatchSize), nil
	}
	t.Cleanup(func() { githubGraphQLCall = previous })

	candidates := make([]SnapshotCandidate, githubSnapshotBatchSize+1)
	for index := range candidates {
		candidates[index] = SnapshotCandidate{Branch: fmt.Sprintf("feature/%d", index), SHA: fmt.Sprintf("sha%d", index)}
	}
	snapshot, err := GitHubRemoteSnapshot("owner/repo", "main", candidates)
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
	assert.Equal(t, "main111", snapshot.DefaultSHA)
	assert.Len(t, snapshot.Branches, len(candidates))
	assert.Equal(t, candidates[githubSnapshotBatchSize], snapshot.Branches[githubSnapshotBatchSize].Candidate)
}

func TestGitHubRemoteSnapshotRejectsInvalidCandidates(t *testing.T) {
	for _, candidates := range [][]SnapshotCandidate{
		{{Branch: "", SHA: "aaa111"}},
		{{Branch: "feature", SHA: ""}},
		{{Branch: "feature", SHA: "aaa111"}, {Branch: "feature", SHA: "bbb222"}},
	} {
		_, err := GitHubRemoteSnapshot("owner/repo", "main", candidates)
		assert.Error(t, err)
	}
}

func githubSnapshotPayload(t *testing.T, candidateCount int) []byte {
	t.Helper()
	repository := map[string]any{
		"ref0": map[string]any{"target": map[string]string{"oid": "main111"}},
	}
	for index := range candidateCount {
		repository[fmt.Sprintf("ref%d", index+1)] = nil
		repository[fmt.Sprintf("commit%d", index)] = map[string]any{
			"associatedPullRequests": map[string]any{
				"nodes":    []any{},
				"pageInfo": map[string]bool{"hasNextPage": false},
			},
		}
	}
	payload, err := json.Marshal(map[string]any{"data": map[string]any{"repository": repository}})
	require.NoError(t, err)
	return payload
}

func TestResolveFromRemote(t *testing.T) {
	t.Run("github", func(t *testing.T) {
		forgeType, repoSlug, host, err := ResolveFromRemote("git@github.com:owner/my-repo.git")
		require.NoError(t, err)
		assert.Equal(t, GitHub, forgeType)
		assert.Equal(t, "owner/my-repo", repoSlug)
		assert.Equal(t, "github.com", host)
	})
	t.Run("gitlab", func(t *testing.T) {
		forgeType, repoSlug, host, err := ResolveFromRemote("https://gitlab.com/mygroup/myproject.git")
		require.NoError(t, err)
		assert.Equal(t, GitLab, forgeType)
		assert.Equal(t, "mygroup/myproject", repoSlug)
		assert.Equal(t, "gitlab.com", host)
	})
	t.Run("unsupported", func(t *testing.T) {
		_, _, _, err := ResolveFromRemote("https://bitbucket.org/owner/repo.git")
		assert.Error(t, err)
	})
}

func TestParseGithubMergedHead(t *testing.T) {
	payload := []byte(`[{"merged_at":"2026-08-01T10:00:00Z","base":{"ref":"main"},"head":{"ref":"feature","sha":"aaa111","repo":{"owner":{"login":"contributor"}}}}]`)
	merged, err := parseGithubMergedHead(payload, "main", "feature", "aaa111")
	require.NoError(t, err)
	assert.True(t, merged, "fork PRs must be accepted when branch and SHA match")

	merged, err = parseGithubMergedHead(payload, "main", "feature", "other")
	require.NoError(t, err)
	assert.False(t, merged)

	_, err = parseGithubMergedHead([]byte(`not-json`), "main", "feature", "aaa111")
	assert.Error(t, err)

	merged, err = parseGithubMergedHead([]byte(`[{"merged_at":null,"base":{"ref":"main"},"head":{"ref":"feature","sha":"aaa111"}}]`), "main", "feature", "aaa111")
	require.NoError(t, err)
	assert.False(t, merged)

	_, err = parseGithubMergedHead([]byte(`[{"merged_at":"","base":{"ref":"main"},"head":{"ref":"feature","sha":"aaa111"}}]`), "main", "feature", "aaa111")
	assert.Error(t, err)

	_, err = parseGithubMergedHead([]byte(`[{"merged_at":"not-a-date","base":{"ref":"main"},"head":{"ref":"feature","sha":"aaa111"}}]`), "main", "feature", "aaa111")
	assert.Error(t, err)

	merged, err = parseGithubMergedHead([]byte(`[{"merged_at":"2026-08-01T10:00:00Z","base":{"ref":"main"},"head":{"ref":"other","sha":"aaa111"}}][{"merged_at":"2026-08-01T10:00:00Z","base":{"ref":"main"},"head":{"ref":"feature","sha":"aaa111"}}]`), "main", "feature", "aaa111")
	require.NoError(t, err)
	assert.True(t, merged)

	for _, payload := range [][]byte{[]byte(``), []byte(`null`), []byte(`{}`)} {
		_, err = parseGithubMergedHead(payload, "main", "feature", "aaa111")
		assert.Error(t, err)
	}
}

func TestParseGitlabMergedHead(t *testing.T) {
	payload := []byte(`[{"state":"merged","source_branch":"feature","target_branch":"main","sha":"aaa111"}]`)
	merged, err := parseGitlabMergedHead(payload, "main", "feature", "aaa111")
	require.NoError(t, err)
	assert.True(t, merged)

	merged, err = parseGitlabMergedHead([]byte(`[]`), "main", "feature", "aaa111")
	require.NoError(t, err)
	assert.False(t, merged)

	_, err = parseGitlabMergedHead([]byte(`oops`), "main", "feature", "aaa111")
	assert.Error(t, err)

	merged, err = parseGitlabMergedHead([]byte(`[{"state":"opened","source_branch":"feature","target_branch":"main","sha":"aaa111"}]`), "main", "feature", "aaa111")
	require.NoError(t, err)
	assert.False(t, merged)

	merged, err = parseGitlabMergedHead([]byte(`[{"source_branch":"feature","target_branch":"main","sha":"aaa111"}]`), "main", "feature", "aaa111")
	require.NoError(t, err)
	assert.False(t, merged)

	merged, err = parseGitlabMergedHead([]byte(`[{"state":"merged","source_branch":"other","target_branch":"main","sha":"aaa111"}][{"state":"merged","source_branch":"feature","target_branch":"main","sha":"aaa111"}]`), "main", "feature", "aaa111")
	require.NoError(t, err)
	assert.True(t, merged)

	for _, payload := range [][]byte{[]byte(``), []byte(`null`), []byte(`{}`)} {
		_, err = parseGitlabMergedHead(payload, "main", "feature", "aaa111")
		assert.Error(t, err)
	}
}

func TestDecodePaginated(t *testing.T) {
	type value struct {
		Name string `json:"name"`
	}

	values, err := decodePaginated[value]([]byte(`[{"name":"first"}][{"name":"second"}]`))
	require.NoError(t, err)
	assert.Equal(t, []value{{Name: "first"}, {Name: "second"}}, values)

	_, err = decodePaginated[value]([]byte(`[{"name":"first"}]invalid`))
	assert.Error(t, err)
}

func TestPaginatedLists(t *testing.T) {
	t.Run("github PRs", func(t *testing.T) {
		previous := githubAPICall
		githubAPICall = func(string) ([]byte, error) {
			return []byte(`[{"number":1,"title":"first","head":{"ref":"one"}}][{"number":2,"title":"second","head":{"ref":"two"}}]`), nil
		}
		t.Cleanup(func() { githubAPICall = previous })

		prs, err := githubPRList("owner/repo")
		require.NoError(t, err)
		assert.Equal(t, []PRInfo{{Number: 1, Title: "first", Branch: "one"}, {Number: 2, Title: "second", Branch: "two"}}, prs)
	})

	t.Run("github branches", func(t *testing.T) {
		previous := githubAPICall
		githubAPICall = func(string) ([]byte, error) {
			return []byte(`[{"name":"one","commit":{"commit":{"committer":{"date":"2026-08-01T10:00:00Z"}}}}][{"name":"two","commit":{"commit":{"committer":{"date":"2026-08-02T10:00:00Z"}}}}]`), nil
		}
		t.Cleanup(func() { githubAPICall = previous })

		branches, err := githubBranchList("owner/repo")
		require.NoError(t, err)
		assert.Len(t, branches, 2)
		assert.Equal(t, []string{"one", "two"}, []string{branches[0].Name, branches[1].Name})
	})

	t.Run("gitlab MRs", func(t *testing.T) {
		previous := glabAPICall
		glabAPICall = func(string, string) ([]byte, error) {
			return []byte(`[{"iid":1,"title":"first","source_branch":"one"}][{"iid":2,"title":"second","source_branch":"two"}]`), nil
		}
		t.Cleanup(func() { glabAPICall = previous })

		mrs, err := gitlabMRList("group/repo", "gitlab.example")
		require.NoError(t, err)
		assert.Equal(t, []PRInfo{{Number: 1, Title: "first", Branch: "one"}, {Number: 2, Title: "second", Branch: "two"}}, mrs)
	})

	t.Run("gitlab branches", func(t *testing.T) {
		previous := glabAPICall
		glabAPICall = func(string, string) ([]byte, error) {
			return []byte(`[{"name":"one","commit":{"committed_date":"2026-08-01T10:00:00Z"}}][{"name":"two","commit":{"committed_date":"2026-08-02T10:00:00Z"}}]`), nil
		}
		t.Cleanup(func() { glabAPICall = previous })

		branches, err := gitlabBranchList("group/repo", "gitlab.example")
		require.NoError(t, err)
		assert.Len(t, branches, 2)
		assert.Equal(t, []string{"one", "two"}, []string{branches[0].Name, branches[1].Name})
	})
}

func TestMergedPRHeadGitHub(t *testing.T) {
	previousAPI := githubAPICall
	githubAPICall = func(endpoint string) ([]byte, error) {
		assert.Equal(t, "repos/owner/repo/commits/aaa111/pulls?per_page=100", endpoint)
		return []byte(`[{"merged_at":"2026-08-01T10:00:00Z","base":{"ref":"main"},"head":{"ref":"feature/x","sha":"aaa111"}}]`), nil
	}
	t.Cleanup(func() { githubAPICall = previousAPI })

	merged, err := MergedPRHead(GitHub, "owner/repo", "github.com", "main", "feature/x", "aaa111")
	require.NoError(t, err)
	assert.True(t, merged)
}

func TestMergedPRHeadGitLab(t *testing.T) {
	previousAPI := glabAPICall
	glabAPICall = func(host, endpoint string) ([]byte, error) {
		assert.Equal(t, "gitlab.example", host)
		assert.Equal(t, "projects/group%2Fproj/merge_requests?state=merged&source_branch=feature%2Fx&target_branch=main&per_page=100", endpoint)
		return []byte(`[{"state":"merged","source_branch":"feature/x","target_branch":"main","sha":"aaa111"}]`), nil
	}
	t.Cleanup(func() { glabAPICall = previousAPI })

	merged, err := MergedPRHead(GitLab, "group/proj", "gitlab.example", "main", "feature/x", "aaa111")
	require.NoError(t, err)
	assert.True(t, merged)
}

func TestGitLabMergedHeadsBatchesAndPaginatesExactMatches(t *testing.T) {
	previous := gitlabGraphQLCall
	calls := 0
	gitlabGraphQLCall = func(host, query string, variables map[string]any) ([]byte, error) {
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

	matched, err := GitLabMergedHeads("group/project", "gitlab.example", "main", []SnapshotCandidate{
		{Branch: "feature", SHA: "aaa111"},
		{Branch: "reused", SHA: "bbb222"},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"feature": true, "reused": true}, matched)
	assert.Equal(t, 2, calls)
}

func TestGitLabMergedHeadsRejectsIncompletePage(t *testing.T) {
	previous := gitlabGraphQLCall
	gitlabGraphQLCall = func(string, string, map[string]any) ([]byte, error) {
		return []byte(`{"data":{"project":{"mergeRequests":{"nodes":[],"pageInfo":{"endCursor":null,"hasNextPage":true}}}}}`), nil
	}
	t.Cleanup(func() { gitlabGraphQLCall = previous })

	_, err := GitLabMergedHeads("group/project", "gitlab.example", "main", []SnapshotCandidate{{Branch: "feature", SHA: "aaa111"}})
	assert.Error(t, err)
}

func TestGitLabMergedHeadsRejectsUnchangedCursor(t *testing.T) {
	previous := gitlabGraphQLCall
	calls := 0
	gitlabGraphQLCall = func(string, string, map[string]any) ([]byte, error) {
		calls++
		return []byte(`{"data":{"project":{"mergeRequests":{"nodes":[],"pageInfo":{"endCursor":"stuck","hasNextPage":true}}}}}`), nil
	}
	t.Cleanup(func() { gitlabGraphQLCall = previous })

	_, err := GitLabMergedHeads("group/project", "gitlab.example", "main", []SnapshotCandidate{{Branch: "feature", SHA: "aaa111"}})

	require.EqualError(t, err, `glab: GitLab merged-MR query cursor did not advance from "stuck"`)
	assert.Equal(t, 2, calls)
}

func TestMergedPRHeadError(t *testing.T) {
	previousAPI := githubAPICall
	githubAPICall = func(string) ([]byte, error) { return nil, assert.AnError }
	t.Cleanup(func() { githubAPICall = previousAPI })

	_, err := MergedPRHead(GitHub, "owner/repo", "github.com", "main", "feature", "aaa111")
	assert.ErrorIs(t, err, assert.AnError)

	_, err = MergedPRHead(Type("bitbucket"), "owner/repo", "bitbucket.org", "main", "feature", "aaa111")
	assert.Error(t, err)
}
