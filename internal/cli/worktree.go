package cli

import (
	"github.com/spf13/cobra"
)

var worktreeCmd = &cobra.Command{
	Use:     "worktree",
	Aliases: []string{"wt"},
	Short:   "Manage Git worktrees",
	Long:    "Create, switch, delete, and list Git worktrees.",
}

func init() {
	worktreeCmd.AddCommand(worktreeCreateCmd)
	worktreeCmd.AddCommand(worktreeReviewCmd)
	worktreeCmd.AddCommand(worktreeSwitchCmd)
	worktreeCmd.AddCommand(worktreeDeleteCmd)
	worktreeCmd.AddCommand(worktreeListCmd)
	worktreeCmd.AddCommand(worktreeMainCmd)
}
