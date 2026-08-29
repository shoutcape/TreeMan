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
	skipSetup    bool
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

type worktreeSetup struct {
	mainRoot      string
	worktreePath  string
	branch        string
	projectConfig config.Config
	options       creationSetupOptions
}

type setupStatusKind int

const (
	setupStatusSkipped setupStatusKind = iota
	setupStatusCompleted
	setupStatusFailed
)

type setupStatus struct {
	text    string
	kind    setupStatusKind
	linkURL string
}

func skippedStatus(text string) setupStatus {
	return setupStatus{text: text, kind: setupStatusSkipped}
}

func completedStatus(text string) setupStatus {
	return setupStatus{text: text, kind: setupStatusCompleted}
}

func failedStatus(text string) setupStatus {
	return setupStatus{text: text, kind: setupStatusFailed}
}

type setupSummary struct {
	environment      setupStatus
	environmentTools []envrc.ToolStatus
	dependencies     setupStatus
	database         setupStatus
	hooks            setupStatus
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

	environmentStatus := skippedStatus("skipped (requested)")
	if !setup.options.skipEnv {
		result, err := envfile.Copy(setup.mainRoot, setup.worktreePath)
		environmentStatus = skippedStatus("skipped (no environment files found)")
		if err != nil {
			fmt.Fprintln(w, render.Status(ui.ToneWarning, "!", fmt.Sprintf("could not copy env files: %v", err)))
			environmentStatus = failedStatus(fmt.Sprintf("failed: %v", err))
		} else if len(result.Copied) > 0 {
			for _, f := range result.Copied {
				fmt.Fprintln(w, render.Status(ui.ToneSuccess, "✓", "Copied "+f))
			}
			fmt.Fprintln(w, render.Status(ui.ToneSuccess, "✓", fmt.Sprintf("Copied %d env file(s) from main worktree.", len(result.Copied))))
			environmentStatus = completedStatus(fmt.Sprintf("completed: copied %d file(s)", len(result.Copied)))
		}
	}

	databaseStatus := skippedStatus("skipped (requested)")
	if !setup.options.skipDatabase {
		databaseStatus = setupCreatedDatabase(w, render, setup.projectConfig, setup.worktreePath, setup.branch)
	}

	dependenciesStatus := skippedStatus("skipped (requested)")
	if !setup.options.skipDeps {
		dependenciesStatus = setupDependencies(w, render, setup.worktreePath)
	}
	reportNestedModules(w, render, setup.worktreePath)

	hooksStatus := skippedStatus("skipped (requested)")
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
			hooksStatus = skippedStatus("skipped (no post-create hooks configured)")
		}
	}

	return setupSummary{
		environment:      environmentStatus,
		environmentTools: environmentTools,
		dependencies:     dependenciesStatus,
		database:         databaseStatus,
		hooks:            hooksStatus,
	}
}

func printSetupSummary(w io.Writer, render ui.Renderer, summary setupSummary) {
	fmt.Fprintln(w, render.Title("SETUP"))
	writeSetupStatus(w, render, "Environment", summary.environment)
	for _, tool := range summary.environmentTools {
		writeEnvironmentToolStatus(w, render, tool)
	}
	writeSetupStatus(w, render, "Dependencies", summary.dependencies)
	writeSetupStatus(w, render, "Database", summary.database)
	writeSetupStatus(w, render, "Hooks", summary.hooks)
}

func summarizeHooks(results []hooks.RunResult) setupStatus {
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
		return completedStatus(fmt.Sprintf("completed: %d succeeded", succeeded))
	}
	return failedStatus(fmt.Sprintf("completed: %d succeeded, %d failed: %s", succeeded, len(failures), strings.Join(failures, "; ")))
}

func joinArgs(args []string) string {
	return strings.Join(args, " ")
}

func writeSetupStatus(w io.Writer, render ui.Renderer, name string, status setupStatus) {
	tone, symbol := setupStatusAppearance(status.kind)
	text := render.Tone(tone, render.Fit(status.text, 20))
	if status.linkURL != "" {
		text = render.Link(render.Fit(status.text, 20), status.linkURL)
	}
	fmt.Fprintf(w, "  %s  %-14s %s\n", render.Tone(tone, symbol), name, text)
}

func writeEnvironmentToolStatus(w io.Writer, render ui.Renderer, tool envrc.ToolStatus) {
	tone, symbol := environmentToolAppearance(tool.Status)
	fmt.Fprintf(w, "  %s  %-14s %s\n", render.Tone(tone, symbol), tool.Name, render.Tone(tone, render.Fit(tool.Status, 20)))
}

func setupStatusAppearance(kind setupStatusKind) (ui.Tone, string) {
	switch kind {
	case setupStatusFailed:
		return ui.ToneFailure, "✗"
	case setupStatusCompleted:
		return ui.ToneSuccess, "✓"
	default:
		return ui.ToneMuted, "○"
	}
}

func environmentToolAppearance(status string) (ui.Tone, string) {
	switch status {
	case envrc.Available, envrc.Active, envrc.ActiveInCurrentShell:
		return ui.ToneSuccess, "✓"
	default:
		return ui.ToneMuted, "○"
	}
}

// setupDependencies reports dependency setup consistently for every worktree flow.
func setupDependencies(out io.Writer, render ui.Renderer, worktreePath string) setupStatus {
	fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", "Detecting dependencies..."))

	detection, err := deps.Detect(worktreePath)
	if err != nil {
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("dependency installation failed: %v", err)))
		return failedStatus(fmt.Sprintf("failed: %v", err))
	}

	if detection.Python {
		fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "Detected Python project, skipping auto-install (activate your venv manually)."))
		return skippedStatus("skipped (Python project requires manual venv activation)")
	}
	if detection.Installer == nil {
		fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "No known dependency file detected, skipping install."))
		return skippedStatus("skipped")
	}

	installer := detection.Installer
	command := installer.Binary + " " + joinArgs(installer.Args)
	fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", fmt.Sprintf("Detected %s, running %s...", installer.Lockfile, command)))
	if err := deps.Run(worktreePath, installer, out); err != nil {
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("dependency installation failed: %v", err)))
		return failedStatus(fmt.Sprintf("failed: %v", err))
	}

	fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Completed "+command+"."))
	return completedStatus(fmt.Sprintf("completed: installed with %s", installer.Binary))
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
