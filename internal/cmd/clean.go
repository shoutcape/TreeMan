package cmd

import (
	"fmt"
	"os"

	"github.com/shoutcape/treeman/internal/git"
	"github.com/spf13/cobra"
)

func newCleanCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove clean worktrees with branches merged into the default branch",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClean(cmd, dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show worktrees that would be removed")
	return cmd
}

func runClean(cmd *cobra.Command, dryRun bool) error {
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

	var candidates []git.WorktreeEntry
	for _, entry := range entries {
		if entry.Branch == "" || entry.Branch == defaultBranch || samePath(entry.Path, mainRoot) || !mergedBranches[entry.Branch] {
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

	// Remove the current worktree last. Removing it invalidates the process
	// working directory, but deleteWorktree emits mainRoot for the shell wrapper.
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

	removed := 0
	for _, entry := range candidates {
		if dryRun {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\n", entry.Path)
			removed++
			continue
		}
		if err := deleteWorktree(cmd, entry.Path, entry.Branch, mainRoot, false); err != nil {
			return err
		}
		removed++
	}
	if dryRun {
		fmt.Fprintf(os.Stderr, "Would remove %d merged, clean worktree(s).\n", removed)
	} else {
		fmt.Fprintf(os.Stderr, "Removed %d merged, clean worktree(s).\n", removed)
	}
	return nil
}
