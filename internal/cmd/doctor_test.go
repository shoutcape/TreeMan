package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

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

	out := buf.String()
	assert.Contains(t, out, "PASS  Git repository")
	assert.Contains(t, out, "PASS  gh CLI")
	assert.Contains(t, out, "PASS  fzf")
	assert.Contains(t, out, "PASS  docker")
	assert.Contains(t, out, "PASS  Configuration: No .treeman.toml configured.")
	assert.Contains(t, out, "PASS  Database configuration: Branch databases are not configured.")
	assert.Contains(t, out, "WARN  Shell integration: Enable wrappers with: eval \"$(treeman init zsh)\"")
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

	out := buf.String()
	assert.Contains(t, out, "WARN  fzf: Install fzf")
	assert.Contains(t, out, "WARN  docker: Install and start Docker")
}

func TestRunDoctor_ReportsInvalidConfigWithoutProvisioning(t *testing.T) {
	repo, _ := createTestWorktree(t, "feature/doctor-config")
	chdirForTest(t, repo)
	t.Setenv("_TREEMAN_REMOTE_URL", "https://github.com/example/repo.git")
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte("[database\n"), 0o600))
	stubLookPath(t, func(string) error { return nil })

	buf := &bytes.Buffer{}
	runDoctor(commandWithOutput(buf), nil)

	assert.Contains(t, buf.String(), "FAIL  Configuration: Fix could not parse")
}

func TestRunDoctor_ReportsUnsupportedForge(t *testing.T) {
	repo, _ := createTestWorktree(t, "feature/doctor-forge")
	chdirForTest(t, repo)
	t.Setenv("_TREEMAN_REMOTE_URL", "https://example.com/org/repo.git")
	stubLookPath(t, func(string) error { return nil })

	buf := &bytes.Buffer{}
	runDoctor(commandWithOutput(buf), nil)

	assert.Contains(t, buf.String(), "FAIL  Forge: Set origin to github.com or a GitLab instance.")
}

func TestRunDoctor_ReportsRepositoryFailure(t *testing.T) {
	chdirForTest(t, t.TempDir())
	stubLookPath(t, func(string) error { return nil })

	buf := &bytes.Buffer{}
	runDoctor(commandWithOutput(buf), nil)

	assert.Contains(t, buf.String(), "FAIL  Repository: Run treeman doctor from a Git repository.")
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
