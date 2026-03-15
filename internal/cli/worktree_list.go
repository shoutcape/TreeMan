package cli

import (
	"fmt"
	"os"

	"github.com/shoutcape/TreeMan/internal/git"
	"github.com/spf13/cobra"
)

var worktreeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all worktrees",
	Long:  "List all Git worktrees for the current repository.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !git.IsInsideRepo() {
			return fmt.Errorf("not inside a git repository")
		}

		worktrees, err := git.WorktreeList()
		if err != nil {
			return err
		}

		if len(worktrees) == 0 {
			fmt.Fprintln(os.Stderr, "No worktrees found.")
			return nil
		}

		for _, wt := range worktrees {
			branch := wt.Branch
			if branch == "" {
				branch = "(detached)"
			}
			fmt.Fprintf(os.Stderr, "%-60s %s\n", wt.Path, branch)
		}
		return nil
	},
}

var worktreeMainCmd = &cobra.Command{
	Use:   "main",
	Short: "Print the main worktree path",
	Long:  "Print the path to the main (primary) worktree.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !git.IsInsideRepo() {
			return fmt.Errorf("not inside a git repository")
		}

		mainRoot, err := git.MainRoot()
		if err != nil {
			return err
		}

		fmt.Println(mainRoot)
		return nil
	},
}
