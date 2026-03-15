package cli

import (
	"github.com/spf13/cobra"
)

// Version is set at build time via ldflags.
var Version = "dev"

var rootCmd = &cobra.Command{
	Use:   "treeman",
	Short: "TreeMan — Git worktree manager with per-branch runtime isolation",
	Long: `TreeMan manages Git worktrees and optional per-worktree runtimes.

Worktree commands:
  treeman worktree create <branch>    Create a new worktree + branch
  treeman worktree review [number]    Create a review worktree from a PR/MR
  treeman worktree switch [query]     Switch between worktrees (fzf picker)
  treeman worktree delete [query]     Delete a worktree and its branch
  treeman worktree list               List all worktrees

Runtime commands:
  treeman runtime up                  Start runtime for current worktree
  treeman runtime down                Stop runtime for current worktree
  treeman runtime status              Show runtime status
  treeman runtime logs                Tail runtime logs
  treeman runtime env                 Print assigned ports/env
  treeman runtime ls                  List all known runtimes

Shell aliases (wt, wts, wtd, wtpr, wtmr) are provided via wt.sh for
commands that need to change the shell's working directory.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(worktreeCmd)
	rootCmd.AddCommand(runtimeCmd)
	rootCmd.AddCommand(versionCmd)
}
