package cmd

import (
	"bufio"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/database"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
)

var (
	removeWorktree = git.WorktreeRemove
	deleteBranch   = git.DeleteBranch
)

func newDeleteCmd() *cobra.Command {
	var flagPath string
	var flagBranch string
	var flagYes bool
	var flagForce bool

	cmd := &cobra.Command{
		Use:     "delete [query]",
		Short:   "Delete a worktree and its branch via fzf",
		Aliases: []string{"wtd"},
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagPath != "" || flagBranch != "" {
				return runDeleteDirect(cmd, flagPath, flagBranch, flagYes, flagForce)
			}
			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			return runDelete(cmd, query, flagYes, flagForce)
		},
	}

	cmd.Flags().StringVar(&flagPath, "path", "", "Worktree path to delete (skips fzf picker)")
	cmd.Flags().StringVar(&flagBranch, "branch", "", "Branch to delete (skips fzf picker)")
	cmd.Flags().BoolVarP(&flagYes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVarP(&flagForce, "force", "f", false, "Delete a dirty worktree or unmerged branch")
	return cmd
}

func runDeleteDirect(cmd *cobra.Command, path, branch string, skipConfirm, force bool) error {
	if path == "" || branch == "" {
		return fmt.Errorf("--path and --branch are both required in non-interactive mode")
	}
	if !git.IsInsideRepo() {
		return fmt.Errorf("not inside a git repository")
	}
	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return err
	}
	if !skipConfirm {
		printDeleteConfirmation(cmd, path, branch)
		confirmed, err := confirmYN(cmd, "Are you sure? [y/N] ")
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneMuted, "○", "Cancelled."))
			return nil
		}
	}
	return deleteWorktree(cmd, path, branch, mainRoot, force, false)
}

func runDelete(cmd *cobra.Command, query string, skipConfirm, force bool) error {
	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	if !git.IsInsideRepo() {
		return fmt.Errorf("not inside a git repository")
	}
	entries, err := git.WorktreeList()
	if err != nil {
		return err
	}
	if len(entries) <= 1 {
		fmt.Fprintln(out, "Only one worktree exists -- nothing to delete.")
		return nil
	}
	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return err
	}

	var displayLines, paths, branches []string
	for _, entry := range entries {
		if samePath(entry.Path, mainRoot) {
			continue
		}
		displayLines = append(displayLines, pickerRow(render.WorktreeRow(entry.Path, entry.Branch), len(paths)))
		paths = append(paths, entry.Path)
		branches = append(branches, entry.Branch)
	}
	if len(displayLines) == 0 {
		fmt.Fprintln(out, "No deletable worktrees -- only the main worktree exists.")
		return nil
	}
	if !canInteract(cmd) {
		return fmt.Errorf("interactive selection is unavailable; pass --path and --branch")
	}
	if _, err := exec.LookPath("fzf"); err != nil {
		return fmt.Errorf("fzf is required for delete. Install it from https://github.com/junegunn/fzf")
	}

	args := pickerArgs(sessionFor(cmd).errorOutput.Color, " delete worktree ", "delete > ")
	if query != "" {
		args = append(args, "--query", query)
	}
	fzfCmd := exec.Command("fzf", args...)
	fzfCmd.Stdin = strings.NewReader(strings.Join(displayLines, "\n"))
	fzfCmd.Stderr = out
	selectionOutput, err := fzfCmd.Output()
	if err != nil {
		if pickerCancelled(err) {
			fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "Cancelled."))
			return nil
		}
		return fmt.Errorf("fzf failed while selecting a worktree: %w", err)
	}
	selection := strings.TrimSpace(string(selectionOutput))
	if selection == "" {
		fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "Cancelled."))
		return nil
	}
	idx := pickerSelectionIndex(selection, len(paths))
	if idx < 0 {
		return fmt.Errorf("could not map fzf selection to a worktree")
	}
	if !skipConfirm {
		printDeleteConfirmation(cmd, paths[idx], branches[idx])
		confirmed, err := confirmYN(cmd, "Are you sure? [y/N] ")
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "Cancelled."))
			return nil
		}
	}
	return deleteWorktree(cmd, paths[idx], branches[idx], mainRoot, force, false)
}

func deleteWorktree(cmd *cobra.Command, dest, branch, mainRoot string, force, skipMergeCheck bool) error {
	return deleteWorktreeAtSHA(cmd, dest, branch, mainRoot, force, skipMergeCheck, "")
}

