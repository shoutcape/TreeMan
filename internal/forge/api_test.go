package forge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchRef(t *testing.T) {
	tests := []struct {
		name     string
		forge    Type
		number   int
		expected string
	}{
		{"github PR 42", GitHub, 42, "pull/42/head"},
		{"github PR 1", GitHub, 1, "pull/1/head"},
		{"gitlab MR 7", GitLab, 7, "merge-requests/7/head"},
		{"gitlab MR 100", GitLab, 100, "merge-requests/100/head"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, FetchRef(tt.forge, tt.number))
		})
	}
}

func TestCLITool(t *testing.T) {
	assert.Equal(t, "gh", CLITool(GitHub))
	assert.Equal(t, "glab", CLITool(GitLab))
	assert.Equal(t, "", CLITool(Type("unknown")))
}

func TestForgeAPIArgsPaginate(t *testing.T) {
	assert.Equal(t, []string{"api", "repos/owner/repo/branches?per_page=100", "--paginate"},
		ghAPIArgs("repos/owner/repo/branches?per_page=100"))
	assert.Equal(t, []string{"api", "projects/group%2Frepo/merge_requests?per_page=100", "--hostname", "gitlab.example", "--paginate"},
		glabAPIArgs("gitlab.example", "projects/group%2Frepo/merge_requests?per_page=100"))
}

func TestResolveFromRemote_GitHub(t *testing.T) {
	forgeType, repoSlug, host, err := ResolveFromRemote("git@github.com:owner/my-repo.git")
	require.NoError(t, err)
	assert.Equal(t, GitHub, forgeType)
	assert.Equal(t, "owner/my-repo", repoSlug)
	assert.Equal(t, "github.com", host)
}

func TestResolveFromRemote_GitLab(t *testing.T) {
	forgeType, repoSlug, host, err := ResolveFromRemote("git@gitlab.company.com:group/subgroup/proj.git")
	require.NoError(t, err)
	assert.Equal(t, GitLab, forgeType)
	assert.Equal(t, "group/subgroup/proj", repoSlug)
	assert.Equal(t, "gitlab.company.com", host)
}

func TestResolveFromRemote_GitLabHTTPS(t *testing.T) {
	forgeType, repoSlug, host, err := ResolveFromRemote("https://gitlab.com/mygroup/myproject.git")
	require.NoError(t, err)
	assert.Equal(t, GitLab, forgeType)
	assert.Equal(t, "mygroup/myproject", repoSlug)
	assert.Equal(t, "gitlab.com", host)
}

func TestResolveFromRemote_Unsupported(t *testing.T) {
	_, _, _, err := ResolveFromRemote("https://bitbucket.org/owner/repo.git")
	assert.Error(t, err)
}

func TestResolveFromRemote_InvalidURL(t *testing.T) {
	_, _, _, err := ResolveFromRemote("not-a-url")
	assert.Error(t, err)
}

func TestParseGithubMergedHeadSHAs(t *testing.T) {
	tests := []struct {
		name        string
		payload     string
		expected    []string
		expectError bool
	}{
		{
			name:     "merged PR head sha",
			payload:  `[{"merged_at":"2026-08-01T10:00:00Z","head":{"sha":"aaa111"}}]`,
			expected: []string{"aaa111"},
		},
		{
			name:     "closed unmerged excluded",
			payload:  `[{"merged_at":null,"head":{"sha":"bbb222"}}]`,
			expected: nil,
		},
		{
			name: "mixed keeps merged only",
			payload: `[{"number":2,"merged_at":null,"head":{"sha":"ccc333"}},` +
				`{"number":1,"merged_at":"2026-08-01T10:00:00Z","head":{"sha":"ddd444"}}]`,
			expected: []string{"ddd444"},
		},
		{
			name:     "missing head sha skipped",
			payload:  `[{"merged_at":"2026-08-01T10:00:00Z"}]`,
			expected: nil,
		},
		{
			name:     "empty list",
			payload:  `[]`,
			expected: nil,
		},
		{
			// gh api --paginate emits each page as its own JSON array.
			name:     "concatenated pages",
			payload:  `[{"merged_at":"2026-08-01T10:00:00Z","head":{"sha":"eee555"}}][{"merged_at":"2026-08-02T10:00:00Z","head":{"sha":"fff666"}}]`,
			expected: []string{"eee555", "fff666"},
		},
		{
			name:        "invalid json surfaces error",
			payload:     `not-json`,
			expectError: true,
		},
		{
			name:        "empty output surfaces error",
			payload:     ``,
			expectError: true,
		},
		{
			name:        "whitespace output surfaces error",
			payload:     " \n\t ",
			expectError: true,
		},
		{
			name:        "null surfaces error",
			payload:     `null`,
			expectError: true,
		},
		{
			name:        "non-array document surfaces error",
			payload:     `{}`,
			expectError: true,
		},
		{
			name:        "trailing garbage surfaces error",
			payload:     `[] trailing`,
			expectError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shas, err := parseGithubMergedHeadSHAs([]byte(tt.payload))
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, shas)
		})
	}
}

