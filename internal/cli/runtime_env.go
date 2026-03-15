package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/shoutcape/TreeMan/internal/runtime"
	"github.com/spf13/cobra"
)

var runtimeEnvCmd = &cobra.Command{
	Use:   "env",
	Short: "Print assigned ports and environment variables",
	Long: `Print the environment variables and port assignments for the
current worktree's runtime.`,
	Args: cobra.NoArgs,
	RunE: runRuntimeEnv,
}

func runRuntimeEnv(cmd *cobra.Command, args []string) error {
	ctx, err := runtime.ResolveContext()
	if err != nil {
		return err
	}

	state, err := runtime.LoadState(ctx.Repo, ctx.BranchSlug)
	if err != nil {
		return err
	}

	if state == nil {
		return fmt.Errorf("no runtime found for this worktree\nRun 'treeman runtime up' to start one")
	}

	// Print env vars to stdout (machine-readable)
	fmt.Printf("TREEMAN_BRANCH=%s\n", state.Branch)
	fmt.Printf("TREEMAN_BRANCH_SLUG=%s\n", state.BranchSlug)
	fmt.Printf("TREEMAN_RUNTIME_NAME=%s-%s\n", state.Repo, state.BranchSlug)

	if len(state.Ports) > 0 {
		names := make([]string, 0, len(state.Ports))
		for name := range state.Ports {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			port := state.Ports[name]
			envName := fmt.Sprintf("%s_PORT", strings.ToUpper(name))
			fmt.Printf("%s=%d\n", envName, port)
			if name == "app" {
				fmt.Printf("PORT=%d\n", port)
			}
		}
	}

	if state.ComposeProjectName != "" {
		fmt.Printf("COMPOSE_PROJECT_NAME=%s\n", state.ComposeProjectName)
	}

	// Also show the env file path on stderr
	if state.EnvFile != "" {
		envPath, err := runtime.EnvFilePath(state)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Env file: %s\n", envPath)
	}

	return nil
}
