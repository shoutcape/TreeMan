package deps_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shoutcape/treeman/internal/deps"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectInstaller(t *testing.T) {
	tests := []struct {
		name         string
		files        []string
		wantLockfile string
		wantBinary   string
		wantArgs     []string
	}{
		{
			name:         "pnpm",
			files:        []string{"pnpm-lock.yaml", "package.json"},
			wantLockfile: "pnpm-lock.yaml",
			wantBinary:   "pnpm",
			wantArgs:     []string{"install"},
		},
		{
			name:         "yarn",
			files:        []string{"yarn.lock", "package.json"},
			wantLockfile: "yarn.lock",
			wantBinary:   "yarn",
			wantArgs:     []string{"install"},
		},
		{
			name:         "npm",
			files:        []string{"package-lock.json", "package.json"},
			wantLockfile: "package-lock.json",
			wantBinary:   "npm",
			wantArgs:     []string{"install"},
		},
		{
			name:         "go mod",
			files:        []string{"go.mod", "go.sum", "main.go"},
			wantLockfile: "go.mod",
			wantBinary:   "go",
			wantArgs:     []string{"mod", "download"},
		},
		{
			name:         "cargo",
			files:        []string{"Cargo.toml", "src"},
			wantLockfile: "Cargo.toml",
			wantBinary:   "cargo",
			wantArgs:     []string{"fetch"},
		},
		{
			// pnpm takes priority over npm when both are present
			name:         "pnpm beats npm",
			files:        []string{"pnpm-lock.yaml", "package-lock.json"},
			wantLockfile: "pnpm-lock.yaml",
			wantBinary:   "pnpm",
			wantArgs:     []string{"install"},
		},
		{
			// yarn takes priority over npm
			name:         "yarn beats npm",
			files:        []string{"yarn.lock", "package-lock.json"},
			wantLockfile: "yarn.lock",
			wantBinary:   "yarn",
			wantArgs:     []string{"install"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deps.DetectInstaller(tt.files)
			require.NotNil(t, got)
			assert.Equal(t, tt.wantLockfile, got.Lockfile)
			assert.Equal(t, tt.wantBinary, got.Binary)
			assert.Equal(t, tt.wantArgs, got.Args)
		})
	}
}

func TestDetectInstaller_NoMatch(t *testing.T) {
	cases := []struct {
		name  string
		files []string
	}{
		{"empty", []string{}},
		{"python only", []string{"requirements.txt"}},
		{"pyproject only", []string{"pyproject.toml"}},
		{"unrecognised files", []string{"Gemfile.lock", "Cargo.lock"}},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Nil(t, deps.DetectInstaller(tt.files))
		})
	}
}

func TestIsPythonProject(t *testing.T) {
	assert.True(t, deps.IsPythonProject([]string{"requirements.txt"}))
	assert.True(t, deps.IsPythonProject([]string{"pyproject.toml", "README.md"}))
	assert.False(t, deps.IsPythonProject([]string{"go.mod", "main.go"}))
	assert.False(t, deps.IsPythonProject([]string{}))
}

func TestDiscoverNestedModules(t *testing.T) {
	dir := t.TempDir()
	output, err := exec.Command("git", "init", "--initial-branch=main", dir).CombinedOutput()
	require.NoErrorf(t, err, "git init: %s", output)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("generated/\n"), 0o644))
	for _, path := range []string{
		"go.mod",
		"apps/web/pnpm-lock.yaml",
		"packages/api/go.mod",
		"tools/script/pyproject.toml",
		"node_modules/ignored/package-lock.json",
		".git/ignored/go.mod",
		".worktrees/ignored/go.mod",
		".opencode/cache/go.mod",
		"vendor/ignored/go.mod",
		"generated/ignored/go.mod",
		".venv/ignored/pyproject.toml",
	} {
		fullPath := filepath.Join(dir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755))
		require.NoError(t, os.WriteFile(fullPath, []byte(""), 0644))
	}

	modules, err := deps.DiscoverNestedModules(dir)

	require.NoError(t, err)
	assert.Equal(t, []deps.Module{
		{Path: ".opencode/cache", Manifest: "go.mod"},
		{Path: "apps/web", Manifest: "pnpm-lock.yaml"},
		{Path: "packages/api", Manifest: "go.mod"},
		{Path: "tools/script", Manifest: "pyproject.toml"},
	}, modules)
}
