package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shoutcape/treeman/internal/terminal"
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
	assert.NotContains(t, stderr.String(), "\x1b")
	out := ui.StripANSI(stderr.String())
	assert.Contains(t, out, "DIAGNOSTICS")
	assert.Contains(t, out, "READY")
	assert.Contains(t, out, "UNAVAILABLE OR NOT CONFIGURED")
	assert.Contains(t, out, "✓  Repository           Git repository detected")
	assert.Contains(t, out, "✓  Forge CLI            GitHub repository; gh installed")
	assert.Contains(t, out, "○  Configuration        No .treeman.toml found; optional setup disabled")
	assert.Contains(t, out, "○  Database setup       Not configured; add [database] to enable")
	assert.Contains(t, out, "✓  Interactive picker   fzf installed")
	assert.Contains(t, out, "✓  Container support    Docker installed; daemon ready")
	assert.Contains(t, out, "○  Shell integration    Not configured")
	assert.Contains(t, out, "Run: treeman shell install")
	assert.Contains(t, out, "4 passed · 3 informational")
	assert.Less(t, strings.Index(out, "READY"), strings.Index(out, "✓  Repository"))
	assert.Less(t, strings.Index(out, "✓  Container support"), strings.Index(out, "UNAVAILABLE OR NOT CONFIGURED"))
	assert.Less(t, strings.Index(out, "UNAVAILABLE OR NOT CONFIGURED"), strings.Index(out, "○  Configuration"))
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

func TestDoctorCommand_JSONReportsSuccessWithoutHumanOutput(t *testing.T) {
	repo, _ := createTestWorktree(t, "feature/doctor-json")
	chdirForTest(t, repo)
	t.Setenv("_TREEMAN_REMOTE_URL", "https://github.com/example/repo.git")
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("HOME", t.TempDir())
	stubLookPath(t, func(string) error { return nil })
	stubDockerDaemonReady(t, nil)
	stubTerminalColor(t)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command := New("", "", "")
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs([]string{"doctor", "--json"})

	require.NoError(t, command.Execute())
	assert.Empty(t, stderr.String())
	assert.NotContains(t, stdout.String(), "\x1b")
	assert.NotContains(t, stdout.String(), "DIAGNOSTICS")

	report := decodeReport(t, stdout)
	assert.True(t, report.OK)
	assert.Equal(t, []string{
		"repository", "forge_cli", "configuration", "database_setup",
		"interactive_picker", "container_support", "shell_integration",
	}, checkNames(report.Checks))
	assert.Equal(t, "pass", report.Checks[0].Status)
	assert.Equal(t, "info", report.Checks[2].Status)
	assert.Equal(t, "", report.Checks[0].Hint)
}

func TestDoctorCommand_JSONReportsFailuresAfterOutput(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T)
		checkName string
		message   string
	}{
		{
			name: "missing Git",
			setup: func(t *testing.T) {
				chdirForTest(t, t.TempDir())
				stubLookPath(t, func(tool string) error {
					if tool == "git" {
						return fmt.Errorf("not found")
					}
					return nil
				})
				stubDockerDaemonReady(t, nil)
			},
			checkName: "git",
			message:   "Not installed",
		},
		{
			name: "outside repository",
			setup: func(t *testing.T) {
				chdirForTest(t, t.TempDir())
				stubLookPath(t, func(string) error { return nil })
				stubDockerDaemonReady(t, nil)
			},
			checkName: "repository",
			message:   "Not detected",
		},
		{
			name: "unsupported forge",
			setup: func(t *testing.T) {
				repo, _ := createTestWorktree(t, "feature/doctor-json-forge")
				chdirForTest(t, repo)
				t.Setenv("_TREEMAN_REMOTE_URL", "https://example.com/org/repo.git")
				stubLookPath(t, func(string) error { return nil })
				stubDockerDaemonReady(t, nil)
			},
			checkName: "forge_cli",
			message:   "Unsupported forge",
		},
		{
			name: "invalid configuration",
			setup: func(t *testing.T) {
				repo, _ := createTestWorktree(t, "feature/doctor-json-config")
				chdirForTest(t, repo)
				t.Setenv("_TREEMAN_REMOTE_URL", "https://github.com/example/repo.git")
				require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte("[database\n"), 0o600))
				stubLookPath(t, func(string) error { return nil })
				stubDockerDaemonReady(t, nil)
			},
			checkName: "configuration",
			message:   "Invalid .treeman.toml",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.setup(t)
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			command := New("", "", "")
			command.SetOut(stdout)
			command.SetErr(stderr)
			command.SetArgs([]string{"doctor", "--json"})

			require.EqualError(t, command.Execute(), "doctor found failed diagnostics; resolve them and rerun")
			assert.Empty(t, stderr.String())
			report := decodeReport(t, stdout)
			assert.False(t, report.OK)
			assertDoctorCheck(t, report.Checks, test.checkName, "fail", test.message)
		})
	}
}

