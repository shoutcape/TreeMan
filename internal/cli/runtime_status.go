package cli

import (
	"fmt"
	"os"
	"sort"

	"github.com/shoutcape/TreeMan/internal/runtime"
	"github.com/spf13/cobra"
)

var runtimeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show runtime status for current worktree",
	Long: `Show the runtime status for the current worktree.

Displays whether the runtime is running, stopped, or stale,
along with port assignments and URLs.`,
	Args: cobra.NoArgs,
	RunE: runRuntimeStatus,
}

func runRuntimeStatus(cmd *cobra.Command, args []string) error {
	ctx, err := runtime.ResolveContext()
	if err != nil {
		return err
	}

	state, err := runtime.LoadState(ctx.Repo, ctx.BranchSlug)
	if err != nil {
		return err
	}

	if state == nil {
		fmt.Fprintln(os.Stderr, "No runtime configured for this worktree.")
		fmt.Fprintln(os.Stderr, "Run 'treeman runtime up' to start one.")
		return nil
	}

	// Check actual process status
	actualStatus := runtime.CheckProcessStatus(state)
	if actualStatus != state.Status {
		state.Status = actualStatus
		runtime.SaveState(state)
	}

	fmt.Fprintf(os.Stderr, "Worktree: %s\n", ctx.BranchSlug)
	fmt.Fprintf(os.Stderr, "Branch:   %s\n", state.Branch)
	fmt.Fprintf(os.Stderr, "Type:     %s\n", state.RuntimeType)
	fmt.Fprintf(os.Stderr, "Status:   %s\n", state.Status)

	if state.PID > 0 {
		fmt.Fprintf(os.Stderr, "PID:      %d\n", state.PID)
	}

	if state.Command != "" {
		fmt.Fprintf(os.Stderr, "Command:  %s\n", state.Command)
	}

	if len(state.Ports) > 0 {
		names := make([]string, 0, len(state.Ports))
		for name := range state.Ports {
			names = append(names, name)
		}
		sort.Strings(names)

		fmt.Fprintln(os.Stderr, "Ports:")
		for _, name := range names {
			fmt.Fprintf(os.Stderr, "  %s: http://localhost:%d\n",
				name, state.Ports[name])
		}
	}

	if state.LogFile != "" {
		fmt.Fprintf(os.Stderr, "Logs:     %s\n", state.LogFile)
	}

	if !state.StartedAt.IsZero() {
		fmt.Fprintf(os.Stderr, "Started:  %s\n", state.StartedAt.Format("2006-01-02 15:04:05"))
	}

	if state.Status == "stale" {
		fmt.Fprintln(os.Stderr, "\nRuntime is stale (process exited unexpectedly).")
		fmt.Fprintln(os.Stderr, "Run 'treeman runtime up' to restart or 'treeman runtime down' to clean state.")
	}

	return nil
}
