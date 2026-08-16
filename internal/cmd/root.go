package cmd

import (
	"github.com/spf13/cobra"
)

// New returns the root cobra command for treeman.
func New(version, commit, date string) *cobra.Command {
	var showVersion bool
	root := &cobra.Command{
		Use:   "treeman",
		Short: "Git worktree management CLI",
		Long: `TreeMan is a Git worktree management CLI.

It provides fast commands to create, switch, review, and delete
Git worktrees -- keeping your branches isolated without juggling stashes.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Run: func(cmd *cobra.Command, args []string) {
			if showVersion {
				printVersion(cmd, version, commit, date)
				return
			}
			printOverview(cmd)
		},
	}
	root.Flags().BoolVarP(&showVersion, "version", "v", false, "Print version information")

	root.AddCommand(newVersionCmd(version, commit, date))
	root.AddCommand(newCreateCmd())
	root.AddCommand(newBranchCmd())
	root.AddCommand(newReviewCmd())
	root.AddCommand(newSwitchCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newCleanCmd())
	root.AddCommand(newDeleteCmd())
	root.AddCommand(newDatabaseCmd())
	root.AddCommand(newInitCmd())

	return root
}

func printOverview(cmd *cobra.Command) {
	_, _ = cmd.OutOrStdout().Write([]byte(`TreeMan manages isolated Git worktrees.

  treeman create <branch>  Create a runnable worktree
  treeman branch [query]   Check out a remote branch
  treeman review [number]  Check out a PR or MR
  treeman switch [query]   Select a worktree
  treeman list [--json]    List worktrees and state
  treeman clean            Remove clean worktrees merged into default branch
  treeman delete [query]   Remove a worktree and branch
  treeman database         Inspect or repair branch databases

Run "treeman --help" for full command and flag reference.
`))
}
