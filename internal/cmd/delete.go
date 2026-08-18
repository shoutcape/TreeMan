package cmd

import (
	"bufio"
	"fmt"
	"os"
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
		printDeleteConfirmation(path, branch)
		if !confirmYN(cmd, "Are you sure? [y/N] ") {
			fmt.Fprintln(os.Stderr, "Cancelled.")
			return nil
		}
	}
	return deleteWorktree(cmd, path, branch, mainRoot, force)
}

func runDelete(cmd *cobra.Command, query string, skipConfirm, force bool) error {
	if _, err := exec.LookPath("fzf"); err != nil {
		return fmt.Errorf("fzf is required for delete. Install it from https://github.com/junegunn/fzf")
	}
	if !git.IsInsideRepo() {
		return fmt.Errorf("not inside a git repository")
	}
	entries, err := git.WorktreeList()
	if err != nil {
		return err
	}
	if len(entries) <= 1 {
		fmt.Fprintln(os.Stderr, "Only one worktree exists -- nothing to delete.")
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
		displayLines = append(displayLines, pickerRow(ui.WorktreeRow(entry.Path, entry.Branch), len(paths)))
		paths = append(paths, entry.Path)
		branches = append(branches, entry.Branch)
	}
	if len(displayLines) == 0 {
		fmt.Fprintln(os.Stderr, "No deletable worktrees -- only the main worktree exists.")
		return nil
	}

	args := pickerArgs(" delete worktree ", "delete > ")
	if query != "" {
		args = append(args, "--query", query)
	}
	fzfCmd := exec.Command("fzf", args...)
	fzfCmd.Stdin = strings.NewReader(strings.Join(displayLines, "\n"))
	fzfCmd.Stderr = os.Stderr
	out, err := fzfCmd.Output()
	if err != nil {
		if pickerCancelled(err) {
			fmt.Fprintln(os.Stderr, "Cancelled.")
			return nil
		}
		return fmt.Errorf("fzf failed while selecting a worktree: %w", err)
	}
	selection := strings.TrimSpace(string(out))
	if selection == "" {
		fmt.Fprintln(os.Stderr, "Cancelled.")
		return nil
	}
	idx := pickerSelectionIndex(selection, len(paths))
	if idx < 0 {
		return fmt.Errorf("could not map fzf selection to a worktree")
	}
	if !skipConfirm {
		printDeleteConfirmation(paths[idx], branches[idx])
		if !confirmYN(cmd, "Are you sure? [y/N] ") {
			fmt.Fprintln(os.Stderr, "Cancelled.")
			return nil
		}
	}
	return deleteWorktree(cmd, paths[idx], branches[idx], mainRoot, force)
}

func deleteWorktree(cmd *cobra.Command, dest, branch, mainRoot string, force bool) error {
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
	if !force {
		canDelete, err := git.BranchCanDelete(mainRoot, branch)
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

	// All destructive checks have passed. Database cleanup remains best-effort.
	cfgResult := config.Load(mainRoot)
	if dbEnvKey := cfgResult.Config.DatabaseEnvKey(); dbEnvKey != "" {
		if err := database.CleanupBranchDB(entry.Path, dbEnvKey); err != nil {
			fmt.Fprintln(os.Stderr, ui.RenderStatus(ui.ToneWarning, "!", fmt.Sprintf("database cleanup failed: %v", err)))
		}
	}
	if err := removeWorktree(entry.Path, force); err != nil {
		return deleteWorktreeFailure(err, "none", fmt.Sprintf("worktree %q, branch %q", entry.Path, branch), fmt.Sprintf("resolve the error, then retry: treeman delete --path %q --branch %q --yes%s", entry.Path, branch, forceFlag(force)))
	}
	if err := deleteBranch(mainRoot, branch, force); err != nil {
		return deleteWorktreeFailure(err, fmt.Sprintf("removed worktree %q", entry.Path), fmt.Sprintf("branch %q", branch), fmt.Sprintf("git -C %q branch %s %q", mainRoot, deleteBranchFlag(force), branch))
	}
	fmt.Fprintln(os.Stderr, ui.RenderStatus(ui.ToneSuccess, "✓", "Deleted worktree and branch: "+branch))
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

func printDeleteConfirmation(path, branch string) {
	fmt.Fprintln(os.Stderr, ui.RenderStatus(ui.ToneWarning, "!", "About to delete:"))
	fmt.Fprintf(os.Stderr, "  Worktree: %s\n", ui.RenderPath(path))
	fmt.Fprintf(os.Stderr, "  Branch:   %s\n\n", ui.RenderBranch(branch))
}

func confirmYN(cmd *cobra.Command, prompt string) bool {
	fmt.Fprint(os.Stderr, prompt)
	scanner := bufio.NewScanner(cmd.InOrStdin())
	if scanner.Scan() {
		return strings.EqualFold(strings.TrimSpace(scanner.Text()), "y")
	}
	return false
}
