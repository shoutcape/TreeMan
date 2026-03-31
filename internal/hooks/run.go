// Package hooks executes user-defined lifecycle commands from .treeman.toml.
//
// Hook commands run sequentially in the target worktree directory via the
// system shell. Failures are treated as warnings (best-effort) -- a failing
// hook never prevents worktree creation.
package hooks

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
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
func RunPostCreate(dir string, cmds []string) []RunResult {
	if len(cmds) == 0 {
		return nil
	}

	results := make([]RunResult, 0, len(cmds))
	for _, c := range cmds {
		err := runShellCommand(dir, c)
		results = append(results, RunResult{Command: c, Err: err})
	}
	return results
}

// runShellCommand executes a single command string via the system shell.
func runShellCommand(dir, command string) error {
	shell, flag := shellCmd()

	cmd := exec.Command(shell, flag, command)
	cmd.Dir = dir
	cmd.Stdout = os.Stderr // hooks output goes to stderr (stdout is reserved for the path)
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("command %q failed: %w", command, err)
	}
	return nil
}

// shellCmd returns the shell binary and flag for running a command string.
func shellCmd() (string, string) {
	if runtime.GOOS == "windows" {
		return "cmd", "/C"
	}
	return "sh", "-c"
}
