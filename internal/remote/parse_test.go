package remote_test

import (
	"testing"

	"github.com/shoutcape/treeman/internal/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseHost(t *testing.T) {
	// All test cases mirror smoke-test.sh:202-210 exactly.
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"github ssh shorthand", "git@github.com:owner/repo.git", "github.com"},
		{"github https", "https://github.com/owner/repo.git", "github.com"},
		{"github ssh://", "ssh://git@github.com/owner/repo.git", "github.com"},
		{"gitlab.com ssh shorthand", "git@gitlab.com:group/project.git", "gitlab.com"},
		{"gitlab.com https", "https://gitlab.com/group/project.git", "gitlab.com"},
		{"self-hosted gitlab ssh", "git@gitlab.company.com:acme/frontend/webapp.git", "gitlab.company.com"},
		{"self-hosted gitlab https", "https://gitlab.company.com/acme/frontend/webapp.git", "gitlab.company.com"},
		{"ssh:// with port", "ssh://git@gitlab.company.com:2222/group/project.git", "gitlab.company.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := remote.ParseHost(tt.url)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseHost_Error(t *testing.T) {
	_, err := remote.ParseHost("ftp://example.com/repo")
	assert.Error(t, err)
}

func TestParsePath(t *testing.T) {
	// All test cases mirror smoke-test.sh:212-225 exactly.
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"github ssh .git", "git@github.com:owner/repo.git", "owner/repo"},
		{"github ssh no .git", "git@github.com:owner/repo", "owner/repo"},
		{"github https .git", "https://github.com/owner/repo.git", "owner/repo"},
		{"github https no .git", "https://github.com/owner/repo", "owner/repo"},
		{"github ssh://", "ssh://git@github.com/owner/repo.git", "owner/repo"},
		{"gitlab nested groups ssh", "git@gitlab.company.com:acme/frontend/webapp.git", "acme/frontend/webapp"},
		{"gitlab nested groups https", "https://gitlab.company.com/acme/frontend/webapp.git", "acme/frontend/webapp"},
		{"gitlab nested groups ssh://", "ssh://git@gitlab.company.com/acme/frontend/webapp.git", "acme/frontend/webapp"},
		{"ssh:// with port", "ssh://git@gitlab.company.com:2222/group/project.git", "group/project"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := remote.ParsePath(tt.url)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParsePath_Error(t *testing.T) {
	_, err := remote.ParsePath("ftp://example.com/repo")
	assert.Error(t, err)
}

func TestURLEncode(t *testing.T) {
	// Mirrors smoke-test.sh:242-245.
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"simple path", "owner/repo", "owner%2Frepo"},
		{"nested path", "acme/frontend/webapp", "acme%2Ffrontend%2Fwebapp"},
		{"no special chars", "ownerrepo", "ownerrepo"},
		{"dot and dash", "my-org/my.repo", "my-org%2Fmy.repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, remote.URLEncode(tt.in))
		})
	}
}
