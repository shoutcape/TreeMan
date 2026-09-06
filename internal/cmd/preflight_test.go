package cmd

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
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
	worktreesBefore := gitTestOutput(t, repo, "worktree", "list", "--porcelain")
	refsBefore := gitTestOutput(t, repo, "show-ref")
	envBefore, err := os.ReadFile(filepath.Join(repo, ".env"))
	require.NoError(t, err)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command := New("", "", "")
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs([]string{"preflight"})

	require.NoError(t, command.Execute())
	assert.Empty(t, stdout.String())
	assert.NoDirExists(t, filepath.Join(repo, ".worktrees"))
	assert.Equal(t, worktreesBefore, gitTestOutput(t, repo, "worktree", "list", "--porcelain"))
	assert.Equal(t, refsBefore, gitTestOutput(t, repo, "show-ref"))
	envAfter, err := os.ReadFile(filepath.Join(repo, ".env"))
	require.NoError(t, err)
	assert.Equal(t, envBefore, envAfter)

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

func TestPreflightDoesNotMutateSetupStateInEitherOutputMode(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		json bool
	}{
		{name: "human", args: []string{"preflight"}},
		{name: "json", args: []string{"preflight", "--json"}, json: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo, _ := createTestWorktree(t, "feature/preflight-read-only-"+tt.name)
			chdirForTest(t, repo)
			stateHome := t.TempDir()
			t.Setenv("XDG_STATE_HOME", stateHome)
			require.NoError(t, os.WriteFile(filepath.Join(repo, ".env"), []byte("DATABASE_URL=postgres://postgres@localhost:5432/app\n"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(repo, "package-lock.json"), []byte("lockfileVersion: '9.0'\n"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte("[database]\nenv_key = \"DATABASE_URL\"\ncontainer = \"test-postgres\"\n\n[hooks]\npost_create = [\"preflight-hook-sentinel\"]\n"), 0o600))
			for _, module := range []string{"apps/admin/go.mod", "services/api/go.mod"} {
				path := filepath.Join(repo, module)
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				require.NoError(t, os.WriteFile(path, []byte("module example.com/test\n"), 0o600))
			}

			binDir := t.TempDir()
			installPreflightReadOnlyTool(t, binDir, "npm", "printf 'npm invoked\\n' >> \"$PREFLIGHT_COMMAND_LOG\"\n")
			installPreflightReadOnlyTool(t, binDir, "preflight-hook-sentinel", "printf 'hook invoked\\n' >> \"$PREFLIGHT_COMMAND_LOG\"\n")
			dockerLog := filepath.Join(t.TempDir(), "docker.log")
			installPreflightReadOnlyTool(t, binDir, "docker", "printf '%s\\n' \"$*\" >> \"$PREFLIGHT_DOCKER_LOG\"\nif [ \"$1\" = \"ps\" ]; then\n  printf '%s\\n' '{\"ID\":\"postgres-id\",\"Names\":\"test-postgres\",\"Image\":\"postgres:16\",\"Ports\":\"0.0.0.0:5432->5432/tcp\"}'\nfi\n")
			commandLog := filepath.Join(t.TempDir(), "commands.log")
			t.Setenv("PREFLIGHT_COMMAND_LOG", commandLog)
			t.Setenv("PREFLIGHT_DOCKER_LOG", dockerLog)
			t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			if tt.json {
				stubTerminalColor(t)
			}

			worktreesBefore := gitTestOutput(t, repo, "worktree", "list", "--porcelain")
			refsBefore := gitTestOutput(t, repo, "show-ref")
			envBefore := readTestFile(t, filepath.Join(repo, ".env"))
			lockBefore := readTestFile(t, filepath.Join(repo, "package-lock.json"))
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			command := New("", "", "")
			command.SetOut(stdout)
			command.SetErr(stderr)
			command.SetArgs(tt.args)

			require.NoError(t, command.Execute())
			assert.Equal(t, worktreesBefore, gitTestOutput(t, repo, "worktree", "list", "--porcelain"))
			assert.Equal(t, refsBefore, gitTestOutput(t, repo, "show-ref"))
			assert.Equal(t, envBefore, readTestFile(t, filepath.Join(repo, ".env")))
			assert.Equal(t, lockBefore, readTestFile(t, filepath.Join(repo, "package-lock.json")))
			assert.NoDirExists(t, filepath.Join(repo, ".worktrees"))
			assert.NoDirExists(t, filepath.Join(repo, ".treeman"))
			assert.NoDirExists(t, filepath.Join(repo, "node_modules"))
			assert.NoDirExists(t, filepath.Join(stateHome, "treeman"))
			assert.NoFileExists(t, commandLog)
			assert.Equal(t, "ps --no-trunc --format {{json .}}\n", readTestFile(t, dockerLog))

			if tt.json {
				assert.Empty(t, stderr.String())
				report := decodeReport(t, stdout)
				assert.True(t, report.OK)
				require.Len(t, report.Checks, 7)
				assert.Equal(t, []string{"environment", "dependencies", "nested_module", "nested_module", "configuration", "database", "hooks"}, checkNames(report.Checks))
				assert.Equal(t, jsonCheck{Name: "nested_module", Status: "info", Message: "apps/admin (go.mod): skipped; not installed automatically.", Hint: ""}, report.Checks[2])
				assert.Equal(t, jsonCheck{Name: "nested_module", Status: "info", Message: "services/api (go.mod): skipped; not installed automatically.", Hint: ""}, report.Checks[3])
			} else {
				assert.Empty(t, stdout.String())
				assert.Contains(t, ui.StripANSI(stderr.String()), "apps/admin (go.mod)")
				assert.Contains(t, ui.StripANSI(stderr.String()), "services/api (go.mod)")
			}
		})
	}
}

