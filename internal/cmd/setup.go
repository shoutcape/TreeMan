package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/shoutcape/treeman/internal/deps"
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

func (o creationSetupOptions) printSkipped(w io.Writer, render ui.Renderer) {
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
			fmt.Fprintf(w, "  %s  %s\n", render.Tone(ui.ToneMuted, "○"), render.Muted("Skipped: "+action.name+" (requested)"))
		}
	}
}

func writeSetupStatus(w io.Writer, render ui.Renderer, name, status string) {
	tone, symbol := setupStatusAppearance(status)
	fmt.Fprintf(w, "  %s  %-14s %s\n", render.Tone(tone, symbol), name, render.Tone(tone, render.Fit(status, 20)))
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

// setupDependencies reports dependency setup consistently for every worktree flow.
func setupDependencies(out io.Writer, render ui.Renderer, worktreePath string) string {
	fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", "Detecting dependencies..."))

	detection, err := deps.Detect(worktreePath)
	if err != nil {
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("dependency installation failed: %v", err)))
		return fmt.Sprintf("failed: %v", err)
	}

	if detection.Python {
		fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "Detected Python project, skipping auto-install (activate your venv manually)."))
		return "skipped (Python project requires manual venv activation)"
	}
	if detection.Installer == nil {
		fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "No known dependency file detected, skipping install."))
		return "skipped"
	}

	installer := detection.Installer
	command := installer.Binary + " " + joinArgs(installer.Args)
	fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", fmt.Sprintf("Detected %s, running %s...", installer.Lockfile, command)))
	if err := deps.Run(worktreePath, installer, out); err != nil {
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("dependency installation failed: %v", err)))
		return fmt.Sprintf("failed: %v", err)
	}

	fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Completed "+command+"."))
	return fmt.Sprintf("completed: installed with %s", installer.Binary)
}

func reportNestedModules(w io.Writer, render ui.Renderer, dir string) {
	modules, err := deps.DiscoverNestedModules(dir)
	if err != nil {
		fmt.Fprintln(w, render.Status(ui.ToneWarning, "!", fmt.Sprintf("could not discover nested modules: %v", err)))
		return
	}
	for _, module := range modules {
		fmt.Fprintln(w, render.Status(ui.ToneMuted, "○", fmt.Sprintf("Nested module %s (%s): skipped; not installed automatically.", module.Path, module.Manifest)))
	}
}
