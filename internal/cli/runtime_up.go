package cli

import (
	"fmt"
	"os"
	"sort"

	"github.com/shoutcape/TreeMan/internal/runtime"
	"github.com/spf13/cobra"
)

var runtimeUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Start runtime for current worktree",
	Long: `Start the configured runtime for the current worktree.

Reads .treeman.yml, allocates unique ports, generates .env.treeman,
and starts the runtime process or docker-compose stack.`,
	Args: cobra.NoArgs,
	RunE: runRuntimeUp,
}

func runRuntimeUp(cmd *cobra.Command, args []string) error {
	ctx, err := runtime.ResolveContext()
	if err != nil {
		return err
	}

	// Check for existing running runtime
	existing, err := runtime.LoadState(ctx.Repo, ctx.BranchSlug)
	if err != nil {
		return err
	}
	if existing != nil && existing.Status == "running" {
		status := runtime.CheckProcessStatus(existing)
		if status == "running" {
			return fmt.Errorf("runtime is already running (PID %d)\nUse 'treeman runtime down' to stop it first", existing.PID)
		}
		// Stale — update state and continue
		existing.Status = "stale"
		runtime.SaveState(existing)
	}

	// Load config
	cfgPath, err := runtime.FindConfig(ctx.WorktreePath)
	if err != nil {
		return err
	}

	cfg, err := runtime.LoadConfig(cfgPath)
	if err != nil {
		return err
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	switch cfg.Runtime.Type {
	case "process":
		return startProcessRuntime(cfg, ctx)
	case "docker-compose":
		return fmt.Errorf("docker-compose runtime is not yet implemented")
	default:
		return fmt.Errorf("unsupported runtime type: %s", cfg.Runtime.Type)
	}
}

func startProcessRuntime(cfg *runtime.Config, ctx *runtime.WorktreeContext) error {
	fmt.Fprintf(os.Stderr, "Starting runtime for %s...\n", ctx.Branch)

	state, err := runtime.StartProcess(cfg, ctx.WorktreePath, ctx.Repo, ctx.Branch, ctx.BranchSlug)
	if err != nil {
		return err
	}

	// Print summary
	fmt.Fprintf(os.Stderr, "\nRuntime started\n")
	fmt.Fprintf(os.Stderr, "  Worktree: %s\n", ctx.BranchSlug)
	fmt.Fprintf(os.Stderr, "  Type:     process\n")
	fmt.Fprintf(os.Stderr, "  PID:      %d\n", state.PID)

	// Print port URLs sorted by name
	if len(state.Ports) > 0 {
		names := make([]string, 0, len(state.Ports))
		for name := range state.Ports {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			fmt.Fprintf(os.Stderr, "  %s: http://localhost:%d\n",
				capitalize(name), state.Ports[name])
		}
	}

	fmt.Fprintf(os.Stderr, "  Logs:     %s\n", state.LogFile)

	return nil
}

func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	return string(s[0]-32) + s[1:]
}