func installPreflightReadOnlyTool(t *testing.T, binDir, name, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\n"+body), 0o755))
}

func TestPreflightCommandWritesJSONReportWithoutCreatingWorktree(t *testing.T) {
	repo, _ := createTestWorktree(t, "feature/preflight-json")
	chdirForTest(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env"), []byte("KEY=value\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "apps", "admin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "apps", "admin", "go.mod"), []byte("module example.com/admin\n"), 0o600))
	worktreesBefore := gitTestOutput(t, repo, "worktree", "list", "--porcelain")
	refsBefore := gitTestOutput(t, repo, "show-ref")

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command := New("", "", "")
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs([]string{"preflight", "--json"})

	require.NoError(t, command.Execute())
	assert.Empty(t, stderr.String())
	assert.NoDirExists(t, filepath.Join(repo, ".worktrees"))
	assert.Equal(t, worktreesBefore, gitTestOutput(t, repo, "worktree", "list", "--porcelain"))
	assert.Equal(t, refsBefore, gitTestOutput(t, repo, "show-ref"))

	report := decodeReport(t, stdout)
	assert.True(t, report.OK)
	require.Len(t, report.Checks, 6)
	assert.Equal(t, []string{"environment", "dependencies", "nested_module", "configuration", "database", "hooks"}, checkNames(report.Checks))
	assert.Equal(t, jsonCheck{Name: "nested_module", Status: "info", Message: "apps/admin (go.mod): skipped; not installed automatically.", Hint: ""}, report.Checks[2])
}

func TestPreflightCommandWritesFailedChecksAsJSONButSucceeds(t *testing.T) {
	repo, _ := createTestWorktree(t, "feature/preflight-json-failure")
	chdirForTest(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte("worktree_dir = \"{branch}\"\n"), 0o600))

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command := New("", "", "")
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs([]string{"preflight", "--json"})

	require.NoError(t, command.Execute())
	assert.Empty(t, stderr.String())
	report := decodeReport(t, stdout)
	assert.False(t, report.OK)
	require.Len(t, report.Checks, 5)
	assert.Equal(t, []string{"environment", "dependencies", "configuration", "database", "hooks"}, checkNames(report.Checks))
	assert.Equal(t, jsonCheck{Name: "configuration", Status: "fail", Message: `unavailable: worktree_dir "{branch}" cannot use {branch}: a branch name may contain "/", so it is not a directory name; the branch slug is appended to worktree_dir automatically`, Hint: ""}, report.Checks[2])
	assert.Equal(t, jsonCheck{Name: "database", Status: "fail", Message: "unavailable: configuration is invalid", Hint: ""}, report.Checks[3])
	assert.Equal(t, jsonCheck{Name: "hooks", Status: "fail", Message: "unavailable: configuration is invalid", Hint: ""}, report.Checks[4])
}

