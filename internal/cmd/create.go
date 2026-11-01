package cmd

import (
	"fmt"

	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/shoutcape/treeman/internal/validate"
	"github.com/spf13/cobra"
)

const databaseDocsURL = "https://github.com/shoutcape/TreeMan/blob/main/docs/integrations/postgresql.md"

func newCreateCmd() *cobra.Command {
	var setupOptions creationSetupOptions
	var execCommand string
	cmd := &cobra.Command{
		Use:   "create <branch-name>",
		Short: "Create a new worktree + branch",
		Long: `Create a new linked worktree and branch from the latest default branch.

The worktree is placed under .worktrees/<branch-slug> inside the repository,
or under the worktree_dir configured in .treeman.toml. If a different branch
already has a worktree at that path, the path gets a short suffix that is
derived from the branch name.

.env* files are automatically copied from the main worktree, and
dependencies are installed if a known lockfile is detected.

Shell integration changes directory to the new worktree; without it, the
path is printed to stdout. With --exec, TreeMan runs the given command in
the new worktree instead.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreate(cmd, args[0], setupOptions, execCommand)
		},
	}
	addCreationSetupFlags(cmd, &setupOptions)
	addLaunchFlag(cmd, &execCommand)

	return cmd
}

func runCreate(cmd *cobra.Command, branch string, setupOptions creationSetupOptions, execCommand string) error {
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

	provisioned, err := provisionWorktree(cmd, provisionRequest{
		mainRoot: mainRoot,
		source:   newBranchFromDefault(branch, fetchStrict),
		options:  setupOptions,
	})
	if err != nil {
		return err
	}
	worktreePath := provisioned.worktree.Path

	fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Worktree ready:"))
	fmt.Fprintf(out, "  Branch: %s\n", render.Branch(render.Fit(branch, 10)))
	fmt.Fprintf(out, "  Path:   %s\n", render.Path(render.Fit(worktreePath, 10)))

	// Report the path so the shell wrapper can cd into it, or hand the
	// worktree to --exec.
	return deliverWorktree(cmd, worktreePath, execCommand)
}
