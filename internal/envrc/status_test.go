package envrc

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetect(t *testing.T) {
	path := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(path, ".envrc"), []byte("use nix\n"), 0644))

	tests := []struct {
		name   string
		lookup func(string) (string, error)
		getenv func(string) string
		want   []ToolStatus
	}{
		{
			name:   "unavailable tools",
			lookup: func(string) (string, error) { return "", errors.New("not found") },
			getenv: func(string) string { return "" },
			want:   []ToolStatus{{Name: "direnv", Status: Unavailable}, {Name: "Nix", Status: Unavailable}},
		},
		{
			name:   "available tools",
			lookup: func(binary string) (string, error) { return "/bin/" + binary, nil },
			getenv: func(string) string { return "" },
			want:   []ToolStatus{{Name: "direnv", Status: Available}, {Name: "Nix", Status: Available}},
		},
		{
			name:   "active tools",
			lookup: func(binary string) (string, error) { return "/bin/" + binary, nil },
			getenv: func(name string) string {
				if name == "DIRENV_FILE" {
					return filepath.Join(path, ".envrc")
				}
				return "pure"
			},
			want: []ToolStatus{{Name: "direnv", Status: Active}, {Name: "Nix", Status: ActiveInCurrentShell}},
		},
		{
			name:   "active environment without executable",
			lookup: func(string) (string, error) { return "", errors.New("not found") },
			getenv: func(name string) string {
				if name == "DIRENV_FILE" {
					return filepath.Join(path, ".envrc")
				}
				return "pure"
			},
			want: []ToolStatus{{Name: "direnv", Status: Unavailable}, {Name: "Nix", Status: Unavailable}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, detect(path, test.lookup, test.getenv))
		})
	}
}

func TestDetect_NoEnvrc(t *testing.T) {
	lookedUp := false

	status := detect(t.TempDir(), func(string) (string, error) {
		lookedUp = true
		return "", nil
	}, func(string) string { return "" })

	assert.Empty(t, status)
	assert.False(t, lookedUp)
}
