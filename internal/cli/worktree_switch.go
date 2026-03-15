package cli

import (
	"fmt"
	"os"

	"github.com/shoutcape/TreeMan/internal/git"
	"github.com/shoutcape/TreeMan/internal/ui"
	"github.com/spf13/cobra"
)

var worktreeSwitchCmd = &cobra.Command{
	Use:   "switch [query]",
	Short: "Switch between worktrees via fzf picker",
	Long: `Interactively select a worktree to switch to using fzf.

An optional query argument pre-filters the fzf list.
Prints the selected worktree path to stdout for the shell wrapper to cd into.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWorktreeSwitch,
}

func runWorktreeSwitch(cmd *cobra.Command, args []string) error {
	if !git.IsInsideRepo() {
		return fmt.Errorf("not inside a git repository")
	}

	worktrees, err := git.WorktreeList()
	if err != nil {
		return err
	}

	if len(worktrees) == 0 {
		return fmt.Errorf("no worktrees found")
	}

	if len(worktrees) == 1 {
		fmt.Fprintln(os.Stderr, "Only one worktree exists — nothing to switch to.")
		return nil
	}

	// Build display lines and keep track of full paths
	displayLines := formatWorktreeDisplay(worktrees, "")
	query := ""
	if len(args) > 0 {
		query = args[0]
	}

	selection, err := ui.Fzf(displayLines, ui.FzfOptions{
		BorderLabel: " worktrees ",
		Prompt:      "switch > ",
		Query:       query,
		Ansi:        true,
	})
	if err != nil {
		return err
	}

	if selection == "" {
		return nil
	}

	// Find which worktree was selected by matching the display line
	idx := findSelectionIndex(displayLines, selection)
	if idx < 0 || idx >= len(worktrees) {
		return fmt.Errorf("could not determine selected worktree")
	}

	dest := worktrees[idx].Path

	cwd, _ := os.Getwd()
	if dest == cwd {
		fmt.Fprintln(os.Stderr, "Already in this worktree.")
		return nil
	}

	short := shortPath(dest)
	fmt.Fprintf(os.Stderr, "cd → …/%s\n", short)

	// Print path to stdout for shell wrapper
	fmt.Println(dest)
	return nil
}

// formatWorktreeDisplay builds ANSI-colored display lines for worktrees.
// If excludeMain is non-empty, that path is excluded from the list.
func formatWorktreeDisplay(worktrees []git.Worktree, excludeMain string) []string {
	var lines []string

	for _, wt := range worktrees {
		if excludeMain != "" && wt.Path == excludeMain {
			continue
		}

		short := shortPath(wt.Path)
		branch := wt.Branch
		if branch == "" {
			branch = "(detached)"
		}

		// TreeMan palette: #C4915E warm brown (path), #B2B644 bright olive (branch)
		line := fmt.Sprintf(
			"\033[38;2;196;145;94m%-40s\033[0m  \033[38;2;178;182;68m[%s]\033[0m",
			short, branch)
		lines = append(lines, line)
	}

	return lines
}

// shortPath returns the last two path components for display.
func shortPath(path string) string {
	parts := splitPath(path)
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return path
}

// splitPath splits a path into components.
func splitPath(path string) []string {
	var parts []string
	for _, p := range splitOnSlash(path) {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func splitOnSlash(s string) []string {
	var result []string
	current := ""
	for _, c := range s {
		if c == '/' {
			if current != "" {
				result = append(result, current)
			}
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

// findSelectionIndex finds the index of the selected line in the display lines.
func findSelectionIndex(displayLines []string, selection string) int {
	// Strip ANSI codes for comparison
	cleanSelection := stripAnsi(selection)
	for i, line := range displayLines {
		if stripAnsi(line) == cleanSelection {
			return i
		}
	}
	return -1
}

// stripAnsi removes ANSI escape codes from a string.
func stripAnsi(s string) string {
	result := ""
	inEscape := false
	for _, c := range s {
		if c == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
				inEscape = false
			}
			continue
		}
		result += string(c)
	}
	return result
}
