// Package launch replaces the TreeMan process with a user command.
//
// A launched command takes over the terminal in a worktree that TreeMan has
// made ready. TreeMan does not stay in the process tree and does not wait for
// the command: the command inherits the terminal and reports its own exit
// status to the shell that started TreeMan.
package launch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/shoutcape/treeman/internal/sh"
)

// InDir replaces the running process with command, which the system shell runs
// with dir as its working directory.
//
// It returns only when the handover fails, so a caller must treat a nil return
// as unreachable.
func InDir(dir, command string) error {
	shell, flag := sh.Command()
	binary, err := exec.LookPath(shell)
	if err != nil {
		return fmt.Errorf("could not find %s to run %q: %w", shell, command, err)
	}

	target, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("could not resolve %q: %w", dir, err)
	}
	if err := os.Chdir(target); err != nil {
		return fmt.Errorf("could not enter %q: %w", target, err)
	}
	if err := syscall.Exec(binary, []string{shell, flag, command}, environment(target)); err != nil {
		return fmt.Errorf("could not run %q: %w", command, err)
	}
	return nil
}

// environment returns the process environment with PWD set to dir. A shell that
// trusts an inherited PWD would otherwise report the directory TreeMan ran in.
func environment(dir string) []string {
	current := os.Environ()
	env := make([]string, 0, len(current)+1)
	for _, entry := range current {
		if strings.HasPrefix(entry, "PWD=") || strings.HasPrefix(entry, "OLDPWD=") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "PWD="+dir)
}
