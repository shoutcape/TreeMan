package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
)

func newSwitchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "switch [query]",
		Short: "Switch between worktrees via fzf",
		Long: `Open an interactive fzf picker listing all worktrees.

An optional query pre-filters the list.

The selected worktree path is printed to stdout so that a shell wrapper
can cd into it.`,
		Aliases: []string{"wts"},
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			return runSwitch(cmd, query)
		},
	}
}

func runSwitch(cmd *cobra.Command, query string) error {
	entries, err := git.WorktreeList()
	if err != nil {
		return fmt.Errorf("not in a git repository or no worktrees found")
	}
	if len(entries) == 0 {
		return fmt.Errorf("no worktrees found")
	}
	if len(entries) == 1 {
		fmt.Fprintln(os.Stderr, "Only one worktree exists — nothing to switch to.")
		return nil
	}

	if query != "" {
		for _, entry := range entries {
			if entry.Path == query || entry.Branch == query {
				return printSwitchDestination(cmd, entry.Path)
			}
		}
	}

	if _, err := exec.LookPath("fzf"); err != nil {
		return fmt.Errorf("fzf is required for switch. Install it from https://github.com/junegunn/fzf")
	}

	// Keep fzf's visible label separate from its stable row identity.
	var displayLines []string
	var fullPaths []string
	for i, e := range entries {
		displayLines = append(displayLines, pickerRow(ui.WorktreeRow(e.Path, e.Branch), i))
		fullPaths = append(fullPaths, e.Path)
	}

	display := strings.Join(displayLines, "\n")

	fzfArgs := pickerArgs(" worktrees ", "switch > ")
	if query != "" {
		fzfArgs = append(fzfArgs, "--query", query)
	}

	fzfCmd := exec.Command("fzf", fzfArgs...)
	fzfCmd.Stdin = strings.NewReader(display)
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

	idx := pickerSelectionIndex(selection, len(fullPaths))
	if idx < 0 {
		return fmt.Errorf("could not map fzf selection to a worktree path")
	}
	dest := fullPaths[idx]

	return printSwitchDestination(cmd, dest)
}

func printSwitchDestination(cmd *cobra.Command, dest string) error {
	// Determine current directory to detect same-worktree selection.
	cwd, _ := os.Getwd()
	if dest == cwd {
		fmt.Fprintln(os.Stderr, "Already in this worktree.")
		return nil
	}

	short := filepath.Base(dest)
	fmt.Fprintln(os.Stderr, ui.RenderStatus(ui.ToneInfo, "→", fmt.Sprintf("cd .../%s", short)))

	// Print path to stdout for shell wrapper cd.
	fmt.Fprintln(cmd.OutOrStdout(), dest)

	return nil
}
