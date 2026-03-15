package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/shoutcape/TreeMan/internal/git"
	"github.com/shoutcape/TreeMan/internal/ui"
	"github.com/spf13/cobra"
)

var worktreeDeleteCmd = &cobra.Command{
	Use:   "delete [query]",
	Short: "Delete a worktree and its branch via fzf picker",
	Long: `Interactively select a worktree to delete using fzf.

The main worktree is protected and cannot be deleted.
A confirmation prompt is shown before deletion.
Prints the path to cd to after deletion (typically the main worktree).`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWorktreeDelete,
}

// deleteFlags holds flags specific to the delete command.
var deleteBranch string
var deletePath string

func init() {
	// Flags for lazygit integration (direct deletion without fzf)
	worktreeDeleteCmd.Flags().StringVar(&deleteBranch, "branch", "", "Delete worktree for this branch (skip fzf picker)")
	worktreeDeleteCmd.Flags().StringVar(&deletePath, "path", "", "Delete worktree at this path (skip fzf picker)")
}

func runWorktreeDelete(cmd *cobra.Command, args []string) error {
	if !git.IsInsideRepo() {
		return fmt.Errorf("not inside a git repository")
	}

	mainRoot, err := git.MainRoot()
	if err != nil {
		return fmt.Errorf("finding main worktree: %w", err)
	}

	// Direct deletion mode (for lazygit integration)
	if deleteBranch != "" {
		return deleteByBranch(deleteBranch, mainRoot)
	}
	if deletePath != "" {
		return deleteByPath(deletePath, mainRoot)
	}

	// Interactive fzf picker mode
	return deleteInteractive(args, mainRoot)
}

func deleteInteractive(args []string, mainRoot string) error {
	worktrees, err := git.WorktreeList()
	if err != nil {
		return err
	}

	if len(worktrees) <= 1 {
		fmt.Fprintln(os.Stderr, "Only one worktree exists — nothing to delete.")
		return nil
	}

	// Filter out main worktree
	var deletable []git.Worktree
	for _, wt := range worktrees {
		if wt.Path != mainRoot {
			deletable = append(deletable, wt)
		}
	}

	if len(deletable) == 0 {
		fmt.Fprintln(os.Stderr, "No deletable worktrees — only the main worktree exists.")
		return nil
	}

	displayLines := formatWorktreeDisplay(deletable, "")
	query := ""
	if len(args) > 0 {
		query = args[0]
	}

	selection, err := ui.Fzf(displayLines, ui.FzfOptions{
		BorderLabel: " delete worktree ",
		Prompt:      "delete > ",
		Query:       query,
		Ansi:        true,
	})
	if err != nil {
		return err
	}

	if selection == "" {
		return nil
	}

	idx := findSelectionIndex(displayLines, selection)
	if idx < 0 || idx >= len(deletable) {
		return fmt.Errorf("could not determine selected worktree")
	}

	dest := deletable[idx].Path
	branch := deletable[idx].Branch

	return confirmAndDelete(dest, branch, mainRoot)
}

func deleteByBranch(branch, mainRoot string) error {
	// Guard: cannot delete default branch
	defaultBranch, _ := git.DefaultBranch()
	if branch == defaultBranch {
		return fmt.Errorf("cannot delete the default branch '%s'", branch)
	}

	dest := git.FindWorktreeForBranch(branch)
	if dest == "" {
		return fmt.Errorf("no worktree found for branch '%s'", branch)
	}

	if dest == mainRoot {
		return fmt.Errorf("cannot delete the main worktree")
	}

	return doDelete(dest, branch, mainRoot)
}

func deleteByPath(path, mainRoot string) error {
	if path == mainRoot {
		return fmt.Errorf("cannot delete the main worktree")
	}

	// Find the branch for this path
	worktrees, err := git.WorktreeList()
	if err != nil {
		return err
	}

	var branch string
	for _, wt := range worktrees {
		if wt.Path == path {
			branch = wt.Branch
			break
		}
	}

	if branch == "" {
		return fmt.Errorf("no worktree found at '%s'", path)
	}

	return doDelete(path, branch, mainRoot)
}

func confirmAndDelete(dest, branch, mainRoot string) error {
	short := shortPath(dest)
	fmt.Fprintln(os.Stderr, "About to delete:")
	fmt.Fprintf(os.Stderr, "  Worktree: %s\n", dest)
	fmt.Fprintf(os.Stderr, "  Branch:   %s\n", branch)
	fmt.Fprintln(os.Stderr)
	fmt.Fprint(os.Stderr, "Are you sure? [y/N] ")

	reader := bufio.NewReader(os.Stdin)
	confirm, _ := reader.ReadString('\n')
	confirm = strings.TrimSpace(confirm)

	if confirm != "y" && confirm != "Y" {
		fmt.Fprintln(os.Stderr, "Cancelled.")
		return nil
	}

	_ = short // used for display only
	return doDelete(dest, branch, mainRoot)
}

func doDelete(dest, branch, mainRoot string) error {
	// Guard: cannot delete default branch
	defaultBranch, _ := git.DefaultBranch()
	if branch == defaultBranch {
		return fmt.Errorf("cannot delete the default branch '%s'", branch)
	}

	if dest == mainRoot {
		return fmt.Errorf("cannot delete the main worktree")
	}

	// Check if we're inside the worktree being deleted
	cwd, _ := os.Getwd()
	if strings.HasPrefix(cwd, dest) {
		fmt.Fprintln(os.Stderr, "Currently inside this worktree — switching to main worktree...")
	}

	fmt.Fprintln(os.Stderr, "Removing worktree...")
	if err := git.WorktreeRemove(dest); err != nil {
		return fmt.Errorf("failed to remove worktree '%s'. Use 'git worktree remove --force' to force it", dest)
	}

	fmt.Fprintf(os.Stderr, "Deleting branch '%s'...\n", branch)
	if err := git.BranchDelete(branch); err != nil {
		return fmt.Errorf("branch '%s' could not be deleted", branch)
	}

	fmt.Fprintln(os.Stderr, "Done — worktree and branch removed.")

	// Print main root to stdout so shell wrapper can cd there
	fmt.Println(mainRoot)
	return nil
}
