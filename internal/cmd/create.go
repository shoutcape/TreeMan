package cmd

import (
	"fmt"
	"os"

	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/shoutcape/treeman/internal/validate"
	"github.com/shoutcape/treeman/internal/worktree"
	"github.com/spf13/cobra"
)

const databaseDocsURL = "https://github.com/shoutcape/TreeMan/blob/main/docs/integrations/postgresql.md"

func newCreateCmd() *cobra.Command {
	var setupOptions creationSetupOptions
	var launchOptions worktreeLaunchOptions
	cmd := &cobra.Command{
		Use:   "create <branch-name>",
		Short: "Create a new worktree + branch",
		Long: `Create a new linked worktree and branch from the latest default branch.

The worktree is placed under .worktrees/<branch-slug> inside the repository.
If a different branch already has a worktree at that path, the path gets a
short suffix that is derived from the branch name.

.env* files are automatically copied from the main worktree, and
dependencies are installed if a known lockfile is detected.

The path of the new worktree is printed to stdout so that a shell wrapper
can cd into it. With --exec, TreeMan runs the given command in the new
worktree instead of printing the path.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := launchOptions.validate(cmd); err != nil {
				return err
			}
			return runCreate(cmd, args[0], setupOptions, launchOptions)
		},
	}
	addCreationSetupFlags(cmd, &setupOptions)
	addLaunchFlag(cmd, &launchOptions)

	return cmd
}

func runCreate(cmd *cobra.Command, branch string, setupOptions creationSetupOptions, launchOptions worktreeLaunchOptions) error {
	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	// Validate branch name.
	if err := validate.BranchName(branch); err != nil {
		return err
	}

	// Must be inside a git repo.
	if !git.IsInsideRepo() {
		return fmt.Errorf("not inside a git repository")
	}

	// Main worktree root.
	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return err
	}

	// Resolve default branch.
	defaultBranch, err := git.DetectDefaultBranch()
	if err != nil {
		return err
	}

	// Guard: branch must not already exist.
	if git.BranchExists(branch) {
		return fmt.Errorf("branch %q already exists locally", branch)
	}

	// Fetch latest default branch.
	fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", fmt.Sprintf("Fetching latest %s from origin...", defaultBranch)))
	if err := git.Fetch(defaultBranch); err != nil {
		return err
	}

	// Build worktree path. Existing worktrees keep their path; a branch whose
	// slug collides with another branch's worktree gets a suffixed path.
	existing, err := git.WorktreeList()
	if err != nil {
		return err
	}
	worktreePath, err := worktree.ResolvePathForBranch(mainRoot, branch, existing)
	if err != nil {
		return err
	}

	// Guard: directory must not already exist.
	if _, err := os.Stat(worktreePath); err == nil {
		return fmt.Errorf("directory %q already exists", worktreePath)
	}

	// Create worktree + branch.
	fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", fmt.Sprintf("Creating worktree at %s (branch: %s)...", worktreePath, branch)))
	created, err := git.CreateWorktree(worktreePath, branch, "origin/"+defaultBranch)
	if err != nil {
		return err
	}

	summary := setupCreatedWorktree(out, render, mainRoot, created, setupOptions)

	// Print result to stderr for the user.
	fmt.Fprintln(out, "")
	printSetupSummary(out, render, summary)
	fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Worktree ready:"))
	fmt.Fprintf(out, "  Branch: %s\n", render.Branch(render.Fit(branch, 10)))
	fmt.Fprintf(out, "  Path:   %s\n", render.Path(render.Fit(worktreePath, 10)))

	// Print path to stdout so the shell wrapper can cd into it, or hand the
	// worktree to --exec.
	return deliverWorktree(cmd, worktreePath, launchOptions)
}
