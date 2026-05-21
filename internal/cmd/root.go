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
Git worktrees -- keeping your branches isolated without juggling stashes.`,
		SilenceUsage:  true,
		SilenceErrors: true,

		// PersistentPreRunE runs before every subcommand. It reports any
		// errors from background deletions that were spawned by earlier
		// 'treeman delete' runs.
		//
		// NOTE: Cobra does NOT chain PersistentPreRunE hooks. If a subcommand
		// defines its own PersistentPreRunE, it will replace this one.
		// In that case the subcommand must call reportDeleteErrors() explicitly.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			reportDeleteErrors()
			return nil
		},
	}

	root.AddCommand(newVersionCmd(version, commit, date))
	root.AddCommand(newCreateCmd())
	root.AddCommand(newBranchCmd())
	root.AddCommand(newReviewCmd())
	root.AddCommand(newSwitchCmd())
	root.AddCommand(newDeleteCmd())
	root.AddCommand(newOpenCmd())
	root.AddCommand(newInitCmd())

	return root
}
