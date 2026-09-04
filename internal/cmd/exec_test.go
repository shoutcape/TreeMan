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

// execCommand builds a command that carries the --exec flag, so validate can
// tell an unset flag from an empty one.
func execCommand(t *testing.T, value string) (*cobra.Command, worktreeLaunchOptions) {
	t.Helper()
	var options worktreeLaunchOptions
	cmd := commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{})
	addLaunchFlag(cmd, &options)
	require.NoError(t, cmd.Flags().Set(execFlagName, value))
	return cmd, options
}

func TestDeliverWorktree_WithoutExecPrintsPath(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	record := stubLaunch(t, nil)

	require.NoError(t, deliverWorktree(commandWithOutput(stdout, stderr), "/tmp/worktree", worktreeLaunchOptions{}))

	assert.Equal(t, "/tmp/worktree\n", stdout.String())
	assert.False(t, record.called)
}

func TestDeliverWorktree_WithExecRunsCommandAndPrintsNoPath(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	record := stubLaunch(t, nil)

	require.NoError(t, deliverWorktree(commandWithOutput(stdout, stderr), "/tmp/worktree", worktreeLaunchOptions{command: "claude"}))

	assert.True(t, record.called)
	assert.Equal(t, "/tmp/worktree", record.dir)
	assert.Equal(t, "claude", record.command)
	// The launched command owns stdout, so the path contract does not apply.
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "Running claude in /tmp/worktree")
}

func TestDeliverWorktree_ReportsHandoverFailure(t *testing.T) {
	stubLaunch(t, errors.New("exec format error"))

	err := deliverWorktree(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), "/tmp/worktree", worktreeLaunchOptions{command: "claude"})

	assert.ErrorContains(t, err, "exec format error")
}

func TestLaunchOptions_ValidateRejectsEmptyExec(t *testing.T) {
	for _, value := range []string{"", "   "} {
		t.Run(fmt.Sprintf("%q", value), func(t *testing.T) {
			cmd, options := execCommand(t, value)

			assert.ErrorContains(t, options.validate(cmd), "--exec needs a command")
		})
	}
}

func TestLaunchOptions_ValidateAcceptsCommandAndUnsetFlag(t *testing.T) {
	cmd, options := execCommand(t, "nvim .")
	require.NoError(t, options.validate(cmd))

	var unset worktreeLaunchOptions
	bare := commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{})
	addLaunchFlag(bare, &unset)
	assert.NoError(t, unset.validate(bare))
}

func TestDeliverSwitchDestination_ExecRunsInCurrentWorktree(t *testing.T) {
	worktree := t.TempDir()
	chdirForTest(t, worktree)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	record := stubLaunch(t, nil)

	require.NoError(t, deliverSwitchDestination(commandWithOutput(stdout, stderr), worktree, worktreeLaunchOptions{command: "lazygit"}))

	assert.True(t, record.called)
	assert.Equal(t, worktree, record.dir)
	assert.NotContains(t, stderr.String(), "Already in this worktree.")
}

func TestDeliverSwitchDestination_WithoutExecReportsDestination(t *testing.T) {
	worktree := t.TempDir()
	chdirForTest(t, worktree)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	record := stubLaunch(t, nil)

	require.NoError(t, deliverSwitchDestination(commandWithOutput(stdout, stderr), worktree, worktreeLaunchOptions{}))

	assert.False(t, record.called)
	assert.Contains(t, stderr.String(), "Already in this worktree.")
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

// TestPosixShellIntegration_ExecBypassesCapture runs the generated function
// against a fake treeman. Without --exec the function reads stdout and changes
// directory; with --exec it must not, because the launched command owns the
// terminal that command substitution would take away.
func TestPosixShellIntegration_ExecBypassesCapture(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}

	binDir := t.TempDir()
	destination := t.TempDir()
	fake := filepath.Join(binDir, "treeman")
	require.NoError(t, os.WriteFile(fake, []byte("#!/bin/sh\nprintf '%s\\n' \""+destination+"\"\n"), 0o755))

	script := fmt.Sprintf(posixShellIntegration, "bash") + `
treeman switch "$@"
printf 'cwd=%s\n' "$PWD"
`
	run := func(args ...string) string {
		command := exec.Command(bash, append([]string{"-c", script, "bash"}, args...)...)
		command.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"))
		command.Dir = t.TempDir()
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
		return string(output)
	}

	assert.Contains(t, run(), "cwd="+destination, "without --exec the wrapper changes directory")
	for _, args := range [][]string{{"-x", "true"}, {"--exec", "true"}, {"--exec=true"}} {
		assert.NotContains(t, run(args...), "cwd="+destination, "--exec must not be captured: %v", args)
	}
}