func TestDoctorCommand_JSONWarningsDoNotFail(t *testing.T) {
	repo, _ := createTestWorktree(t, "feature/doctor-json-warnings")
	chdirForTest(t, repo)
	t.Setenv("_TREEMAN_REMOTE_URL", "https://github.com/example/repo.git")
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
	command.SetArgs([]string{"doctor", "--json"})

	require.NoError(t, command.Execute())
	assert.Empty(t, stderr.String())
	report := decodeReport(t, stdout)
	assert.True(t, report.OK)
	assert.Contains(t, report.Checks, jsonCheck{Name: "interactive_picker", Status: "warn", Message: "fzf not installed", Hint: "Install fzf: https://github.com/junegunn/fzf"})
	assert.Contains(t, report.Checks, jsonCheck{Name: "container_support", Status: "warn", Message: "Docker not installed", Hint: "Install and start Docker: https://docs.docker.com/get-docker/"})
}

func TestDoctorCommand_HelpIncludesJSONFlag(t *testing.T) {
	stdout := &bytes.Buffer{}
	command := New("", "", "")
	command.SetOut(stdout)
	command.SetArgs([]string{"doctor", "--help"})

	require.NoError(t, command.Execute())
	assert.Contains(t, ui.StripANSI(stdout.String()), "--json")
}

func TestRunDoctor_PropagatesJSONWriteErrors(t *testing.T) {
	chdirForTest(t, t.TempDir())
	stubLookPath(t, func(string) error { return nil })
	stubDockerDaemonReady(t, nil)

	cmd := &cobra.Command{}
	writeErr := errors.New("write failed")
	cmd.SetOut(reportErrorWriter{err: writeErr})
	require.ErrorIs(t, runDoctor(cmd, true), writeErr)
}

func assertDoctorCheck(t *testing.T, checks []jsonCheck, name, status, message string) {
	t.Helper()
	for _, check := range checks {
		if check.Name == name {
			assert.Equal(t, status, check.Status)
			assert.Equal(t, message, check.Message)
			return
		}
	}
	t.Errorf("missing %q check", name)
}

func stubTerminalColor(t *testing.T) {
	t.Helper()
	previous := terminalCapabilities
	terminalCapabilities = func(io.Reader, io.Writer) terminal.Capabilities {
		return terminal.Capabilities{Color: true}
	}
	t.Cleanup(func() { terminalCapabilities = previous })
}

func TestCollectDockerDiagnostic_DistinguishesUnavailableDaemon(t *testing.T) {
	stubLookPath(t, func(string) error { return nil })
	stubDockerDaemonReady(t, fmt.Errorf("daemon unavailable"))

	diagnostic := collectDockerDiagnostic()

	assert.Equal(t, CheckWarn, diagnostic.status)
	assert.Equal(t, "Docker installed; daemon unavailable", diagnostic.message)
	assert.Equal(t, "Start Docker, then rerun treeman doctor.", diagnostic.hint)
}

func TestDiagnosticSummaryToneUsesHighestSeverity(t *testing.T) {
	assert.Equal(t, ui.ToneSuccess, diagnosticSummaryTone(map[CheckStatus]int{CheckPass: 2, CheckInfo: 1}))
	assert.Equal(t, ui.ToneWarning, diagnosticSummaryTone(map[CheckStatus]int{CheckPass: 2, CheckWarn: 1}))
	assert.Equal(t, ui.ToneFailure, diagnosticSummaryTone(map[CheckStatus]int{CheckPass: 2, CheckWarn: 1, CheckFail: 1}))
}

