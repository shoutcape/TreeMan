package cmd

import (
	"fmt"
	"strings"

	"github.com/shoutcape/treeman/internal/launch"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
)

// execFlagName names the flag that hands a ready worktree to a command.
const execFlagName = "exec"

// worktreeLaunchOptions carries the command that replaces TreeMan once a
// worktree is ready. An empty command keeps the default result: the path on
// stdout.
type worktreeLaunchOptions struct {
	command string
}

// launchInWorktree performs the process handover. Tests replace it because a
// successful handover never returns.
var launchInWorktree = launch.InDir

func addLaunchFlag(cmd *cobra.Command, options *worktreeLaunchOptions) {
	cmd.Flags().StringVarP(&options.command, execFlagName, "x", "",
		"Run a command in the worktree instead of printing its path")
}

// validate rejects an --exec that names no command. Commands call it before
// they create anything, so a bad flag fails before the work.
func (options worktreeLaunchOptions) validate(cmd *cobra.Command) error {
	if cmd.Flags().Changed(execFlagName) && strings.TrimSpace(options.command) == "" {
		return fmt.Errorf("--exec needs a command to run")
	}
	return nil
}

// deliverWorktree hands the ready worktree to the caller.
//
// Without --exec it prints the path to stdout, which is what shell integration
// reads to change directory. With --exec it replaces TreeMan with the command
// instead, so no path is printed and the call does not return.
func deliverWorktree(cmd *cobra.Command, path string, options worktreeLaunchOptions) error {
	if options.command == "" {
		fmt.Fprintln(cmd.OutOrStdout(), path)
		return nil
	}

	render := commandRenderer(cmd)
	fmt.Fprintln(cmd.ErrOrStderr(), render.Status(ui.ToneInfo, "→", fmt.Sprintf("Running %s in %s", options.command, path)))
	return launchInWorktree(path, options.command)
}
