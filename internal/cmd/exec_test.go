package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// launchRecord is one recorded handover, which a test inspects instead of
// letting the process be replaced.
type launchRecord struct {
	dir     string
	command string
	called  bool
}

// stubLaunch replaces the process handover for one test and reports what the
// command under test asked to run.
func stubLaunch(t *testing.T, err error) *launchRecord {
	t.Helper()
	record := &launchRecord{}
	previous := launchInWorktree
	launchInWorktree = func(dir, command string) error {
		record.dir, record.command, record.called = dir, command, true
		return err
	}
	t.Cleanup(func() { launchInWorktree = previous })
	return record
}

func TestDeliverWorktree_WithoutExecReportsPath(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	record := stubLaunch(t, nil)

	require.NoError(t, deliverWorktree(commandWithOutput(stdout, stderr), "/tmp/worktree", ""))

	assert.Equal(t, "/tmp/worktree\n", stdout.String())
	assert.False(t, record.called)
}

func TestDeliverWorktree_WithExecRunsCommandAndReportsNoPath(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	record := stubLaunch(t, nil)

	require.NoError(t, deliverWorktree(commandWithOutput(stdout, stderr), "/tmp/worktree", "claude"))

	assert.True(t, record.called)
	assert.Equal(t, "/tmp/worktree", record.dir)
	assert.Equal(t, "claude", record.command)
	// The launched command owns stdout, and there is no destination to report.
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "Running claude in /tmp/worktree")
}

func TestDeliverWorktree_ReportsHandoverFailure(t *testing.T) {
	stubLaunch(t, errors.New("exec format error"))

	err := deliverWorktree(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), "/tmp/worktree", "claude")

	assert.ErrorContains(t, err, "exec format error")
}

// reportingTo points TreeMan at a destination file for one test, standing in
// for the variable startup takes out of the environment.
func reportingTo(t *testing.T, file string) {
	t.Helper()
	previous := destinationFile
	destinationFile = file
	t.Cleanup(func() { destinationFile = previous })
}

// TestReportDestination_WritesTheCdFile covers the contract shell integration
// relies on: TreeMan never has to be captured to report where to cd.
func TestReportDestination_WritesTheCdFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "destination")
	reportingTo(t, file)
	stdout := &bytes.Buffer{}

	require.NoError(t, reportDestination(commandWithOutput(stdout, &bytes.Buffer{}), "/tmp/worktree"))

	contents, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/worktree\n", string(contents))
	assert.Empty(t, stdout.String(), "the destination file replaces the stdout contract, it does not double it")
}

func TestReportDestination_FallsBackToStdout(t *testing.T) {
	reportingTo(t, "")
	stdout := &bytes.Buffer{}

	require.NoError(t, reportDestination(commandWithOutput(stdout, &bytes.Buffer{}), "/tmp/worktree"))

	assert.Equal(t, "/tmp/worktree\n", stdout.String())
}

func TestReportDestination_ReportsAnUnwritableCdFile(t *testing.T) {
	reportingTo(t, filepath.Join(t.TempDir(), "absent", "destination"))

	err := reportDestination(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), "/tmp/worktree")

	assert.ErrorContains(t, err, "could not report")
}

// TestTakeDestinationFile_KeepsTheHandshakeFromChildren proves the invariant
// that makes --exec safe. The variable belongs to the one TreeMan the wrapper
// started: a child that could still see it would write its own destination into
// the wrapper's file, and the wrapper would cd there once the launched command
// exited.
func TestTakeDestinationFile_KeepsTheHandshakeFromChildren(t *testing.T) {
	file := filepath.Join(t.TempDir(), "destination")
	t.Setenv(cdFileEnv, file)

	assert.Equal(t, file, takeDestinationFile(), "startup keeps the file it took")

	assert.Empty(t, os.Getenv(cdFileEnv))
	inherited, err := exec.Command("sh", "-c", `printf %s "$`+cdFileEnv+`"`).Output()
	require.NoError(t, err)
	assert.Empty(t, string(inherited), "a hook or an --exec command must not inherit the destination file")
}

// TestLaunchFlag_RejectsEmptyExec exercises the check through Execute, because
// registering the flag is what installs it -- a command cannot forget to call
// it, and it must run before RunE creates anything.
func TestLaunchFlag_RejectsEmptyExec(t *testing.T) {
	for _, value := range []string{"", "   "} {
		t.Run(fmt.Sprintf("%q", value), func(t *testing.T) {
			ran := false
			cmd := launchFlagCommand(&ran)
			cmd.SetArgs([]string{"--" + execFlagName, value})

			assert.ErrorContains(t, cmd.Execute(), "--exec needs a command")
			assert.False(t, ran, "the check runs before the command does any work")
		})
	}
}

