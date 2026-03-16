package cmd

import (
	"github.com/spf13/cobra"
)

// New returns the root cobra command for treeman.
func New(version, commit, date string) *cobra.Command {
	root := &cobra.Command{
		Use:   "treeman",
		Short: "Git worktree management CLI",
		Long: `TreeMan is a Git worktree management CLI.

It provides fast commands to create, switch, review, and delete
Git worktrees — keeping your branches isolated without juggling stashes.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newVersionCmd(version, commit, date))

	return root
}