func TestPreflightCommandWritesWarningAsColorlessJSON(t *testing.T) {
	repo, _ := createTestWorktree(t, "feature/preflight-json-warning")
	chdirForTest(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "package.json"), []byte(`{"packageManager":"yarn@4.9.2"}`), 0o600))
	gitPath, err := exec.LookPath("git")
	require.NoError(t, err)
	binDir := t.TempDir()
	require.NoError(t, os.Symlink(gitPath, filepath.Join(binDir, "git")))
	t.Setenv("PATH", binDir)
	stubTerminalColor(t)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command := New("", "", "")
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs([]string{"preflight", "--json"})

	require.NoError(t, command.Execute())
	assert.Empty(t, stderr.String())
	report := decodeReport(t, stdout)
	assert.True(t, report.OK)
	require.Len(t, report.Checks, 5)
	assert.Equal(t, jsonCheck{Name: "dependencies", Status: "warn", Message: "limited: package.json detected but corepack is not installed", Hint: ""}, report.Checks[1])
}

func TestPreflightCommandWritesRepositorySetupFailureAsJSON(t *testing.T) {
	chdirForTest(t, t.TempDir())
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command := New("", "", "")
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs([]string{"preflight", "--json"})

	err := command.Execute()

	require.EqualError(t, err, "not inside a git repository")
	assert.Empty(t, stderr.String())
	report := decodeReport(t, stdout)
	assert.False(t, report.OK)
	assert.Equal(t, []jsonCheck{{Name: "repository", Status: "fail", Message: "not inside a git repository", Hint: ""}}, report.Checks)
}

func TestPreflightCommandWritesWorktreeSetupFailureAsJSON(t *testing.T) {
	chdirForTest(t, t.TempDir())
	binDir := t.TempDir()
	gitStub := filepath.Join(binDir, "git")
	require.NoError(t, os.WriteFile(gitStub, []byte("#!/bin/sh\nif [ \"$1\" = \"rev-parse\" ]; then\n  printf '.git'\n  exit 0\nfi\nprintf 'worktree unavailable\\n' >&2\nexit 1\n"), 0o755))
	t.Setenv("PATH", binDir)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command := New("", "", "")
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs([]string{"preflight", "--json"})

	require.EqualError(t, command.Execute(), "could not list worktrees: git worktree list --porcelain: worktree unavailable")
	assert.Empty(t, stderr.String())
	report := decodeReport(t, stdout)
	assert.False(t, report.OK)
	assert.Equal(t, []jsonCheck{{Name: "worktree", Status: "fail", Message: "could not list worktrees: git worktree list --porcelain: worktree unavailable", Hint: ""}}, report.Checks)
}

func TestPreflightCommandPropagatesRepositorySetupJSONWriteFailure(t *testing.T) {
	chdirForTest(t, t.TempDir())
	writeErr := errors.New("write failed")
	command := New("", "", "")
	command.SetOut(reportErrorWriter{err: writeErr})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"preflight", "--json"})

	require.ErrorIs(t, command.Execute(), writeErr)
}