func TestLaunchFlag_AcceptsCommandAndUnsetFlag(t *testing.T) {
	for name, args := range map[string][]string{
		"a command": {"--" + execFlagName, "nvim ."},
		"unset":     {},
	} {
		t.Run(name, func(t *testing.T) {
			ran := false
			cmd := launchFlagCommand(&ran)
			cmd.SetArgs(args)

			require.NoError(t, cmd.Execute())
			assert.True(t, ran)
		})
	}
}

// launchFlagCommand builds the smallest command that carries --exec, and
// records whether its work ran.
func launchFlagCommand(ran *bool) *cobra.Command {
	var execCommand string
	cmd := commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{})
	cmd.RunE = func(*cobra.Command, []string) error {
		*ran = true
		return nil
	}
	addLaunchFlag(cmd, &execCommand)
	return cmd
}

func TestWorktreeCommands_AcceptExecFlag(t *testing.T) {
	for _, name := range []string{"create", "branch", "review", "switch"} {
		t.Run(name, func(t *testing.T) {
			command, _, err := New("test", "", "").Find([]string{name})
			require.NoError(t, err)

			flag := command.Flags().Lookup(execFlagName)
			require.NotNil(t, flag)
			assert.Equal(t, "x", flag.Shorthand)
		})
	}
}

// TestShellIntegration_ChangesDirectory runs each generated wrapper against a
// fake treeman. The wrapper reads one path from the destination file, so it
// needs to know nothing about TreeMan's flags: a run that reports no
// destination -- what --exec does -- leaves the shell where it was.
func TestShellIntegration_ChangesDirectory(t *testing.T) {
	for _, shell := range []struct {
		name        string
		integration string
		reportPWD   string
	}{
		{name: "bash", integration: fmt.Sprintf(posixShellIntegration, "bash"), reportPWD: `printf 'cwd=%s\n' "$PWD"`},
		{name: "zsh", integration: fmt.Sprintf(posixShellIntegration, "zsh"), reportPWD: `printf 'cwd=%s\n' "$PWD"`},
		{name: "fish", integration: fishShellIntegration, reportPWD: `printf 'cwd=%s\n' $PWD`},
	} {
		t.Run(shell.name, func(t *testing.T) {
			shellPath, err := exec.LookPath(shell.name)
			if err != nil {
				t.Skipf("%s is not installed", shell.name)
			}
			destination := t.TempDir()
			binDir := fakeTreeman(t, destination)

			// TreeMan's arguments belong in the script. Passing them as the
			// shell's own arguments makes fish read a leading dash as one of
			// its options, and makes the POSIX shells need a $0 first.
			run := func(treemanArgs string) string {
				script := shell.integration + "\ntreeman switch " + treemanArgs + "\n" + shell.reportPWD
				command := exec.Command(shellPath, "-c", script)
				command.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"))
				command.Dir = t.TempDir()
				output, err := command.CombinedOutput()
				require.NoError(t, err, string(output))
				return string(output)
			}

			assert.Contains(t, run(""), "cwd="+destination,
				"a reported destination changes the shell directory")
			assert.NotContains(t, run("--exec true"), "cwd="+destination,
				"--exec reports no destination, so the shell stays put")
			assert.Contains(t, run("--exec true"), "exec-ran",
				"the launched command still owns stdout")
		})
	}
}

// TestShellIntegration_WithoutMktemp covers the one dependency the wrapper
// has. TreeMan must still run, and the lost directory change must be
// explained rather than silently dropped.
func TestShellIntegration_WithoutMktemp(t *testing.T) {
	shellPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	destination := t.TempDir()
	binDir := fakeTreeman(t, destination)

	script := fmt.Sprintf(posixShellIntegration, "bash") + "\ntreeman switch\nprintf 'cwd=%s\\n' \"$PWD\""
	command := exec.Command(shellPath, "-c", script)
	command.Env = append(os.Environ(), "PATH="+binDir) // no mktemp
	command.Dir = t.TempDir()

	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	assert.NotContains(t, string(output), "cwd="+destination)
	assert.Contains(t, string(output), "mktemp is unavailable")
}

// fakeTreeman installs a treeman that reports destination through the
// destination file, unless it was asked to exec -- which is how the real
// binary behaves once a command takes over.
func fakeTreeman(t *testing.T, destination string) string {
	t.Helper()
	binDir := t.TempDir()
	script := "#!/bin/sh\n" +
		"for arg in \"$@\"; do\n" +
		"  case \"$arg\" in --exec|-x) echo exec-ran; exit 0 ;; esac\n" +
		"done\n" +
		"printf '%s\\n' \"" + destination + "\" > \"$TREEMAN_CD_FILE\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "treeman"), []byte(script), 0o755))
	return binDir
}
