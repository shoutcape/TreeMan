package cmd

import (
	"bytes"
	"errors"
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
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte("[database]\nenv_key = \"DATABASE_URL\"\n\n[hooks]\npost_create = [\"make build\", \"make seed\"]\n"), 0o600))
	nestedModule := filepath.Join(repo, "apps", "admin", "go.mod")
	require.NoError(t, os.MkdirAll(filepath.Dir(nestedModule), 0o755))
	require.NoError(t, os.WriteFile(nestedModule, []byte("module example.com/admin\n"), 0o600))
	stubLookPath(t, func(string) error { return nil })
	stubPreflightDatabaseTarget(t, nil)

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
	assert.Contains(t, out, "✓  Database       ready: DATABASE_URL has a PostgreSQL URI")
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
	assert.NotContains(t, out, ".opencode/cache")
	assert.NotContains(t, out, "generated/legacy")
}

func stubPreflightDatabaseTarget(t *testing.T, err error) {
	t.Helper()
	previous := preflightDatabaseTarget
	preflightDatabaseTarget = func(string, string, string) error { return err }
	t.Cleanup(func() { preflightDatabaseTarget = previous })
}

func TestPreflightDatabaseStatusReportsUnavailableContainer(t *testing.T) {
	repo := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env"), []byte("DATABASE_URL=postgres://postgres@localhost:5432/app\n"), 0o600))
	stubPreflightDatabaseTarget(t, errors.New("Docker is unavailable"))

	status := preflightDatabaseStatus(repo, config.LoadResult{Config: config.Config{Database: &config.DatabaseConfig{EnvKey: "DATABASE_URL"}}})

	assert.Equal(t, ui.ToneWarning, status.tone)
	assert.Equal(t, "limited: no ready PostgreSQL container: Docker is unavailable", status.message)
}
