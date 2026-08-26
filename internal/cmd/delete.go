package cmd

import (
	"bufio"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shoutcape/treeman/internal/database"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
)

var (
	removeWorktree    = git.WorktreeRemove
	deleteBranchAtSHA = git.DeleteBranchAtSHA
	newCleanupBatch   = func() databaseCleanupBatch { return database.NewCleanupBatch() }
)

type databaseCleanupBatch interface {
	Prepare(string) (func() error, error)
	Flush() error
}

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
	return deleteWorktree(cmd, path, branch, mainRoot, force)
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
	return deleteWorktree(cmd, paths[idx], branches[idx], mainRoot, force)
}

func deleteWorktree(cmd *cobra.Command, dest, branch, mainRoot string, force bool) error {
	return deleteWorktreeAtSHA(cmd, dest, branch, mainRoot, force, false, "")
}

// deleteVerifiedWorktree preserves the exact-SHA cleanup helper used by the
// merge classifier while routing it through database-aware deletion.
func deleteVerifiedWorktree(cmd *cobra.Command, dest, branch, mainRoot string, force bool, expectedSHA string) error {
	if expectedSHA == "" {
		return fmt.Errorf("cannot skip merge check for branch %q without an expected SHA", branch)
	}
	return deleteWorktreeAtSHA(cmd, dest, branch, mainRoot, force, true, expectedSHA)
}

// deleteWorktreeAtSHA removes a worktree and its branch. An expected SHA makes
// cleanup conditional on the exact commit whose merge was verified. Every
// deletion snapshots its branch SHA and compare-and-deletes that exact ref.
func deleteWorktreeAtSHA(cmd *cobra.Command, dest, branch, mainRoot string, force, skipMergeCheck bool, expectedSHA string) error {
	batch := newCleanupBatch()
	if err := deleteWorktreeAtSHAWithDatabase(cmd, dest, branch, mainRoot, force, skipMergeCheck, expectedSHA, batch); err != nil {
		return err
	}
	if err := batch.Flush(); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneWarning, "!", fmt.Sprintf("database cleanup failed: %v", err)))
	}
	return nil
}

// deleteWorktreeAtSHAWithDatabase lets clean share one cleanup batch across
// worktrees while keeping ownership transitions inside the database package.
func deleteWorktreeAtSHAWithDatabase(cmd *cobra.Command, dest, branch, mainRoot string, force, skipMergeCheck bool, expectedSHA string, batch databaseCleanupBatch) error {
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
	defaultBranch, err := detectDefaultBranchWithWarning(cmd)
	if err != nil {
		return fmt.Errorf("cannot delete branch %q because the default branch could not be detected: %w", branch, err)
	}
	if branch == defaultBranch {
		return fmt.Errorf("cannot delete the default branch %q", branch)
	}
	branchSHA, err := git.BranchSHA(branch)
	if err != nil {
		return fmt.Errorf("cannot remove worktree %q because branch %q could not be resolved: %w", entry.Path, branch, err)
	}
	if expectedSHA != "" && branchSHA != expectedSHA {
		return fmt.Errorf("cannot remove worktree %q: branch %q moved after merge verification (expected %s, found %s)", entry.Path, branch, expectedSHA, branchSHA)
	}
	dirty, err := git.WorktreeDirty(entry.Path)
	if err != nil {
		return err
	}
	if dirty && !force {
		return fmt.Errorf("worktree %q has uncommitted or untracked changes; use --force to delete it", entry.Path)
	}
	if !force && !skipMergeCheck {
		canDelete, err := git.BranchCanDeleteAtSHA(mainRoot, branch, branchSHA)
		if err != nil {
			return err
		}
		if !canDelete {
			return fmt.Errorf("branch %q is not fully merged; use --force to delete it", branch)
		}
	}
	currentRoot, err := git.CurrentWorktreeRoot()
	if err != nil {
		return err
	}

	// Snapshot the database target while the worktree environment still exists.
	// Cleanup itself runs only after Git deletion succeeds.
	var commitDatabaseCleanup func() error
	if batch == nil {
		batch = newCleanupBatch()
	}
	if commitDatabaseCleanup, err = batch.Prepare(entry.Path); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneWarning, "!", fmt.Sprintf("database cleanup failed: %v", err)))
	}
	if err := removeWorktree(entry.Path, force); err != nil {
		return deleteWorktreeFailure(err, "none", fmt.Sprintf("worktree %q, branch %q", entry.Path, branch), fmt.Sprintf("resolve the error, then retry: treeman delete --path %q --branch %q --yes%s", entry.Path, branch, forceFlag(force)))
	}
	if err := deleteBranchAtSHA(mainRoot, branch, branchSHA); err != nil {
		return deleteWorktreeFailure(
			fmt.Errorf("branch %q was preserved after deletion checks: %w", branch, err),
			fmt.Sprintf("removed worktree %q", entry.Path),
			fmt.Sprintf("branch %q", branch),
			fmt.Sprintf("inspect branch %q, then delete it manually if appropriate: git -C %q branch -D %q", branch, mainRoot, branch),
		)
	}
	if commitDatabaseCleanup != nil {
		if err := commitDatabaseCleanup(); err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneWarning, "!", fmt.Sprintf("database cleanup could not be recorded for retry: %v", err)))
		}
	}
	fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneSuccess, "✓", "Deleted worktree and branch: "+branch))
	if samePath(currentRoot, entry.Path) {
		fmt.Fprintln(cmd.OutOrStdout(), mainRoot)
	}
	return nil
}

func detectDefaultBranchWithWarning(cmd *cobra.Command) (string, error) {
	branch, slowPath, err := git.DetectDefaultBranchVerbose()
	if err != nil {
		return "", err
	}
	if slowPath {
		fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneWarning, "!",
			"origin/HEAD is not set locally -- used a network call to detect the default branch. Run: git remote set-head origin --auto"))
	}
	return branch, nil
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
