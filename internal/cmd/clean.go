package cmd

import (
	"fmt"

	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
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
		branchWidth := len("BRANCH")
		for _, entry := range candidates {
			branchWidth = max(branchWidth, len(entry.Branch))
		}

		// Stdout is reserved for the main worktree path when the current
		// worktree is removed, allowing the shell wrapper to navigate there.
		out := cmd.ErrOrStderr()
		fmt.Fprintln(out, ui.RenderTitle("Cleanup candidates"))
		fmt.Fprintln(out, ui.RenderMuted("Merged, clean worktrees and branches to remove"))
		fmt.Fprintln(out)
		fmt.Fprintf(out, "  %s  %s\n", ui.RenderHeader(fmt.Sprintf("%-*s", branchWidth, "BRANCH")), ui.RenderHeader("WORKTREE"))
		for _, entry := range candidates {
			fmt.Fprintf(out, "  %s  %s\n", ui.RenderBranch(fmt.Sprintf("%-*s", branchWidth, entry.Branch)), ui.RenderPath(entry.Path))
		}
		fmt.Fprintln(out)
	}
	if dryRun {
		fmt.Fprintln(cmd.ErrOrStderr(), ui.RenderStatus(ui.ToneInfo, "→", fmt.Sprintf("Would remove %d merged, clean worktree(s).", len(candidates))))
		return nil
	}
	if len(candidates) > 0 && !skipConfirm && !confirmYN(cmd, "Remove these worktrees and branches? [y/N] ") {
		fmt.Fprintln(cmd.ErrOrStderr(), ui.RenderStatus(ui.ToneMuted, "○", "Cancelled."))
		return nil
	}

	removed := 0
	for _, entry := range candidates {
		// Candidates are already checked for direct merge ancestry or a deleted remote.
		if err := deleteWorktree(cmd, entry.Path, entry.Branch, mainRoot, false, true); err != nil {
			return err
		}
		removed++
	}
	fmt.Fprintln(cmd.ErrOrStderr(), ui.RenderStatus(ui.ToneSuccess, "✓", fmt.Sprintf("Removed %d merged, clean worktree(s).", removed)))
	return nil
}