func TestPreflightCommandPropagatesWorktreeSetupJSONWriteFailure(t *testing.T) {
	chdirForTest(t, t.TempDir())
	binDir := t.TempDir()
	gitStub := filepath.Join(binDir, "git")
	require.NoError(t, os.WriteFile(gitStub, []byte("#!/bin/sh\nif [ \"$1\" = \"rev-parse\" ]; then\n  printf '.git'\n  exit 0\nfi\nprintf 'worktree unavailable\\n' >&2\nexit 1\n"), 0o755))
	t.Setenv("PATH", binDir)
	writeErr := errors.New("write failed")
	command := New("", "", "")
	command.SetOut(reportErrorWriter{err: writeErr})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"preflight", "--json"})

	require.ErrorIs(t, command.Execute(), writeErr)
}

func TestPreflightCommandPropagatesJSONWriteFailure(t *testing.T) {
	repo, _ := createTestWorktree(t, "feature/preflight-json-write-error")
	chdirForTest(t, repo)
	writeErr := errors.New("write failed")
	command := New("", "", "")
	command.SetOut(reportErrorWriter{err: writeErr})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"preflight", "--json"})

	require.ErrorIs(t, command.Execute(), writeErr)
}

func TestPreflightHelpIncludesJSONFlag(t *testing.T) {
	stdout := &bytes.Buffer{}
	command := New("", "", "")
	command.SetOut(stdout)
	command.SetArgs([]string{"preflight", "--help"})

	require.NoError(t, command.Execute())
	assert.Contains(t, stdout.String(), "--json")
}

func TestPreflightReportsCorepackManagedYarn(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"packageManager":"yarn@4.9.2"}`), 0o600))
	installPreflightInstaller(t, "corepack")

	statuses := preflightDependenciesStatuses(dir)

	require.NotEmpty(t, statuses)
	assert.Equal(t, "dependencies", statuses[0].id)
	assert.Equal(t, CheckPass, statuses[0].status)
	assert.Equal(t, "ready: package.json detected; will run corepack yarn install", statuses[0].message)
}

func TestPreflightReportsMissingCorepackForModernYarn(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"packageManager":"yarn@4.9.2"}`), 0o600))
	t.Setenv("PATH", "")

	statuses := preflightDependenciesStatuses(dir)

	require.NotEmpty(t, statuses)
	assert.Equal(t, CheckWarn, statuses[0].status)
	assert.Equal(t, "limited: package.json detected but corepack is not installed", statuses[0].message)
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

	assert.Equal(t, "configuration", status.id)
	assert.Equal(t, CheckFail, status.status)
	assert.Equal(t, "unavailable: could not parse .treeman.toml", status.message)
	assert.Equal(t, "unavailable: configuration is invalid", preflightDatabaseStatus(t.TempDir(), result).message)
	assert.Equal(t, "unavailable: configuration is invalid", preflightHooksStatus(result).message)
}

func TestPreflightRejectsInvalidWorktreeDirectoryAndExcludesConfiguredDirectory(t *testing.T) {
	repo, _ := createTestWorktree(t, "feature/preflight-path")
	chdirForTest(t, repo)
	configuredModule := filepath.Join(repo, "build", "trees", "old", "go.mod")
	unrelatedModule := filepath.Join(repo, "packages", "trees", "go.mod")
	for _, path := range []string{configuredModule, unrelatedModule} {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, nil, 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte("worktree_dir = \"build/trees\"\n"), 0o600))

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command := New("", "", "")
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs([]string{"preflight"})
	require.NoError(t, command.Execute())

	out := ui.StripANSI(stderr.String())
	assert.NotContains(t, out, "build/trees/old")
	assert.Contains(t, out, "packages/trees (go.mod)")

	require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte("worktree_dir = \"{branch}\"\n"), 0o600))
	stderr.Reset()
	require.NoError(t, command.Execute())
	assert.Contains(t, ui.StripANSI(stderr.String()), "Configuration  unavailable: worktree_dir")
}
