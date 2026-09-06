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
	var execCommand string
	cmd := &cobra.Command{
		Use:   "switch [query]",
		Short: "Switch between worktrees via fzf",
		Long: `Open an interactive fzf picker listing all worktrees.

An optional query pre-filters the list.

Shell integration changes directory to the selection; without it, the path
is printed to stdout. With --exec, TreeMan runs the given command in the
selected worktree instead.`,
		Aliases: commandAliases("switch"),
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			return runSwitch(cmd, query, execCommand)
		},
	}
	addLaunchFlag(cmd, &execCommand)

	return cmd
}

func runSwitch(cmd *cobra.Command, query, execCommand string) error {
	dest, err := selectWorktree(cmd, query, execCommand != "")
	if err != nil || dest == "" {
		return err
	}
	if execCommand != "" {
		// A launched command runs in the selection even when it is the current
		// worktree, because it is what the caller asked to run and not a
		// directory change that would do nothing.
		return deliverWorktree(cmd, dest, execCommand)
	}
	return printSwitchDestination(cmd, dest)
}

// selectWorktree resolves the worktree the caller wants, by exact query match
// or through the fzf picker. An empty path with a nil error means there is
// nothing to select or the picker was cancelled; either way the reason is
// already on stderr.
//
// launching reports whether a command will run in the selection, which is work
// worth doing in the current worktree even though changing directory to it is
// not.
func selectWorktree(cmd *cobra.Command, query string, launching bool) (string, error) {
	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	entries, err := git.WorktreeList()
	if err != nil {
		return "", fmt.Errorf("not in a git repository or no worktrees found")
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("no worktrees found")
	}
	if len(entries) == 1 && !launching {
		fmt.Fprintln(out, "Only one worktree exists - nothing to switch to.")
		return "", nil
	}

	if query != "" {
		for _, entry := range entries {
			if entry.Branch == query || samePath(entry.Path, query) {
				return entry.Path, nil
			}
		}
	} else if len(entries) == 1 {
		// Nothing to choose between, so the picker would ask an empty question.
		return entries[0].Path, nil
	}

	if !canInteract(cmd) {
		return "", fmt.Errorf("interactive selection is unavailable; pass an exact branch name or worktree path")
	}
	if _, err := exec.LookPath("fzf"); err != nil {
		return "", fmt.Errorf("fzf is required for switch. Install it from https://github.com/junegunn/fzf")
	}

	// Keep fzf's visible label separate from its stable row identity.
	var displayLines []string
	var fullPaths []string
	for i, e := range entries {
		displayLines = append(displayLines, pickerRow(render.WorktreeRow(e.Path, e.Branch), i))
		fullPaths = append(fullPaths, e.Path)
	}

	fzfArgs := pickerArgs(sessionFor(cmd).errorOutput.Color, " worktrees ", "switch > ")
	if query != "" {
		fzfArgs = append(fzfArgs, "--query", query)
	}

	fzfCmd := exec.Command("fzf", fzfArgs...)
	fzfCmd.Stdin = strings.NewReader(strings.Join(displayLines, "\n"))
	fzfCmd.Stderr = out

	selectionOutput, err := fzfCmd.Output()
	if err != nil {
		if pickerCancelled(err) {
			fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "Cancelled."))
			return "", nil
		}
		return "", fmt.Errorf("fzf failed while selecting a worktree: %w", err)
	}

	selection := strings.TrimSpace(string(selectionOutput))
	if selection == "" {
		fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "Cancelled."))
		return "", nil
	}

	idx := pickerSelectionIndex(selection, len(fullPaths))
	if idx < 0 {
		return "", fmt.Errorf("could not map fzf selection to a worktree path")
	}
	return fullPaths[idx], nil
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

	return reportDestination(cmd, dest)
}
