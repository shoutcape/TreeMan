package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunDoctor_ReportsReadyRepository(t *testing.T) {
	repo, _ := createTestWorktree(t, "feature/doctor")
	chdirForTest(t, repo)
	t.Setenv("_TREEMAN_REMOTE_URL", "https://github.com/example/repo.git")
	t.Setenv("SHELL", "/bin/zsh")
	stubLookPath(t, func(string) error { return nil })

	buf := &bytes.Buffer{}
	runDoctor(commandWithOutput(buf), nil)

	out := ui.StripANSI(buf.String())
	assert.Contains(t, out, "DIAGNOSTICS")
	assert.Contains(t, out, "✓  Repository           Git repository detected")
	assert.Contains(t, out, "✓  Forge CLI            GitHub repository; gh installed")
	assert.Contains(t, out, "○  Configuration        No .treeman.toml found; optional setup disabled")
	assert.Contains(t, out, "○  Database setup       Not configured; add [database] to enable")
	assert.Contains(t, out, "✓  Interactive picker   fzf installed")
	assert.Contains(t, out, "✓  Container support    Docker installed; daemon unchecked")
	assert.Contains(t, out, "○  Shell integration    Cannot verify from a subprocess")
	assert.Contains(t, out, "To enable future shells, add to ~/.zshrc:")
	assert.Contains(t, out, "eval \"$(treeman init zsh)\"")
	assert.Contains(t, out, "4 passed · 3 informational")
}

func TestRunDoctor_ReportsMissingOptionalToolsWithRecovery(t *testing.T) {
	repo, _ := createTestWorktree(t, "feature/doctor-warn")
	chdirForTest(t, repo)
	t.Setenv("_TREEMAN_REMOTE_URL", "https://github.com/example/repo.git")
	stubLookPath(t, func(tool string) error {
		if tool == "fzf" || tool == "docker" {
			return fmt.Errorf("not found")
		}
		return nil
	})

	buf := &bytes.Buffer{}
	runDoctor(commandWithOutput(buf), nil)

	out := ui.StripANSI(buf.String())
	assert.Contains(t, out, "!  Interactive picker   fzf not installed")
	assert.Contains(t, out, "Install fzf: https://github.com/junegunn/fzf")
	assert.Contains(t, out, "!  Container support    Docker not installed")
	assert.Contains(t, out, "Install and start Docker: https://docs.docker.com/get-docker/")
}

func TestRunDoctor_ReportsInvalidConfigWithoutProvisioning(t *testing.T) {
	repo, _ := createTestWorktree(t, "feature/doctor-config")
	chdirForTest(t, repo)
	t.Setenv("_TREEMAN_REMOTE_URL", "https://github.com/example/repo.git")
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte("[database\n"), 0o600))
	stubLookPath(t, func(string) error { return nil })

	buf := &bytes.Buffer{}
	runDoctor(commandWithOutput(buf), nil)

	out := ui.StripANSI(buf.String())
	assert.Contains(t, out, "✗  Configuration        Invalid .treeman.toml")
	assert.Contains(t, out, "Fix could not parse")
}

func TestRunDoctor_ReportsUnsupportedForge(t *testing.T) {
	repo, _ := createTestWorktree(t, "feature/doctor-forge")
	chdirForTest(t, repo)
	t.Setenv("_TREEMAN_REMOTE_URL", "https://example.com/org/repo.git")
	stubLookPath(t, func(string) error { return nil })

	buf := &bytes.Buffer{}
	runDoctor(commandWithOutput(buf), nil)

	out := ui.StripANSI(buf.String())
	assert.Contains(t, out, "✗  Forge CLI            Unsupported forge")
	assert.Contains(t, out, "Set origin to github.com or a GitLab instance.")
}

func TestRunDoctor_ReportsRepositoryFailure(t *testing.T) {
	chdirForTest(t, t.TempDir())
	stubLookPath(t, func(string) error { return nil })

	buf := &bytes.Buffer{}
	runDoctor(commandWithOutput(buf), nil)

	out := ui.StripANSI(buf.String())
	assert.Contains(t, out, "✗  Repository           Not detected")
	assert.Contains(t, out, "Run treeman doctor from a Git repository.")
}

func commandWithOutput(out *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(out)
	return cmd
}

func stubLookPath(t *testing.T, lookup func(string) error) {
	t.Helper()
	previous := lookPath
	lookPath = func(file string) (string, error) {
		if err := lookup(file); err != nil {
			return "", err
		}
		return "/test/bin/" + file, nil
	}
	t.Cleanup(func() { lookPath = previous })
}