func TestParseGitlabMergedHeadSHAs(t *testing.T) {
	tests := []struct {
		name        string
		payload     string
		expected    []string
		expectError bool
	}{
		{
			name:     "merged MR source sha",
			payload:  `[{"iid":7,"state":"merged","sha":"aaa111"}]`,
			expected: []string{"aaa111"},
		},
		{
			name:     "empty list",
			payload:  `[]`,
			expected: nil,
		},
		{
			name:     "concatenated pages",
			payload:  `[{"sha":"bbb222"}][{"sha":"ccc333"}]`,
			expected: []string{"bbb222", "ccc333"},
		},
		{
			name:        "invalid json surfaces error",
			payload:     `oops`,
			expectError: true,
		},
		{
			name:        "empty output surfaces error",
			payload:     ``,
			expectError: true,
		},
		{
			name:        "whitespace output surfaces error",
			payload:     " \n\t ",
			expectError: true,
		},
		{
			name:        "null surfaces error",
			payload:     `null`,
			expectError: true,
		},
		{
			name:        "non-array document surfaces error",
			payload:     `{}`,
			expectError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shas, err := parseGitlabMergedHeadSHAs([]byte(tt.payload))
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, shas)
		})
	}
}

func TestMergedPRHeadSHAs_GitHub(t *testing.T) {
	var capturedEndpoint string
	previousAPI := githubAPICall
	githubAPICall = func(endpoint string) ([]byte, error) {
		capturedEndpoint = endpoint
		return []byte(`[{"merged_at":"2026-08-01T10:00:00Z","head":{"sha":"aaa111"}}]`), nil
	}
	t.Cleanup(func() { githubAPICall = previousAPI })

	shas, err := MergedPRHeadSHAs(GitHub, "owner/repo", "github.com", "feature/x")
	require.NoError(t, err)
	assert.Equal(t, []string{"aaa111"}, shas)
	assert.Equal(t,
		"repos/owner/repo/pulls?state=closed&head=owner%3Afeature%2Fx&per_page=100",
		capturedEndpoint)

	githubAPICall = func(string) ([]byte, error) { return []byte(`[{"merged_at":null,"head":{"sha":"bbb222"}}]`), nil }
	shas, err = MergedPRHeadSHAs(GitHub, "owner/repo", "github.com", "feature")
	require.NoError(t, err)
	assert.Empty(t, shas)
}

func TestMergedPRHeadSHAs_GitHub_Error(t *testing.T) {
	previousAPI := githubAPICall
	githubAPICall = func(string) ([]byte, error) { return nil, assert.AnError }
	t.Cleanup(func() { githubAPICall = previousAPI })

	_, err := MergedPRHeadSHAs(GitHub, "owner/repo", "github.com", "feature")
	assert.ErrorIs(t, err, assert.AnError)
}

func TestMergedPRHeadSHAs_GitLab(t *testing.T) {
	var capturedHost, capturedEndpoint string
	previousAPI := glabAPICall
	glabAPICall = func(host, endpoint string) ([]byte, error) {
		capturedHost = host
		capturedEndpoint = endpoint
		return []byte(`[{"iid":3,"state":"merged","sha":"aaa111"}]`), nil
	}
	t.Cleanup(func() { glabAPICall = previousAPI })

	shas, err := MergedPRHeadSHAs(GitLab, "group/proj", "gitlab.example", "feature/x")
	require.NoError(t, err)
	assert.Equal(t, []string{"aaa111"}, shas)
	assert.Equal(t, "gitlab.example", capturedHost)
	assert.Equal(t,
		"projects/group%2Fproj/merge_requests?source_branch=feature%2Fx&state=merged&per_page=100",
		capturedEndpoint)

	glabAPICall = func(string, string) ([]byte, error) { return []byte(`[]`), nil }
	shas, err = MergedPRHeadSHAs(GitLab, "group/proj", "gitlab.example", "feature")
	require.NoError(t, err)
	assert.Empty(t, shas)
}

func TestMergedPRHeadSHAs_UnsupportedForge(t *testing.T) {
	_, err := MergedPRHeadSHAs(Type("bitbucket"), "owner/repo", "bitbucket.org", "feature")
	assert.Error(t, err)
}
