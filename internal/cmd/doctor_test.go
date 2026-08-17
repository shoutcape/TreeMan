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

func TestDoctorCommand_SucceedsWithoutFailedDiagnostics(t *testing.T) {
	repo, _ := createTestWorktree(t, "feature/doctor")
	chdirForTest(t, repo)
	t.Setenv("_TREEMAN_REMOTE_URL", "https://github.com/example/repo.git")
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("HOME", t.TempDir())
	stubLookPath(t, func(string) error { return nil })
	stubDockerDaemonReady(t, nil)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command := New("", "", "")
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs([]string{"doctor"})

	require.NoError(t, command.Execute())
	assert.Empty(t, stdout.String())
	out := ui.StripANSI(stderr.String())
	assert.Contains(t, out, "DIAGNOSTICS")
	assert.Contains(t, out, "✓  Repository           Git repository detected")
	assert.Contains(t, out, "✓  Forge CLI            GitHub repository; gh installed")
	assert.Contains(t, out, "○  Configuration        No .treeman.toml found; optional setup disabled")
	assert.Contains(t, out, "○  Database setup       Not configured; add [database] to enable")
	assert.Contains(t, out, "✓  Interactive picker   fzf installed")
	assert.Contains(t, out, "✓  Container support    Docker installed; daemon ready")
	assert.Contains(t, out, "○  Shell integration    Not configured")
	assert.Contains(t, out, "Add to ~/.zshrc:")
	assert.Contains(t, out, "eval \"$(treeman init zsh)\"")
	assert.Contains(t, out, "4 passed · 3 informational")
}

func TestDoctorCommand_FailsAfterRenderingDiagnostics(t *testing.T) {
	chdirForTest(t, t.TempDir())
	t.Setenv("SHELL", "")
	stubLookPath(t, func(string) error { return nil })
	stubDockerDaemonReady(t, nil)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command := New("", "", "")
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs([]string{"doctor"})

	err := command.Execute()

	require.EqualError(t, err, "doctor found failed diagnostics; resolve them and rerun")
	assert.Empty(t, stdout.String())
	out := ui.StripANSI(stderr.String())
	assert.Contains(t, out, "✗  Repository           Not detected")
	assert.Contains(t, out, "Shell integration")
	assert.Contains(t, out, "failed")
}

func TestCollectDockerDiagnostic_DistinguishesUnavailableDaemon(t *testing.T) {
	stubLookPath(t, func(string) error { return nil })
	stubDockerDaemonReady(t, fmt.Errorf("daemon unavailable"))

	diagnostic := collectDockerDiagnostic()

	assert.Equal(t, diagnosticWarn, diagnostic.status)
	assert.Equal(t, "Docker installed; daemon unavailable", diagnostic.message)
	assert.Equal(t, "Start Docker, then rerun treeman doctor.", diagnostic.hint)
}

func TestCollectShellDiagnostic_DetectsConfiguredShellIntegration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")
	require.NoError(t, os.WriteFile(filepath.Join(home, ".zshrc"), []byte(`eval "$(treeman init zsh)"`), 0o600))

	diagnostic := collectShellDiagnostic()

	assert.Equal(t, diagnosticPass, diagnostic.status)
	assert.Equal(t, "Shell integration", diagnostic.name)
	assert.Equal(t, "Configured in ~/.zshrc", diagnostic.message)
}

func TestCollectShellDiagnostic_OnlyDetectsGeneratedEvalEntry(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		want     bool
	}{
		{name: "double quoted", contents: `eval "$(treeman init zsh)"`, want: true},
		{name: "whitespace and comment", contents: `  eval   '$( treeman   init  zsh )' # TreeMan`, want: true},
		{name: "arbitrary text", contents: `echo 'eval "$(treeman init zsh)"'`, want: false},
		{name: "wrong shell", contents: `eval "$(treeman init bash)"`, want: false},
		{name: "commented", contents: `# eval "$(treeman init zsh)"`, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, hasShellIntegration(test.contents, "zsh"))
		})
	}
}

func TestCollectShellDiagnostic_ReportsUnverifiableShells(t *testing.T) {
	tests := []struct {
		name    string
		shell   string
		message string
	}{
		{name: "unset", message: "SHELL is not set; integration cannot be verified"},
		{name: "unsupported", shell: "/usr/bin/fish", message: "Unsupported shell fish; only bash and zsh can be verified"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("SHELL", test.shell)

			diagnostic := collectShellDiagnostic()

			assert.Equal(t, diagnosticInfo, diagnostic.status)
			assert.Equal(t, test.message, diagnostic.message)
			assert.Empty(t, diagnostic.hint)
		})
	}
}

func TestRunDoctor_ReportsMissingOptionalToolsWithRecovery(t *testing.T) {
	repo, _ := createTestWorktree(t, "feature/doctor-warn")
	chdirForTest(t, repo)
	t.Setenv("_TREEMAN_REMOTE_URL", "https://github.com/example/repo.git")
	stubDockerDaemonReady(t, nil)
	stubLookPath(t, func(tool string) error {
		if tool == "fzf" || tool == "docker" {
			return fmt.Errorf("not found")
		}
		return nil
	})

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command := New("", "", "")
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs([]string{"doctor"})

	require.NoError(t, command.Execute())
	assert.Empty(t, stdout.String())

	out := ui.StripANSI(stderr.String())
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
	stubDockerDaemonReady(t, nil)

	buf := &bytes.Buffer{}
	require.Error(t, runDoctor(commandWithOutput(&bytes.Buffer{}, buf), nil))

	out := ui.StripANSI(buf.String())
	assert.Contains(t, out, "✗  Configuration        Invalid .treeman.toml")
	assert.Contains(t, out, "Fix could not parse")
}

func TestRunDoctor_ReportsUnsupportedForge(t *testing.T) {
	repo, _ := createTestWorktree(t, "feature/doctor-forge")
	chdirForTest(t, repo)
	t.Setenv("_TREEMAN_REMOTE_URL", "https://example.com/org/repo.git")
	stubLookPath(t, func(string) error { return nil })
	stubDockerDaemonReady(t, nil)

	buf := &bytes.Buffer{}
	require.Error(t, runDoctor(commandWithOutput(&bytes.Buffer{}, buf), nil))

	out := ui.StripANSI(buf.String())
	assert.Contains(t, out, "✗  Forge CLI            Unsupported forge")
	assert.Contains(t, out, "Set origin to github.com or a GitLab instance.")
}

func TestRunDoctor_ReportsRepositoryFailure(t *testing.T) {
	chdirForTest(t, t.TempDir())
	stubLookPath(t, func(string) error { return nil })
	stubDockerDaemonReady(t, nil)

	buf := &bytes.Buffer{}
	require.Error(t, runDoctor(commandWithOutput(&bytes.Buffer{}, buf), nil))

	out := ui.StripANSI(buf.String())
	assert.Contains(t, out, "✗  Repository           Not detected")
	assert.Contains(t, out, "Run treeman doctor from a Git repository.")
}

func commandWithOutput(out, err *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(out)
	cmd.SetErr(err)
	return cmd
}

func stubDockerDaemonReady(t *testing.T, err error) {
	t.Helper()
	previous := dockerDaemonReady
	dockerDaemonReady = func() error { return err }
	t.Cleanup(func() { dockerDaemonReady = previous })
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
