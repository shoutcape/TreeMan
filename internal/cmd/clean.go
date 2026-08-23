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
	render := commandRenderer(cmd)
	if !git.IsInsideRepo() {
		return fmt.Errorf("not inside a git repository")
	}
	defaultBranch, err := git.DetectDefaultBranch()
	if err != nil {
		return err
	}
	entries, err := git.WorktreeList()
	if err != nil {
		return err
	}
	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return err
	}

	out := cmd.ErrOrStderr()
	var branchNames []string
	for _, entry := range entries {
		if entry.Branch == "" || entry.Branch == defaultBranch || samePath(entry.Path, mainRoot) {
			continue
		}
		branchNames = append(branchNames, entry.Branch)
	}
	state, err := refreshMergeState(defaultBranch, branchNames)
	if err != nil {
		return err
	}
	verified, warning, err := classifyCleanable("origin/"+defaultBranch, defaultBranch, branchNames, state)
	if err != nil {
		return err
	}
	if warning != "" {
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", warning))
	}

	var candidates []git.WorktreeEntry
	for _, entry := range entries {
		if entry.Branch == "" || entry.Branch == defaultBranch || samePath(entry.Path, mainRoot) || verified[entry.Branch] == "" {
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
		fmt.Fprintln(out, render.Title("Cleanup candidates"))
		fmt.Fprintln(out, render.Muted("Merged, clean worktrees and branches to remove"))
		fmt.Fprintln(out)
		fmt.Fprintf(out, "  %s  %s\n", render.Header(fmt.Sprintf("%-*s", branchWidth, "BRANCH")), render.Header("WORKTREE"))
		for _, entry := range candidates {
			fmt.Fprintf(out, "  %s  %s\n", render.Branch(fmt.Sprintf("%-*s", branchWidth, entry.Branch)), render.Path(entry.Path))
		}
		fmt.Fprintln(out)
	}
	if dryRun {
		fmt.Fprintln(cmd.ErrOrStderr(), render.Status(ui.ToneInfo, "→", fmt.Sprintf("Would remove %d merged, clean worktree(s).", len(candidates))))
		return nil
	}
	if len(candidates) > 0 && !skipConfirm {
		confirmed, err := confirmYN(cmd, "Remove these worktrees and branches? [y/N] ")
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(cmd.ErrOrStderr(), render.Status(ui.ToneMuted, "○", "Cancelled."))
			return nil
		}
	}
	if len(candidates) > 0 {
		candidateBranches := make([]string, 0, len(candidates))
		for _, entry := range candidates {
			candidateBranches = append(candidateBranches, entry.Branch)
		}
		state, err := refreshMergeState(defaultBranch, candidateBranches)
		if err != nil {
			return err
		}
		verified, warning, err = classifyCleanable("origin/"+defaultBranch, defaultBranch, candidateBranches, state)
		if err != nil {
			return err
		}
		if warning != "" {
			fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", warning))
		}
	}

	removed := 0
	for _, entry := range candidates {
		expectedSHA := verified[entry.Branch]
		if expectedSHA == "" {
			continue
		}
		dirty, err := git.WorktreeDirty(entry.Path)
		if err != nil {
			return err
		}
		if dirty {
			continue
		}
		// Candidates are verified merges: ancestors of the freshly fetched
		// default branch or forge-confirmed squash/rebase merges.
		if err := deleteWorktreeAtSHA(cmd, entry.Path, entry.Branch, mainRoot, false, true, expectedSHA); err != nil {
			return err
		}
		removed++
	}
	fmt.Fprintln(cmd.ErrOrStderr(), render.Status(ui.ToneSuccess, "✓", fmt.Sprintf("Removed %d merged, clean worktree(s).", removed)))
	return nil
}
