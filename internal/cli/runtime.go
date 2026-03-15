package cli

import (
	"github.com/spf13/cobra"
)

var runtimeCmd = &cobra.Command{
	Use:   "runtime",
	Short: "Manage per-worktree runtimes",
	Long: `Manage isolated runtimes for each worktree.

A runtime runs your project's dev server (or docker-compose stack) with
automatically allocated ports and environment variables, so multiple
worktrees can run simultaneously without collisions.

Configure runtimes via .treeman.yml in your project root.`,
}

func init() {
	runtimeCmd.AddCommand(runtimeUpCmd)
	runtimeCmd.AddCommand(runtimeDownCmd)
	runtimeCmd.AddCommand(runtimeStatusCmd)
	runtimeCmd.AddCommand(runtimeLogsCmd)
	runtimeCmd.AddCommand(runtimeEnvCmd)
	runtimeCmd.AddCommand(runtimeLsCmd)
}
