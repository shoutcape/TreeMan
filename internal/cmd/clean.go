package cmd

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/spf13/cobra"
)

var (
	cleanTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#6FB8D2"))
	cleanDescriptionStyle = lipgloss.NewStyle().
				Faint(true)
	cleanHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Faint(true)
	cleanBranchStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#B2B644")).
				Width(40)
	cleanPathStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#C4915E"))
)

func newCleanCmd() *cobra.Command {
	var dryRun bool
	var skipConfirm bool
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove clean worktrees with branches merged into the default branch",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClean(cmd, dryRun, skipConfirm)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show worktrees that would be removed")
	cmd.Flags().BoolVarP(&skipConfirm, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func runClean(cmd *cobra.Command, dryRun, skipConfirm bool) error {
	if !git.IsInsideRepo() {
		return fmt.Errorf("not inside a git repository")
	}
	defaultBranch, err := git.DetectDefaultBranch()
	if err != nil {
		return err
	}
	if err := git.Fetch("refs/heads/" + defaultBranch + ":refs/remotes/origin/" + defaultBranch); err != nil {
		return fmt.Errorf("could not fetch origin/%s: %w", defaultBranch, err)
	}
	entries, err := git.WorktreeList()
	if err != nil {
		return err
	}
	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return err
	}
	mergedBranches, err := git.MergedBranches("origin/" + defaultBranch)
	if err != nil {
		return err
	}

	// Collect non-default branch names to check remote existence in one call.
	var nonDefaultBranches []string
	for _, entry := range entries {
		if entry.Branch != "" && entry.Branch != defaultBranch && !samePath(entry.Path, mainRoot) {
			nonDefaultBranches = append(nonDefaultBranches, entry.Branch)
		}
	}
	remoteBranchExists, err := git.RemoteBranchesExist(nonDefaultBranches)
	if err != nil {
		// Non-fatal: fall back to ancestry-only detection.
		remoteBranchExists = map[string]bool{}
	}

	var candidates []git.WorktreeEntry
	for _, entry := range entries {
		// A branch qualifies for cleanup if it is merged (direct ancestor) OR
		// its remote is gone (squash-merge or abandoned branch deleted on origin).
		isMerged := mergedBranches[entry.Branch] || !remoteBranchExists[entry.Branch]
		if entry.Branch == "" || entry.Branch == defaultBranch || samePath(entry.Path, mainRoot) || !isMerged {
			continue
		}
		dirty, err := git.WorktreeDirty(entry.Path)
		if err != nil {
			return err
		}
		if dirty {
			continue
		}
		candidates = append(candidates, entry)
	}

	// Remove the current worktree last so its process working directory remains valid.
	currentRoot, err := git.CurrentWorktreeRoot()
	if err != nil {
		return err
	}
	for i := range candidates {
		if samePath(candidates[i].Path, currentRoot) {
			candidates = append(append(candidates[:i:i], candidates[i+1:]...), candidates[i])
			break
		}
	}

	if len(candidates) > 0 {
		out := cmd.OutOrStdout()
		fmt.Fprintln(out, cleanTitleStyle.Render("Cleanup candidates"))
		fmt.Fprintln(out, cleanDescriptionStyle.Render("Merged, clean worktrees and branches to remove"))
		fmt.Fprintln(out)
		fmt.Fprintf(out, "  %s  %s\n", cleanHeaderStyle.Width(40).Render("BRANCH"), cleanHeaderStyle.Render("WORKTREE"))
		for _, entry := range candidates {
			fmt.Fprintf(out, "  %s  %s\n", cleanBranchStyle.Render(entry.Branch), cleanPathStyle.Render(entry.Path))
		}
		fmt.Fprintln(out)
	}
	if dryRun {
		fmt.Fprintf(os.Stderr, "Would remove %d merged, clean worktree(s).\n", len(candidates))
		return nil
	}
	if len(candidates) > 0 && !skipConfirm && !confirmYN(cmd, "Remove these worktrees and branches? [y/N] ") {
		fmt.Fprintln(os.Stderr, "Cancelled.")
		return nil
	}

	removed := 0
	for _, entry := range candidates {
		if err := deleteWorktree(cmd, entry.Path, entry.Branch, mainRoot, false); err != nil {
			return err
		}
		removed++
	}
	fmt.Fprintf(os.Stderr, "Removed %d merged, clean worktree(s).\n", removed)
	return nil
}
