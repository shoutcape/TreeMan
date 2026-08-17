package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// creationSetupOptions controls optional worktree setup actions.
type creationSetupOptions struct {
	skipEnv      bool
	skipDatabase bool
	skipDeps     bool
	skipHooks    bool
}

func addCreationSetupFlags(cmd *cobra.Command, options *creationSetupOptions) {
	cmd.Flags().BoolVar(&options.skipEnv, "skip-env", false, "Skip copying .env* files")
	cmd.Flags().BoolVar(&options.skipDatabase, "skip-database", false, "Skip branch database setup")
	cmd.Flags().BoolVar(&options.skipDeps, "skip-deps", false, "Skip dependency installation")
	cmd.Flags().BoolVar(&options.skipHooks, "skip-hooks", false, "Skip post-create hooks")
}

func (o creationSetupOptions) printSkipped(w io.Writer) {
	for _, action := range []struct {
		skipped bool
		name    string
	}{
		{o.skipEnv, "environment file copy"},
		{o.skipDatabase, "database setup"},
		{o.skipDeps, "dependency installation"},
		{o.skipHooks, "post-create hooks"},
	} {
		if action.skipped {
			fmt.Fprintf(w, "  Skipped: %s (requested)\n", action.name)
		}
	}
}
