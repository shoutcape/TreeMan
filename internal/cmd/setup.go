package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/deps"
	"github.com/shoutcape/treeman/internal/envfile"
	"github.com/shoutcape/treeman/internal/envrc"
	"github.com/shoutcape/treeman/internal/hooks"
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

type worktreeSetup struct {
	mainRoot      string
	worktreePath  string
	branch        string
	projectConfig config.Config
	options       creationSetupOptions
}

type setupSummary struct {
	environment      string
	environmentTools []envrc.ToolStatus
	dependencies     string
	database         string
	hooks            string
	databaseDocs     bool
}

// runWorktreeSetup performs the common best-effort setup actions for every
// newly created worktree and captures their results for a consistent summary.
func runWorktreeSetup(w io.Writer, render ui.Renderer, setup worktreeSetup) setupSummary {
	// Probe the source before copying so --skip-env and copy failures do not
	// hide an .envrc that would otherwise be supplied to the worktree.
	environmentTools := envrc.Detect(setup.mainRoot)
	if len(environmentTools) == 0 {
		environmentTools = envrc.Detect(setup.worktreePath)
	}

	environmentStatus := "skipped (requested)"
	if !setup.options.skipEnv {
		result, err := envfile.Copy(setup.mainRoot, setup.worktreePath)
		environmentStatus = "skipped (no environment files found)"
		if err != nil {
			fmt.Fprintln(w, render.Status(ui.ToneWarning, "!", fmt.Sprintf("could not copy env files: %v", err)))
			environmentStatus = fmt.Sprintf("failed: %v", err)
		} else if len(result.Copied) > 0 {
			for _, f := range result.Copied {
				fmt.Fprintln(w, render.Status(ui.ToneSuccess, "✓", "Copied "+f))
			}
			fmt.Fprintln(w, render.Status(ui.ToneSuccess, "✓", fmt.Sprintf("Copied %d env file(s) from main worktree.", len(result.Copied))))
			environmentStatus = fmt.Sprintf("completed: copied %d file(s)", len(result.Copied))
		}
	}

	databaseStatus := "skipped (requested)"
	if !setup.options.skipDatabase {
		databaseStatus = setupCreatedDatabase(w, render, setup.projectConfig, setup.worktreePath, setup.branch)
	}

	dependenciesStatus := "skipped (requested)"
	if !setup.options.skipDeps {
		dependenciesStatus = setupDependencies(w, render, setup.worktreePath)
	}
	reportNestedModules(w, render, setup.worktreePath)

	hooksStatus := "skipped (requested)"
	if !setup.options.skipHooks {
		if postCreateCmds := setup.projectConfig.PostCreateHooks(); len(postCreateCmds) > 0 {
			fmt.Fprintln(w, render.Status(ui.ToneInfo, "→", fmt.Sprintf("Running %d post-create hook(s)...", len(postCreateCmds))))
			hookResults := hooks.RunPostCreate(setup.worktreePath, postCreateCmds, w)
			for _, r := range hookResults {
				if r.Err != nil {
					fmt.Fprintln(w, render.Status(ui.ToneWarning, "!", fmt.Sprintf("hook %q failed: %v", r.Command, r.Err)))
				} else {
					fmt.Fprintln(w, render.Status(ui.ToneSuccess, "✓", "Ran: "+r.Command))
				}
			}
			hooksStatus = summarizeHooks(hookResults)
		} else {
			hooksStatus = "skipped (no post-create hooks configured)"
		}
	}

	return setupSummary{
		environment:      environmentStatus,
		environmentTools: environmentTools,
		dependencies:     dependenciesStatus,
		database:         databaseStatus,
		hooks:            hooksStatus,
		databaseDocs:     strings.HasPrefix(databaseStatus, "skipped"),
	}
}

func printSetupSummary(w io.Writer, render ui.Renderer, summary setupSummary) {
	fmt.Fprintln(w, render.Title("SETUP"))
	writeSetupStatus(w, render, "Environment", summary.environment)
	for _, tool := range summary.environmentTools {
		writeSetupStatus(w, render, tool.Name, tool.Status)
	}
	writeSetupStatus(w, render, "Dependencies", summary.dependencies)
	if summary.databaseDocs {
		fmt.Fprintf(w, "  %s  %-14s %s\n", render.Tone(ui.ToneMuted, "○"), "Database", render.Link(render.Fit("Not configured. Configure database", 20), databaseDocsURL))
	} else {
		writeSetupStatus(w, render, "Database", summary.database)
	}
	writeSetupStatus(w, render, "Hooks", summary.hooks)
}

func summarizeHooks(results []hooks.RunResult) string {
	succeeded := 0
	var failures []string
	for _, result := range results {
		if result.Err != nil {
			failures = append(failures, fmt.Sprintf("%q: %v", result.Command, result.Err))
			continue
		}
		succeeded++
	}

	if len(failures) == 0 {
		return fmt.Sprintf("completed: %d succeeded", succeeded)
	}
	return fmt.Sprintf("completed: %d succeeded, %d failed: %s", succeeded, len(failures), strings.Join(failures, "; "))
}

func joinArgs(args []string) string {
	return strings.Join(args, " ")
}

func writeSetupStatus(w io.Writer, render ui.Renderer, name, status string) {
	tone, symbol := setupStatusAppearance(status)
	fmt.Fprintf(w, "  %s  %-14s %s\n", render.Tone(tone, symbol), name, render.Tone(tone, render.Fit(status, 20)))
}

func setupStatusAppearance(status string) (ui.Tone, string) {
	switch {
	case strings.Contains(status, "failed"):
		return ui.ToneFailure, "✗"
	case strings.HasPrefix(status, "completed"), status == envrc.Available, status == envrc.Active, status == envrc.ActiveInCurrentShell:
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
