package cli

import (
	"fmt"
	"os"

	"github.com/shoutcape/TreeMan/internal/git"
	"github.com/spf13/cobra"
)

var worktreeCreateCmd = &cobra.Command{
	Use:   "create <branch>",
	Short: "Create a new worktree and branch",
	Long: `Create a new Git worktree with a new branch based on the latest default branch.

The worktree is placed as a sibling directory of the main worktree:
  <parent>/<repo>.<branch-slug>

Branch name slashes are converted to dashes in the directory name.
Dependencies are auto-installed based on lockfile detection.
Environment files (.env*) are copied from the main worktree.

Prints the new worktree path to stdout on success.`,
	Args: cobra.ExactArgs(1),
	RunE: runWorktreeCreate,
}

func runWorktreeCreate(cmd *cobra.Command, args []string) error {
	branch := args[0]

	if err := git.ValidateBranchName(branch); err != nil {
		return err
	}

	if !git.IsInsideRepo() {
		return fmt.Errorf("not inside a git repository")
	}

	mainRoot, err := git.MainRoot()
	if err != nil {
		return fmt.Errorf("finding main worktree: %w", err)
	}

	defaultBranch, err := git.DefaultBranch()
	if err != nil {
		return err
	}

	if git.LocalBranchExists(branch) {
		return fmt.Errorf("branch '%s' already exists locally", branch)
	}

	worktreePath := git.WorktreePathForBranch(mainRoot, branch)

	if _, err := os.Stat(worktreePath); err == nil {
		return fmt.Errorf("directory '%s' already exists", worktreePath)
	}

	fmt.Fprintf(os.Stderr, "Fetching latest %s from origin...\n", defaultBranch)
	if err := git.Fetch("origin", defaultBranch); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Creating worktree at %s (branch: %s)...\n", worktreePath, branch)
	if err := git.WorktreeAdd(worktreePath, branch, "origin/"+defaultBranch); err != nil {
		return err
	}

	git.CopyEnvFiles(mainRoot, worktreePath)

	fmt.Fprintf(os.Stderr, "Detecting dependencies...\n")
	if err := git.InstallDeps(worktreePath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: dependency installation failed: %v\n", err)
	}

	fmt.Fprintf(os.Stderr, "\nWorktree ready:\n")
	fmt.Fprintf(os.Stderr, "  Path: %s\n", worktreePath)

	// Print path to stdout for shell wrapper to cd into
	fmt.Println(worktreePath)
	return nil
}
