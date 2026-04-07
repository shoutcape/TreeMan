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
	if _, err := exec.LookPath("fzf"); err != nil {
		return fmt.Errorf("fzf is required for switch. Install it from https://github.com/junegunn/fzf")
	}

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

	// Build parallel display-rows and full-paths slices.
	var displayLines []string
	var fullPaths []string
	for _, e := range entries {
		displayLines = append(displayLines, ui.WorktreeRow(e.Path, e.Branch))
		fullPaths = append(fullPaths, e.Path)
	}

	display := strings.Join(displayLines, "\n")

	fzfArgs := []string{
		"--ansi",
		"--border-label", " worktrees ",
		"--prompt=switch > ",
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
		// User cancelled — not an error.
		return nil
	}

	selection := strings.TrimSpace(string(out))
	if selection == "" {
		return nil
	}

	// Map selection back to a full path by stripping ANSI and matching against
	// the plain-text versions of our display rows.
	idx := matchIndex(displayLines, selection)
	if idx < 0 {
		return fmt.Errorf("could not map fzf selection to a worktree path")
	}
	dest := fullPaths[idx]

	// Determine current directory to detect same-worktree selection.
	cwd, _ := os.Getwd()
	if dest == cwd {
		fmt.Fprintln(os.Stderr, "Already in this worktree.")
		return nil
	}

	short := filepath.Base(dest)
	fmt.Fprintf(os.Stderr, "cd → .../%s\n", short)

	// Print path to stdout for shell wrapper cd.
	fmt.Fprintln(cmd.OutOrStdout(), dest)

	return nil
}

// matchPath finds the full path corresponding to the fzf selection by
// comparing stripped ANSI versions of display lines.
func matchPath(displayLines, fullPaths []string, selection string) string {
	plainSelection := ui.StripANSI(strings.TrimSpace(selection))

	for i, line := range displayLines {
		if ui.StripANSI(line) == plainSelection {
			return fullPaths[i]
		}
	}
	return ""
}
