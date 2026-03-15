package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shoutcape/TreeMan/internal/git"
	"github.com/shoutcape/TreeMan/internal/runtime"
	"github.com/spf13/cobra"
)

var runtimeLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List all known runtimes for the current repo",
	Long: `List all runtimes that TreeMan is tracking for the current repository.

Shows branch, runtime type, status, and URLs for each runtime.`,
	Args: cobra.NoArgs,
	RunE: runRuntimeLs,
}

func runRuntimeLs(cmd *cobra.Command, args []string) error {
	if !git.IsInsideRepo() {
		return fmt.Errorf("not inside a git repository")
	}

	mainRoot, err := git.MainRoot()
	if err != nil {
		return err
	}

	repo := filepath.Base(mainRoot)

	states, err := runtime.ListStates(repo)
	if err != nil {
		return err
	}

	if len(states) == 0 {
		fmt.Fprintln(os.Stderr, "No runtimes found for this repository.")
		return nil
	}

	// Update statuses and sort by branch
	for _, state := range states {
		actual := runtime.CheckProcessStatus(state)
		if actual != state.Status {
			state.Status = actual
			runtime.SaveState(state)
		}
	}

	sort.Slice(states, func(i, j int) bool {
		return states[i].Branch < states[j].Branch
	})

	// Print table header
	fmt.Fprintf(os.Stderr, "%-30s %-15s %-10s %s\n",
		"BRANCH", "TYPE", "STATUS", "URL")
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("-", 80))

	for _, state := range states {
		url := ""
		if state.Status == "running" && state.Ports != nil {
			if appPort, ok := state.Ports["app"]; ok {
				url = fmt.Sprintf("http://localhost:%d", appPort)
			}
		}

		fmt.Fprintf(os.Stderr, "%-30s %-15s %-10s %s\n",
			truncate(state.Branch, 30),
			state.RuntimeType,
			state.Status,
			url)
	}

	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
