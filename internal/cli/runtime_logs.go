package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/shoutcape/TreeMan/internal/runtime"
	"github.com/spf13/cobra"
)

var runtimeLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Tail runtime logs for current worktree",
	Long: `Tail the log file for the current worktree's runtime.

For process runtimes, tails the captured stdout/stderr log file.
For docker-compose runtimes, proxies to docker compose logs.`,
	Args: cobra.NoArgs,
	RunE: runRuntimeLogs,
}

var logsFollow bool

func init() {
	runtimeLogsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Follow log output (like tail -f)")
}

func runRuntimeLogs(cmd *cobra.Command, args []string) error {
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

	if state.LogFile == "" {
		return fmt.Errorf("no log file recorded for this runtime")
	}

	if _, err := os.Stat(state.LogFile); err != nil {
		return fmt.Errorf("log file not found: %s", state.LogFile)
	}

	// Use tail to display logs
	tailArgs := []string{"-n", "100"}
	if logsFollow {
		tailArgs = append(tailArgs, "-f")
	}
	tailArgs = append(tailArgs, state.LogFile)

	tailCmd := exec.Command("tail", tailArgs...)
	tailCmd.Stdout = os.Stdout
	tailCmd.Stderr = os.Stderr
	tailCmd.Stdin = os.Stdin

	return tailCmd.Run()
}
