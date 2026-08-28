package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/merge"
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
	Stale    bool   `json:"stale"`
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
	return runListWithClassifier(cmd, merge.NewClassifier(), jsonOutput)
}

func runListWithClassifier(cmd *cobra.Command, classifier merge.ClassifierFunc, jsonOutput bool) error {
	entries, err := git.WorktreeList()
	if err != nil {
		return err
	}
	mainRoot, err := mainWorktreeRoot(entries)
	if err != nil {
		return err
	}
	inspected, err := git.InspectWorktrees(entries)
	if err != nil {
		return err
	}
	currentRoot, err := git.CurrentWorktreeRoot()
	if err != nil {
		return err
	}
	defaultBranch := ""
	verified := map[string]string{}
	if defaultBranch, err = git.DetectDefaultBranch(); err == nil {
		var branchNames []string
		for _, worktree := range inspected {
			entry := worktree.Entry
			if entry.Branch != "" && entry.Branch != defaultBranch && worktree.State != git.WorktreeStateStale {
				branchNames = append(branchNames, entry.Branch)
			}
		}
		classification, classifyErr := classifier(defaultBranch, branchNames)
		if classifyErr != nil {
			writeMergeDiagnostics(cmd.ErrOrStderr(), outputRenderer(cmd), []merge.Diagnostic{{Operation: "merge status unavailable", Err: classifyErr}})
			defaultBranch = ""
		} else {
			for _, candidate := range classification.Merged {
				if candidate.SHA != "" {
					verified[candidate.Branch] = candidate.SHA
				}
			}
			writeMergeDiagnostics(cmd.ErrOrStderr(), outputRenderer(cmd), classification.Diagnostics)
		}
	}
	result := make([]listEntry, 0, len(entries))
	for _, worktree := range inspected {
		entry := worktree.Entry
		// MERGED reports whether the tip is integrated into origin/<default> or
		// descends from a merged PR/MR head. Cleanup uses stricter exact evidence.
		isMerged := defaultBranch != "" &&
			entry.Branch != "" &&
			entry.Branch != defaultBranch &&
			verified[entry.Branch] != ""
		result = append(result, listEntry{
			Path:     entry.Path,
			Branch:   entry.Branch,
			Main:     samePath(entry.Path, mainRoot),
			Current:  samePath(entry.Path, currentRoot),
			Dirty:    worktree.State == git.WorktreeStateDirty,
			Detached: entry.Branch == "",
			Merged:   isMerged,
			Stale:    worktree.State == git.WorktreeStateStale,
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
	render := outputRenderer(cmd)
	fmt.Fprintf(out, "\n%s\n\n", render.Title("WORKTREES"))
	fmt.Fprintf(out, "    %s\n", render.Header(fmt.Sprintf("%-8s %-8s  %-6s  %-27s  %-25s", "MARKERS", "STATUS", "MERGED", "BRANCH", "PATH")))
	fmt.Fprintf(out, "    %s\n", render.Muted(fmt.Sprintf("%-8s %-8s  %-6s  %-27s  %-25s", "───────", "──────", "──────", "───────────────────────────", "─────────────────────────")))
	staleCount := 0
	for _, entry := range entries {
		branch := entry.Branch
		if entry.Detached {
			branch = "(detached)"
		}
		status, tone := listStatus(entry)
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
		fmt.Fprintf(out, "    %s %s  %-6s  %s  %s\n",
			render.Muted(fmt.Sprintf("%-8s", markers)),
			render.Tone(tone, fmt.Sprintf("%-8s", truncateListCell(status, 8))),
			merged,
			render.Branch(fmt.Sprintf("%-27s", truncateListCell(branch, 27))),
			render.Path(displayListPath(entry.Path)),
		)
		if entry.Stale {
			staleCount++
		}
	}
	if staleCount > 0 {
		fmt.Fprintf(out, "\n%s\n", render.Muted(fmt.Sprintf("  %d stale worktree(s) -- directory missing or not a directory. Run: git worktree prune", staleCount)))
	}
}

func listStatus(entry listEntry) (string, ui.Tone) {
	if entry.Stale {
		return "STALE", ui.ToneWarning
	}
	status := "CLEAN"
	tone := ui.ToneSuccess
	if entry.Detached {
		status = "DETACHED"
		tone = ui.ToneMuted
	}
	if entry.Dirty {
		if status == "CLEAN" {
			status = "DIRTY"
		}
		tone = ui.ToneWarning
	}
	return status, tone
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
