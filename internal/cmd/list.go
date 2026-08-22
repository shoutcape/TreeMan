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
	remoteBranchExists := map[string]bool{}
	if defaultBranch, err = git.DetectDefaultBranch(); err == nil {
		if err = git.Fetch("refs/heads/" + defaultBranch + ":refs/remotes/origin/" + defaultBranch); err != nil {
			defaultBranch = ""
		} else {
			if mergedBranches, err = git.MergedBranches("origin/" + defaultBranch); err != nil {
				mergedBranches = map[string]bool{}
			}
			// Collect non-default branch names to check remote existence in one call.
			var nonDefaultBranches []string
			for _, entry := range entries {
				if entry.Branch != "" && entry.Branch != defaultBranch {
					nonDefaultBranches = append(nonDefaultBranches, entry.Branch)
				}
			}
			if len(nonDefaultBranches) > 0 {
				if remoteBranchExists, err = git.RemoteBranchesExist(nonDefaultBranches); err != nil {
					remoteBranchExists = map[string]bool{}
				}
			}
		}
	}

	result := make([]listEntry, 0, len(entries))
	for _, entry := range entries {
		dirty, err := git.WorktreeDirty(entry.Path)
		if err != nil {
			return err
		}
		// A branch is considered merged if:
		//   1. git branch --merged reports it as a direct ancestor of origin/main, OR
		//   2. the branch has no remote counterpart (remote deleted it, indicating a
		//      squash-merge or similar workflow where the branch tip is not a literal
		//      ancestor of main).
		// In both cases we also require that default-branch detection succeeded and
		// that the branch is not the default branch itself.
		isMerged := defaultBranch != "" &&
			entry.Branch != "" &&
			entry.Branch != defaultBranch &&
			(mergedBranches[entry.Branch] || !remoteBranchExists[entry.Branch])
		result = append(result, listEntry{
			Path:     entry.Path,
			Branch:   entry.Branch,
			Main:     samePath(entry.Path, mainRoot),
			Current:  samePath(entry.Path, currentRoot),
			Dirty:    dirty,
			Detached: entry.Branch == "",
			Merged:   isMerged,
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
	}
}

func listStatus(entry listEntry) (string, ui.Tone) {
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
