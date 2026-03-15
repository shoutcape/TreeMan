package cli

import (
	"fmt"
	"os"

	"github.com/shoutcape/TreeMan/internal/runtime"
	"github.com/spf13/cobra"
)

var runtimeDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Stop runtime for current worktree",
	Long: `Stop the running runtime for the current worktree.

For process runtimes, sends SIGTERM and waits for exit.
For docker-compose runtimes, runs docker compose down.`,
	Args: cobra.NoArgs,
	RunE: runRuntimeDown,
}

func runRuntimeDown(cmd *cobra.Command, args []string) error {
	ctx, err := runtime.ResolveContext()
	if err != nil {
		return err
	}

	state, err := runtime.LoadState(ctx.Repo, ctx.BranchSlug)
	if err != nil {
		return err
	}

	if state == nil {
		return fmt.Errorf("no runtime found for this worktree")
	}

	actualStatus := runtime.CheckProcessStatus(state)
	if actualStatus == "stopped" {
		fmt.Fprintln(os.Stderr, "Runtime is already stopped.")
		state.Status = "stopped"
		runtime.SaveState(state)
		return nil
	}

	if actualStatus == "stale" {
		fmt.Fprintln(os.Stderr, "Runtime was stale (process already exited). Cleaning up state.")
		state.Status = "stopped"
		runtime.SaveState(state)
		return nil
	}

	fmt.Fprintf(os.Stderr, "Stopping runtime (PID %d)...\n", state.PID)

	switch state.RuntimeType {
	case "process":
		if err := runtime.StopProcess(state); err != nil {
			return fmt.Errorf("stopping process: %w", err)
		}
	case "docker-compose":
		return fmt.Errorf("docker-compose runtime stop is not yet implemented")
	default:
		return fmt.Errorf("unknown runtime type: %s", state.RuntimeType)
	}

	fmt.Fprintln(os.Stderr, "Runtime stopped.")
	return nil
}
