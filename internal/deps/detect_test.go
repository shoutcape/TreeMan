package deps_test

import (
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

func TestKnownInstallers_ReturnsCopy(t *testing.T) {
	a := deps.KnownInstallers()
	b := deps.KnownInstallers()
	// Mutating one should not affect the other or the package-level list.
	a[0].Binary = "mutated"
	assert.NotEqual(t, "mutated", b[0].Binary)
}
