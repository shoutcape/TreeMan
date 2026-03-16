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
