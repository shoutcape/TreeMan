package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
)

func newDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete [query]",
		Short: "Delete a worktree and its branch via fzf",
		Long: `Open an interactive fzf picker listing all deletable worktrees.

The main worktree and the default branch are protected from deletion.
An optional query pre-filters the list.

After confirmation, the selected worktree is removed and its branch is
deleted with git branch -D.`,
		Aliases: []string{"wtd"},
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			return runDelete(cmd, query)
		},
	}
}

func runDelete(cmd *cobra.Command, query string) error {
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

	// Confirm deletion.
	fmt.Fprintf(os.Stderr, "About to delete:\n")
	fmt.Fprintf(os.Stderr, "  Worktree: %s\n", dest)
	fmt.Fprintf(os.Stderr, "  Branch:   %s\n", branch)
	fmt.Fprintln(os.Stderr, "")

	if !confirmYN(cmd, "Are you sure? [y/N] ") {
		fmt.Fprintln(os.Stderr, "Cancelled.")
		return nil
	}

	return deleteWorktreeAndBranch(dest, branch, mainRoot)
}

// deleteWorktreeAndBranch removes the worktree and deletes its branch with
// guards against deleting the main worktree or the default branch.
//
// Mirrors _wt_delete_worktree_and_branch in wt.sh:461.
func deleteWorktreeAndBranch(dest, branch, mainRoot string) error {
	if dest == mainRoot {
		return fmt.Errorf("cannot delete the main worktree")
	}

	defaultBranch, _ := git.DetectDefaultBranch()
	if defaultBranch != "" && branch == defaultBranch {
		return fmt.Errorf("cannot delete the default branch %q", branch)
	}

	// If currently inside the target worktree, git worktree remove will refuse.
	// Detect this and print a clear message. The shell cannot cd here so we
	// just error out and let the user switch away first.
	cwd, _ := os.Getwd()
	if strings.HasPrefix(cwd, dest) {
		return fmt.Errorf("currently inside this worktree — run 'treeman switch' to leave it first")
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
