package cmd

import (
	"fmt"
	"strings"

	"github.com/shoutcape/treeman/internal/launch"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
)

// execFlagName names the flag that hands a ready worktree to a command. An
// empty command keeps the default result: the worktree becomes the caller's
// shell destination.
const execFlagName = "exec"

// launchInWorktree performs the process handover. Tests replace it because a
// successful handover never returns.
var launchInWorktree = launch.InDir

// addLaunchFlag gives cmd the --exec flag and the check that rejects a flag
// naming no command. The check runs before RunE, so registering the flag is
// enough to fail a bad --exec before the command creates anything.
func addLaunchFlag(cmd *cobra.Command, command *string) {
	cmd.Flags().StringVarP(command, execFlagName, "x", "",
		"Run a command in the worktree instead of moving there")
	cmd.PreRunE = func(cmd *cobra.Command, _ []string) error {
		if cmd.Flags().Changed(execFlagName) && strings.TrimSpace(*command) == "" {
			return fmt.Errorf("--exec needs a command to run")
		}
		return nil
	}
}

// deliverWorktree hands the ready worktree to the caller.
//
// Without --exec it reports the path as the shell's destination. With --exec it
// replaces TreeMan with the command instead, so no destination is reported and
// the call does not return.
func deliverWorktree(cmd *cobra.Command, path, execCommand string) error {
	if execCommand == "" {
		return reportDestination(cmd, path)
	}

	render := commandRenderer(cmd)
	fmt.Fprintln(cmd.ErrOrStderr(), render.Status(ui.ToneInfo, "→", fmt.Sprintf("Running %s in %s", execCommand, path)))
	return launchInWorktree(path, execCommand)
}
