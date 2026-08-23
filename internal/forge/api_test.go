package forge

import (
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

func TestMergedPRHeadError(t *testing.T) {
	previousAPI := githubAPICall
	githubAPICall = func(string) ([]byte, error) { return nil, assert.AnError }
	t.Cleanup(func() { githubAPICall = previousAPI })

	_, err := MergedPRHead(GitHub, "owner/repo", "github.com", "main", "feature", "aaa111")
	assert.ErrorIs(t, err, assert.AnError)

	_, err = MergedPRHead(Type("bitbucket"), "owner/repo", "bitbucket.org", "main", "feature", "aaa111")
	assert.Error(t, err)
}
