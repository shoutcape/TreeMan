package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreflightCommandReportsSetupCompatibilityWithoutCreatingWorktree(t *testing.T) {
	repo, _ := createTestWorktree(t, "feature/preflight")
	chdirForTest(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env"), []byte("DATABASE_URL=postgres://postgres@localhost:5432/app\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env.test"), []byte("test=true\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "pnpm-lock.yaml"), []byte("lockfileVersion: '9.0'\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte("[hooks]\npost_create = [\"make build\", \"make seed\"]\n"), 0o600))
	nestedModule := filepath.Join(repo, "apps", "admin", "go.mod")
	require.NoError(t, os.MkdirAll(filepath.Dir(nestedModule), 0o755))
	require.NoError(t, os.WriteFile(nestedModule, []byte("module example.com/admin\n"), 0o600))
	installPreflightInstaller(t, "pnpm")

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command := New("", "", "")
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs([]string{"preflight"})

	require.NoError(t, command.Execute())
	assert.Empty(t, stdout.String())
	assert.NoDirExists(t, filepath.Join(repo, ".worktrees"))

	out := ui.StripANSI(stderr.String())
	assert.Contains(t, out, "COMPATIBILITY PREFLIGHT")
	assert.Contains(t, out, "✓  Environment    ready: 2 .env file(s) will be copied")
	assert.Contains(t, out, "✓  Dependencies   ready: pnpm-lock.yaml detected; will run pnpm install")
	assert.Contains(t, out, "○  Nested module  apps/admin (go.mod): skipped; not installed automatically.")
	assert.Contains(t, out, "✓  Configuration  ready: .treeman.toml loaded")
	assert.Contains(t, out, "○  Database       not configured: add [database] to .treeman.toml")
	assert.Contains(t, out, "✓  Hooks          ready: 2 post-create hook(s) will run")
}

func TestPreflightCommandReportsNestedModulesWithoutRootDependencySetup(t *testing.T) {
	repo, _ := createTestWorktree(t, "feature/preflight-monorepo")
	chdirForTest(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("generated/\n"), 0o600))
	for _, module := range []string{
		filepath.Join(repo, "apps", "frontend", "package-lock.json"),
		filepath.Join(repo, "services", "backend", "go.mod"),
		filepath.Join(repo, ".opencode", "cache", "go.mod"),
		filepath.Join(repo, "generated", "legacy", "package-lock.json"),
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(module), 0o755))
		require.NoError(t, os.WriteFile(module, []byte{}, 0o600))
	}

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command := New("", "", "")
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs([]string{"preflight"})

	require.NoError(t, command.Execute())
	assert.Empty(t, stdout.String())
	assert.NoDirExists(t, filepath.Join(repo, ".worktrees"))

	out := ui.StripANSI(stderr.String())
	assert.Contains(t, out, "○  Dependencies   not configured at repository root")
	assert.Contains(t, out, "○  Nested module  apps/frontend (package-lock.json): skipped; not installed automatically.")
	assert.Contains(t, out, "○  Nested module  services/backend (go.mod): skipped; not installed automatically.")
	assert.Contains(t, out, "○  Configuration  not configured: .treeman.toml not found")
	assert.Contains(t, out, "○  Nested module  .opencode/cache (go.mod): skipped; not installed automatically.")
	assert.NotContains(t, out, "generated/legacy")
}

func installPreflightInstaller(t *testing.T, binary string) {
	t.Helper()
	binDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(binDir, binary), nil, 0o755))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestPreflightConfigurationStatusReportsInvalidConfiguration(t *testing.T) {
	result := config.LoadResult{Warning: "could not parse .treeman.toml"}
	status := preflightConfigurationStatus(result)

	assert.Equal(t, ui.ToneFailure, status.tone)
	assert.Equal(t, "unavailable: could not parse .treeman.toml", status.message)
	assert.Equal(t, "unavailable: configuration is invalid", preflightDatabaseStatus(t.TempDir(), result).message)
	assert.Equal(t, "unavailable: configuration is invalid", preflightHooksStatus(result).message)
}
