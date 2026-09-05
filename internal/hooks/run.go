// Package hooks executes user-defined lifecycle commands from .treeman.toml.
//
// Hook commands run sequentially in the target worktree directory via the
// system shell. Failures are treated as warnings (best-effort) -- a failing
// hook never prevents worktree creation.
package hooks

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/shoutcape/treeman/internal/sh"
)

// RunResult describes the outcome of running a single hook command.
type RunResult struct {
	// Command is the shell command that was executed.
	Command string
	// Err is non-nil if the command failed.
	Err error
}

// RunPostCreate executes each command in cmds sequentially, using the system
// shell (sh -c on Unix, cmd /C on Windows). Each command runs with dir as
// its working directory.
//
// All commands are attempted regardless of individual failures. The caller
// should treat errors as warnings.
func RunPostCreate(dir string, cmds []string, outputs ...io.Writer) []RunResult {
	if len(cmds) == 0 {
		return nil
	}
	output := io.Writer(os.Stderr)
	if len(outputs) > 0 && outputs[0] != nil {
		output = outputs[0]
	}

	results := make([]RunResult, 0, len(cmds))
	for _, c := range cmds {
		err := runShellCommand(dir, c, output)
		results = append(results, RunResult{Command: c, Err: err})
	}
	return results
}

// runShellCommand executes a single command string via the system shell.
func runShellCommand(dir, command string, output io.Writer) error {
	shell, flag := sh.Command()

	cmd := exec.Command(shell, flag, command)
	cmd.Dir = dir
	cmd.Stdout = output // hook output is status, so it joins TreeMan's own on stderr
	cmd.Stderr = output

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("command %q failed: %w", command, err)
	}
	return nil
}
