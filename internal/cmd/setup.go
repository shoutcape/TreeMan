package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/shoutcape/treeman/internal/ui"
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
			fmt.Fprintf(w, "  %s  %s\n", ui.RenderTone(ui.ToneMuted, "○"), ui.RenderMuted("Skipped: "+action.name+" (requested)"))
		}
	}
}

func writeSetupStatus(w io.Writer, name, status string) {
	tone, symbol := setupStatusAppearance(status)
	fmt.Fprintf(w, "  %s  %-14s %s\n", ui.RenderTone(tone, symbol), name, ui.RenderMuted(status))
}

func setupStatusAppearance(status string) (ui.Tone, string) {
	switch {
	case strings.Contains(status, "failed"):
		return ui.ToneFailure, "✗"
	case strings.HasPrefix(status, "completed"):
		return ui.ToneSuccess, "✓"
	default:
		return ui.ToneMuted, "○"
	}
}