func TestWriteDiagnostics_OmitsEmptySections(t *testing.T) {
	tests := []struct {
		name           string
		diagnostics    []diagnostic
		presentSection string
		absentSection  string
	}{
		{
			name:           "only ready diagnostics",
			diagnostics:    []diagnostic{{status: CheckPass, name: "Repository", message: "Git repository detected"}},
			presentSection: "READY",
			absentSection:  "UNAVAILABLE OR NOT CONFIGURED",
		},
		{
			name: "only unavailable diagnostics",
			diagnostics: []diagnostic{
				{status: CheckInfo, name: "Database setup", message: "Not configured", hint: "Add [database] to enable"},
				{status: CheckWarn, name: "Container support", message: "Docker not installed"},
				{status: CheckFail, name: "Forge CLI", message: "Unsupported forge"},
			},
			presentSection: "UNAVAILABLE OR NOT CONFIGURED",
			absentSection:  "READY",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := &bytes.Buffer{}
			writeDiagnostics(output, ui.NewRenderer(output, terminal.Capabilities{}), test.diagnostics)

			out := ui.StripANSI(output.String())
			assert.Contains(t, out, test.presentSection)
			assert.NotContains(t, out, test.absentSection)
			for _, diagnostic := range test.diagnostics {
				assert.Contains(t, out, diagnostic.name)
				if diagnostic.hint != "" {
					assert.Contains(t, out, diagnostic.hint)
				}
			}
		})
	}
}

func TestCollectShellDiagnostic_DetectsConfiguredShellIntegration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")
	require.NoError(t, os.WriteFile(filepath.Join(home, ".zshrc"), []byte(`eval "$(treeman init zsh)"`), 0o600))

	diagnostic := collectShellDiagnostic()

	assert.Equal(t, CheckPass, diagnostic.status)
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

			assert.Equal(t, CheckInfo, diagnostic.status)
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
	require.Error(t, runDoctor(commandWithOutput(&bytes.Buffer{}, buf), false))

	out := ui.StripANSI(buf.String())
	assert.Contains(t, out, "✗  Configuration        Invalid .treeman.toml")
	assert.Contains(t, out, "Fix could not parse")
}

func TestRunDoctor_ReportsInvalidWorktreeDirectory(t *testing.T) {
	repo, _ := createTestWorktree(t, "feature/doctor-worktree-dir")
	chdirForTest(t, repo)
	t.Setenv("_TREEMAN_REMOTE_URL", "https://github.com/example/repo.git")
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte("worktree_dir = \"{branch}\"\n"), 0o600))
	stubLookPath(t, func(string) error { return nil })
	stubDockerDaemonReady(t, nil)

	buf := &bytes.Buffer{}
	require.Error(t, runDoctor(commandWithOutput(&bytes.Buffer{}, buf), false))

	out := ui.StripANSI(buf.String())
	assert.Contains(t, out, "✗  Configuration        Invalid .treeman.toml")
	assert.Contains(t, out, "cannot use {branch}")
}

func TestRunDoctor_ReportsUnsupportedForge(t *testing.T) {
	repo, _ := createTestWorktree(t, "feature/doctor-forge")
	chdirForTest(t, repo)
	t.Setenv("_TREEMAN_REMOTE_URL", "https://example.com/org/repo.git")
	stubLookPath(t, func(string) error { return nil })
	stubDockerDaemonReady(t, nil)

	buf := &bytes.Buffer{}
	require.Error(t, runDoctor(commandWithOutput(&bytes.Buffer{}, buf), false))

	out := ui.StripANSI(buf.String())
	assert.Contains(t, out, "✗  Forge CLI            Unsupported forge")
	assert.Contains(t, out, "Set origin to github.com or a GitLab instance.")
}

func TestRunDoctor_ReportsRepositoryFailure(t *testing.T) {
	chdirForTest(t, t.TempDir())
	stubLookPath(t, func(string) error { return nil })
	stubDockerDaemonReady(t, nil)

	buf := &bytes.Buffer{}
	require.Error(t, runDoctor(commandWithOutput(&bytes.Buffer{}, buf), false))

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
