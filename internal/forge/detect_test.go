package forge_test

import (
	"testing"

	"github.com/shoutcape/treeman/internal/forge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectFromHost(t *testing.T) {
	// Mirrors smoke-test.sh:227-240.
	tests := []struct {
		name string
		host string
		want forge.Type
	}{
		{"github", "github.com", forge.GitHub},
		{"gitlab.com", "gitlab.com", forge.GitLab},
		{"self-hosted gitlab", "gitlab.company.com", forge.GitLab},
		{"gitlab subdomain", "git.gitlab.corp.io", forge.GitLab},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := forge.DetectFromHost(tt.host)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDetectFromHost_Unsupported(t *testing.T) {
	unsupported := []string{
		"bitbucket.org",
		"codeberg.org",
		"example.com",
	}

	for _, host := range unsupported {
		t.Run(host, func(t *testing.T) {
			_, err := forge.DetectFromHost(host)
			assert.Error(t, err, "expected error for unsupported host %q", host)
		})
	}
}
