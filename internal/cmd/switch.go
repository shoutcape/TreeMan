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
	var launchOptions worktreeLaunchOptions
	cmd := &cobra.Command{
		Use:   "switch [query]",
		Short: "Switch between worktrees via fzf",
		Long: `Open an interactive fzf picker listing all worktrees.

An optional query pre-filters the list.

The selected worktree path is printed to stdout so that a shell wrapper
can cd into it. With --exec, TreeMan runs the given command in the selected
worktree instead of printing the path.`,
		Aliases: []string{"wts"},
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := launchOptions.validate(cmd); err != nil {
				return err
			}
			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			return runSwitch(cmd, query, launchOptions)
		},
	}
	addLaunchFlag(cmd, &launchOptions)

	return cmd
}

func runSwitch(cmd *cobra.Command, query string, launchOptions worktreeLaunchOptions) error {
	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	entries, err := git.WorktreeList()
	if err != nil {
		return fmt.Errorf("not in a git repository or no worktrees found")
	}
	if len(entries) == 0 {
		return fmt.Errorf("no worktrees found")
	}
	if len(entries) == 1 {
		fmt.Fprintln(out, "Only one worktree exists - nothing to switch to.")
		return nil
	}

	if query != "" {
		for _, entry := range entries {
			if entry.Branch == query || samePath(entry.Path, query) {
				return deliverSwitchDestination(cmd, entry.Path, launchOptions)
			}
		}
	}
	if !canInteract(cmd) {
		return fmt.Errorf("interactive selection is unavailable; pass an exact branch name or worktree path")
	}

	if _, err := exec.LookPath("fzf"); err != nil {
		return fmt.Errorf("fzf is required for switch. Install it from https://github.com/junegunn/fzf")
	}

	// Keep fzf's visible label separate from its stable row identity.
	var displayLines []string
	var fullPaths []string
	for i, e := range entries {
		displayLines = append(displayLines, pickerRow(render.WorktreeRow(e.Path, e.Branch), i))
		fullPaths = append(fullPaths, e.Path)
	}

	display := strings.Join(displayLines, "\n")

	fzfArgs := pickerArgs(sessionFor(cmd).errorOutput.Color, " worktrees ", "switch > ")
	if query != "" {
		fzfArgs = append(fzfArgs, "--query", query)
	}

	fzfCmd := exec.Command("fzf", fzfArgs...)
	fzfCmd.Stdin = strings.NewReader(display)
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

	idx := pickerSelectionIndex(selection, len(fullPaths))
	if idx < 0 {
		return fmt.Errorf("could not map fzf selection to a worktree path")
	}
	dest := fullPaths[idx]

	return deliverSwitchDestination(cmd, dest, launchOptions)
}

// deliverSwitchDestination hands the selected worktree to --exec, or reports it
// as a directory to change to. A launched command runs in the selection even
// when it is the current worktree, because it is what the caller asked to run
// and not a directory change that would do nothing.
func deliverSwitchDestination(cmd *cobra.Command, dest string, launchOptions worktreeLaunchOptions) error {
	if launchOptions.command != "" {
		return deliverWorktree(cmd, dest, launchOptions)
	}
	return printSwitchDestination(cmd, dest)
}

func printSwitchDestination(cmd *cobra.Command, dest string) error {
	// Determine current directory to detect same-worktree selection.
	// Compare resolved paths because a symlinked directory reaches the same
	// worktree through a different raw path.
	cwd, _ := os.Getwd()
	if samePath(dest, cwd) {
		fmt.Fprintln(cmd.ErrOrStderr(), "Already in this worktree.")
		return nil
	}

	short := filepath.Base(dest)
	fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneInfo, "→", fmt.Sprintf("cd .../%s", short)))

	// Print path to stdout for shell wrapper cd.
	fmt.Fprintln(cmd.OutOrStdout(), dest)

	return nil
}
