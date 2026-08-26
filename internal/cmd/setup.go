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

type setupStatus struct {
	description string
	warning     bool
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

type dependencySetupResult struct {
	status     setupStatus
	incomplete bool
}

// setupDependencies installs supported dependencies and reports manifests that
// require manual bootstrapping.
func setupDependencies(w io.Writer, render ui.Renderer, worktreePath string, skip bool) dependencySetupResult {
	result := dependencySetupResult{status: setupStatus{description: "skipped (requested)"}}
	if skip {
		return result
	}

	fmt.Fprintln(w, render.Status(ui.ToneInfo, "→", "Detecting dependencies..."))
	installResult, installErr := deps.Install(worktreePath, w)
	result.status.description = "skipped"
	switch {
	case installErr != nil:
		fmt.Fprintln(w, render.Status(ui.ToneWarning, "!", fmt.Sprintf("dependency installation failed: %v", installErr)))
		result.status.description = fmt.Sprintf("failed: %v", installErr)
	case installResult.Python:
		fmt.Fprintln(w, render.Status(ui.ToneMuted, "○", "Detected Python project, skipping auto-install (activate your venv manually)."))
		result.status.description = "skipped (Python project requires manual venv activation)"
	case installResult.Skipped && len(installResult.UnsupportedManifests) == 0:
		fmt.Fprintln(w, render.Status(ui.ToneMuted, "○", "No known dependency file detected, skipping install."))
	case installResult.Installer != nil:
		fmt.Fprintln(w, render.Status(ui.ToneInfo, "→", fmt.Sprintf("Detected %s, running %s %s...",
			installResult.Installer.Lockfile,
			installResult.Installer.Binary,
			strings.Join(installResult.Installer.Args, " "),
		)))
		result.status.description = fmt.Sprintf("completed: installed with %s", installResult.Installer.Binary)
	}
	if len(installResult.UnsupportedManifests) > 0 {
		manifests := strings.Join(installResult.UnsupportedManifests, ", ")
		fmt.Fprintln(w, render.Status(ui.ToneWarning, "!", "Unsupported dependency manifests were not bootstrapped: "+manifests))
		if result.status.description == "skipped" {
			result.status.description = "not bootstrapped: " + manifests
		} else {
			result.status.description += "; not bootstrapped: " + manifests
		}
		result.status.warning = true
		result.incomplete = true
	}
	return result
}

func printWorktreeReadiness(w io.Writer, render ui.Renderer, incomplete bool, subject string) {
	if incomplete {
		fmt.Fprintln(w, render.Status(ui.ToneWarning, "!", subject+" setup incomplete:"))
		return
	}
	fmt.Fprintln(w, render.Status(ui.ToneSuccess, "✓", subject+" ready:"))
}

func writeSetupStatus(w io.Writer, render ui.Renderer, name string, status setupStatus) {
	tone, symbol := setupStatusAppearance(status)
	fmt.Fprintf(w, "  %s  %-14s %s\n", render.Tone(tone, symbol), name, render.Tone(tone, render.Fit(status.description, 20)))
}

func setupStatusAppearance(status setupStatus) (ui.Tone, string) {
	switch {
	case strings.Contains(status.description, "failed"):
		return ui.ToneFailure, "✗"
	case status.warning:
		return ui.ToneWarning, "!"
	case strings.HasPrefix(status.description, "completed"):
		return ui.ToneSuccess, "✓"
	default:
		return ui.ToneMuted, "○"
	}
}