// deleteWorktreeAtSHA removes a worktree and its branch. An expected SHA makes
// cleanup conditional on the exact commit whose merge was verified.
func deleteWorktreeAtSHA(cmd *cobra.Command, dest, branch, mainRoot string, force, skipMergeCheck bool, expectedSHA string) error {
	entry, err := findWorktree(dest)
	if err != nil {
		return err
	}
	if samePath(entry.Path, mainRoot) {
		return fmt.Errorf("cannot delete the main worktree")
	}
	if entry.Branch != branch {
		return fmt.Errorf("worktree %q is checked out on branch %q, not %q", entry.Path, entry.Branch, branch)
	}
	defaultBranch, err := git.DetectDefaultBranch()
	if err != nil {
		return fmt.Errorf("cannot delete branch %q because the default branch could not be detected: %w", branch, err)
	}
	if branch == defaultBranch {
		return fmt.Errorf("cannot delete the default branch %q", branch)
	}
	dirty, err := git.WorktreeDirty(entry.Path)
	if err != nil {
		return err
	}
	if dirty && !force {
		return fmt.Errorf("worktree %q has uncommitted or untracked changes; use --force to delete it", entry.Path)
	}
	if !force && !skipMergeCheck {
		canDelete, err := git.BranchCanDelete(mainRoot, branch)
		if err != nil {
			return err
		}
		if !canDelete {
			return fmt.Errorf("branch %q is not fully merged; use --force to delete it", branch)
		}
	}
	if expectedSHA != "" {
		actualSHA, err := git.BranchSHA(branch)
		if err != nil {
			return fmt.Errorf("cannot remove worktree %q because branch %q could not be resolved for verified cleanup: %w", entry.Path, branch, err)
		}
		if actualSHA != expectedSHA {
			return fmt.Errorf("cannot remove worktree %q: branch %q moved after merge verification (expected %s, found %s)", entry.Path, branch, expectedSHA, actualSHA)
		}
	}
	currentRoot, err := git.CurrentWorktreeRoot()
	if err != nil {
		return err
	}

	// All destructive checks have passed. Database cleanup remains best-effort.
	cfgResult := config.Load(mainRoot)
	if dbEnvKey := cfgResult.Config.DatabaseEnvKey(); dbEnvKey != "" {
		if err := database.CleanupBranchDB(entry.Path, dbEnvKey); err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneWarning, "!", fmt.Sprintf("database cleanup failed: %v", err)))
		}
	}
	if err := removeWorktree(entry.Path, force); err != nil {
		return deleteWorktreeFailure(err, "none", fmt.Sprintf("worktree %q, branch %q", entry.Path, branch), fmt.Sprintf("resolve the error, then retry: treeman delete --path %q --branch %q --yes%s", entry.Path, branch, forceFlag(force)))
	}
	if expectedSHA != "" {
		if err := git.DeleteBranchAtSHA(mainRoot, branch, expectedSHA); err != nil {
			return deleteWorktreeFailure(
				fmt.Errorf("branch %q was preserved because it moved after merge verification: %w", branch, err),
				fmt.Sprintf("removed worktree %q", entry.Path),
				fmt.Sprintf("branch %q", branch),
				fmt.Sprintf("inspect branch %q, then delete it manually if appropriate: git -C %q branch -D %q", branch, mainRoot, branch),
			)
		}
	} else {
		branchForce := force || skipMergeCheck
		if err := deleteBranch(mainRoot, branch, branchForce); err != nil {
			return deleteWorktreeFailure(err, fmt.Sprintf("removed worktree %q", entry.Path), fmt.Sprintf("branch %q", branch), fmt.Sprintf("git -C %q branch %s %q", mainRoot, deleteBranchFlag(branchForce), branch))
		}
	}
	fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneSuccess, "✓", "Deleted worktree and branch: "+branch))
	if samePath(currentRoot, entry.Path) {
		fmt.Fprintln(cmd.OutOrStdout(), mainRoot)
	}
	return nil
}

func deleteWorktreeFailure(err error, completed, remaining, recovery string) error {
	return fmt.Errorf("%w\nCompleted: %s.\nRemaining: %s.\nRecovery: %s", err, completed, remaining, recovery)
}

func forceFlag(force bool) string {
	if force {
		return " --force"
	}
	return ""
}

func deleteBranchFlag(force bool) string {
	if force {
		return "-D"
	}
	return "-d"
}

func findWorktree(path string) (git.WorktreeEntry, error) {
	entries, err := git.WorktreeList()
	if err != nil {
		return git.WorktreeEntry{}, err
	}
	for _, entry := range entries {
		if samePath(entry.Path, path) {
			return entry, nil
		}
	}
	return git.WorktreeEntry{}, fmt.Errorf("path %q is not a linked worktree", path)
}

func samePath(a, b string) bool {
	return canonicalPath(a) == canonicalPath(b)
}

func canonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(abs)
}

func printDeleteConfirmation(cmd *cobra.Command, path, branch string) {
	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", "About to delete:"))
	fmt.Fprintf(out, "  Worktree: %s\n", render.Path(path))
	fmt.Fprintf(out, "  Branch:   %s\n\n", render.Branch(branch))
}

func confirmYN(cmd *cobra.Command, prompt string) (bool, error) {
	if !canInteract(cmd) {
		return false, fmt.Errorf("confirmation required; rerun with --yes")
	}
	fmt.Fprint(cmd.ErrOrStderr(), prompt)
	scanner := bufio.NewScanner(cmd.InOrStdin())
	if scanner.Scan() {
		return strings.EqualFold(strings.TrimSpace(scanner.Text()), "y"), nil
	}
	return false, nil
}
