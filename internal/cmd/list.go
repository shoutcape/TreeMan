package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
)

type listEntry struct {
	Path     string `json:"path"`
	Branch   string `json:"branch"`
	Main     bool   `json:"main"`
	Current  bool   `json:"current"`
	Dirty    bool   `json:"dirty"`
	Detached bool   `json:"detached"`
	Merged   bool   `json:"merged"`
}

func newListCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"wtl"},
		Short:   "List worktrees and their status",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd, jsonOutput)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print worktree state as JSON")
	return cmd
}

func runList(cmd *cobra.Command, jsonOutput bool) error {
	if !git.IsInsideRepo() {
		return fmt.Errorf("not inside a git repository")
	}
	entries, err := git.WorktreeList()
	if err != nil {
		return err
	}
	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return err
	}
	currentRoot, err := git.CurrentWorktreeRoot()
	if err != nil {
		return err
	}
	defaultBranch := ""
	mergedBranches := map[string]bool{}
	if defaultBranch, err = git.DetectDefaultBranch(); err == nil {
		if mergedBranches, err = git.MergedBranches("origin/" + defaultBranch); err != nil {
			mergedBranches = map[string]bool{}
		}
	}

	result := make([]listEntry, 0, len(entries))
	for _, entry := range entries {
		dirty, err := git.WorktreeDirty(entry.Path)
		if err != nil {
			return err
		}
		result = append(result, listEntry{
			Path:     entry.Path,
			Branch:   entry.Branch,
			Main:     samePath(entry.Path, mainRoot),
			Current:  samePath(entry.Path, currentRoot),
			Dirty:    dirty,
			Detached: entry.Branch == "",
			Merged:   defaultBranch != "" && entry.Branch != defaultBranch && mergedBranches[entry.Branch],
		})
	}

	if jsonOutput {
		return writeListJSON(cmd, result)
	}
	writeListHuman(cmd, result)
	return nil
}

func writeListJSON(cmd *cobra.Command, entries []listEntry) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	return encoder.Encode(entries)
}

func writeListHuman(cmd *cobra.Command, entries []listEntry) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "\n%sWORKTREES%s\n\n", ui.ColorCyan, ui.ColorReset)
	fmt.Fprintf(out, "    %s%-8s%s %-8s  %-6s  %-27s  %-25s\n", ui.ColorDim, "MARKERS", ui.ColorReset, "STATUS", "MERGED", "BRANCH", "PATH")
	fmt.Fprintf(out, "    %s%-8s%s %-8s  %-6s  %-27s  %-25s\n", ui.ColorDim, "───────", ui.ColorReset, "──────", "──────", "───────────────────────────", "─────────────────────────")
	for _, entry := range entries {
		branch := entry.Branch
		if entry.Detached {
			branch = "(detached)"
		}
		status, statusColor := listStatus(entry)
		markers := ""
		if entry.Main {
			markers = "M"
		}
		if entry.Current {
			markers += "▶"
		}
		merged := ""
		if entry.Merged {
			merged = "YES"
		}
		fmt.Fprintf(out, "    %s%-8s%s %s%-8s%s  %-6s  %s%-27s%s  %s%s%s\n",
			ui.ColorDim, markers, ui.ColorReset,
			statusColor, truncateListCell(status, 8), ui.ColorReset,
			merged,
			ui.ColorBranch, truncateListCell(branch, 27), ui.ColorReset,
			ui.ColorPath, displayListPath(entry.Path), ui.ColorReset,
		)
	}
}

func listStatus(entry listEntry) (string, string) {
	status := "CLEAN"
	color := ui.ColorStatus
	if entry.Detached {
		status = "DETACHED"
	}
	if entry.Dirty {
		if status == "CLEAN" {
			status = "DIRTY"
		}
		color = ui.ColorWarning
	}
	return status, color
}

func displayListPath(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && (path == home || strings.HasPrefix(path, home+string(os.PathSeparator))) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

func truncateListCell(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	return string(runes[:width-3]) + "..."
}
