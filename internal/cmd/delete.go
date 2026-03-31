package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/database"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
)

func newDeleteCmd() *cobra.Command {
	var flagPath string
	var flagBranch string
	var flagYes bool

	cmd := &cobra.Command{
		Use:   "delete [query]",
		Short: "Delete a worktree and its branch via fzf",
		Long: `Open an interactive fzf picker listing all deletable worktrees.

The main worktree and the default branch are protected from deletion.
An optional query pre-filters the list.

After confirmation, the selected worktree is removed and its branch is
deleted with git branch -D.

Non-interactive mode (for lazygit / scripts):
  treeman delete --path <path> --branch <branch> --yes`,
		Aliases: []string{"wtd"},
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Non-interactive mode: --path + --branch provided directly.
			if flagPath != "" || flagBranch != "" {
				return runDeleteDirect(flagPath, flagBranch, flagYes)
			}

			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			return runDelete(cmd, query, flagYes)
		},
	}

	cmd.Flags().StringVar(&flagPath, "path", "", "Worktree path to delete (skips fzf picker)")
	cmd.Flags().StringVar(&flagBranch, "branch", "", "Branch to delete (skips fzf picker)")
	cmd.Flags().BoolVarP(&flagYes, "yes", "y", false, "Skip confirmation prompt")

	return cmd
}

// runDeleteDirect deletes a worktree by explicit path + branch, used by
// lazygit keybindings where fzf is not available and targets are known.
func runDeleteDirect(path, branch string, skipConfirm bool) error {
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
		fmt.Fprintf(os.Stderr, "About to delete:\n")
		fmt.Fprintf(os.Stderr, "  Worktree: %s\n", path)
		fmt.Fprintf(os.Stderr, "  Branch:   %s\n", branch)
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprint(os.Stderr, "Are you sure? [y/N] ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			if !strings.EqualFold(strings.TrimSpace(scanner.Text()), "y") {
				fmt.Fprintln(os.Stderr, "Cancelled.")
				return nil
			}
		}
	}

	// Load project config for database management.
	cfgResult := config.Load(mainRoot)
	if cfgResult.Warning != "" {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", cfgResult.Warning)
	}
	dbEnvKey := cfgResult.Config.DatabaseEnvKey()

	return deleteWorktreeAndBranch(path, branch, mainRoot, dbEnvKey)
}

func runDelete(cmd *cobra.Command, query string, skipConfirm bool) error {
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
	if len(entries) == 0 {
		return fmt.Errorf("no worktrees found")
	}
	if len(entries) == 1 {
		fmt.Fprintln(os.Stderr, "Only one worktree exists — nothing to delete.")
		return nil
	}

	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return err
	}

	// Build display rows, excluding the main worktree.
	var displayLines []string
	var fullPaths []string
	var branches []string

	for _, e := range entries {
		if e.Path == mainRoot {
			continue
		}
		displayLines = append(displayLines, ui.WorktreeRow(e.Path, e.Branch))
		fullPaths = append(fullPaths, e.Path)
		branches = append(branches, e.Branch)
	}

	if len(displayLines) == 0 {
		fmt.Fprintln(os.Stderr, "No deletable worktrees — only the main worktree exists.")
		return nil
	}

	display := strings.Join(displayLines, "\n")

	fzfArgs := []string{
		"--ansi",
		"--border-label", " delete worktree ",
		"--prompt=delete > ",
		"--select-1",
		"--exit-0",
	}
	if query != "" {
		fzfArgs = append(fzfArgs, "--query", query)
	}

	fzfCmd := exec.Command("fzf", fzfArgs...)
	fzfCmd.Stdin = strings.NewReader(display)
	fzfCmd.Stderr = os.Stderr

	out, err := fzfCmd.Output()
	if err != nil {
		// User cancelled.
		return nil
	}

	selection := strings.TrimSpace(string(out))
	if selection == "" {
		return nil
	}

	// Map selection back to path + branch.
	idx := matchIndex(displayLines, selection)
	if idx < 0 {
		return fmt.Errorf("could not map fzf selection to a worktree")
	}
	dest := fullPaths[idx]
	branch := branches[idx]

	// Confirm deletion (unless --yes was passed).
	fmt.Fprintf(os.Stderr, "About to delete:\n")
	fmt.Fprintf(os.Stderr, "  Worktree: %s\n", dest)
	fmt.Fprintf(os.Stderr, "  Branch:   %s\n", branch)
	fmt.Fprintln(os.Stderr, "")

	if !skipConfirm && !confirmYN(cmd, "Are you sure? [y/N] ") {
		fmt.Fprintln(os.Stderr, "Cancelled.")
		return nil
	}

	// Load project config for database management.
	cfgResult := config.Load(mainRoot)
	if cfgResult.Warning != "" {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", cfgResult.Warning)
	}
	dbEnvKey := cfgResult.Config.DatabaseEnvKey()

	return deleteWorktreeAndBranch(dest, branch, mainRoot, dbEnvKey)
}

// deleteWorktreeAndBranch removes the worktree and deletes its branch with
// guards against deleting the main worktree or the default branch.
//
// If dbEnvKey is non-empty, the branch-specific database is dropped first.
//
// Mirrors _wt_delete_worktree_and_branch in wt.sh:461.
func deleteWorktreeAndBranch(dest, branch, mainRoot, dbEnvKey string) error {
	if dest == mainRoot {
		return fmt.Errorf("cannot delete the main worktree")
	}

	defaultBranch, _ := git.DetectDefaultBranch()
	if defaultBranch != "" && branch == defaultBranch {
		return fmt.Errorf("cannot delete the default branch %q", branch)
	}

	// If currently inside the target worktree, git worktree remove will refuse.
	// Detect this and print a clear message.
	cwd, _ := os.Getwd()
	if strings.HasPrefix(cwd, dest) {
		return fmt.Errorf("currently inside this worktree -- run 'treeman switch' to leave it first")
	}

	// Drop branch-specific database (best-effort, non-fatal).
	if dbEnvKey != "" {
		if err := database.CleanupBranchDB(dest, dbEnvKey); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: database cleanup failed: %v\n", err)
		}
	}

	fmt.Fprintln(os.Stderr, "Removing worktree...")
	if err := git.WorktreeRemove(dest); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Deleting branch %q...\n", branch)
	if err := git.DeleteBranch(branch); err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "Done — worktree and branch removed.")
	return nil
}

// matchIndex returns the index in displayLines that matches the fzf selection,
// using ANSI-stripped comparison.
func matchIndex(displayLines []string, selection string) int {
	plainSelection := ui.StripANSI(strings.TrimSpace(selection))
	for i, line := range displayLines {
		if ui.StripANSI(line) == plainSelection {
			return i
		}
	}
	return -1
}

// confirmYN prints prompt and reads a y/Y response from stdin.
func confirmYN(cmd *cobra.Command, prompt string) bool {
	fmt.Fprint(os.Stderr, prompt)
	scanner := bufio.NewScanner(cmd.InOrStdin())
	if scanner.Scan() {
		answer := strings.TrimSpace(scanner.Text())
		return strings.EqualFold(answer, "y")
	}
	return false
}
