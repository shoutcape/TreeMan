package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/deps"
	"github.com/shoutcape/treeman/internal/envfile"
	"github.com/shoutcape/treeman/internal/envrc"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/hooks"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/shoutcape/treeman/internal/worktree"
	"github.com/spf13/cobra"
)

// creationSetupOptions controls optional worktree setup actions.
type creationSetupOptions struct {
	skipEnv      bool
	skipDatabase bool
	skipDeps     bool
	skipHooks    bool
}

// setupCreatedWorktree performs the setup every newly created worktree gets.
// It works from the paths the flow already resolved, so the configuration that
// chose the worktree's location is the same one that decides what to ignore
// and which hooks to run.
func setupCreatedWorktree(w io.Writer, render ui.Renderer, paths creationPaths, created git.CreatedWorktree, options creationSetupOptions) setupSummary {
	if paths.config.ShouldUpdateGitignore() {
		if err := worktree.EnsureIgnored(paths.mainRoot, paths.parentDir); err != nil {
			fmt.Fprintln(w, render.Status(ui.ToneWarning, "!", fmt.Sprintf("could not update .gitignore: %v", err)))
		}
	}
	return runWorktreeSetup(w, render, worktreeSetup{
		mainRoot:      paths.mainRoot,
		worktreePath:  created.Path,
		branch:        created.Branch,
		worktreeDir:   paths.parentDir,
		projectConfig: paths.config,
		options:       options,
	})
}

// creationSetupFlag is one flag that turns a single setup action off.
type creationSetupFlag struct {
	name  string
	usage string
	value *bool
}

// creationSetupFlags declares those flags. Registration and the check for
// whether a caller set any of them both come from this list, so a new setup
// flag is one entry and nothing else.
func creationSetupFlags(options *creationSetupOptions) []creationSetupFlag {
	return []creationSetupFlag{
		{name: "skip-env", usage: "Skip copying .env* files", value: &options.skipEnv},
		{name: "skip-database", usage: "Skip branch database setup", value: &options.skipDatabase},
		{name: "skip-deps", usage: "Skip dependency installation", value: &options.skipDeps},
		{name: "skip-hooks", usage: "Skip post-create hooks", value: &options.skipHooks},
	}
}

func addCreationSetupFlags(cmd *cobra.Command, options *creationSetupOptions) {
	for _, flag := range creationSetupFlags(options) {
		cmd.Flags().BoolVar(flag.value, flag.name, false, flag.usage)
	}
}

func creationSetupFlagNames() []string {
	flags := creationSetupFlags(&creationSetupOptions{})
	names := make([]string, 0, len(flags))
	for _, flag := range flags {
		names = append(names, flag.name)
	}
	return names
}

// creationSetupFlagsChanged reports whether the caller turned any setup action
// off explicitly, as opposed to leaving every action enabled by default.
func creationSetupFlagsChanged(cmd *cobra.Command) bool {
	for _, name := range creationSetupFlagNames() {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

type worktreeSetup struct {
	mainRoot     string
	worktreePath string
	branch       string
	// worktreeDir is the configured worktree parent directory. Nested-module
	// discovery skips it so worktrees placed inside the repository are not
	// reported as modules of the tree that contains them.
	worktreeDir   string
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

// setupStep pairs one setup action with its reported result. Environment
// carries the detected tools because they describe that step's outcome, and
// every step names the flag that turns it off, so a caller reporting a step
// can say how to skip it.
type setupStep struct {
	name     string
	skipFlag string
	status   setupStatus
	tools    []envrc.ToolStatus
}

// steps lists the setup actions in report order. Every reader of a summary
// goes through this, so a new setup action only has to be added here.
func (summary setupSummary) steps() []setupStep {
	return []setupStep{
		{name: "Environment", skipFlag: "skip-env", status: summary.environment, tools: summary.environmentTools},
		{name: "Dependencies", skipFlag: "skip-deps", status: summary.dependencies},
		{name: "Database", skipFlag: "skip-database", status: summary.database},
		{name: "Hooks", skipFlag: "skip-hooks", status: summary.hooks},
	}
}

// failures describes every setup action that failed, using the lowercased step
// name so the text reads as a sentence fragment.
func (summary setupSummary) failures() []string {
	var failures []string
	for _, step := range summary.steps() {
		if step.status.kind == setupStatusFailed {
			failures = append(failures, strings.ToLower(step.name)+" "+step.status.text)
		}
	}
	return failures
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
	reportNestedModules(w, render, setup.worktreePath, setup.worktreeDir)

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
	for _, step := range summary.steps() {
		writeSetupStatus(w, render, step.name, step.status)
		for _, tool := range step.tools {
			writeEnvironmentToolStatus(w, render, tool)
		}
	}
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

// setupStatusWord names an outcome in one word, for callers that report what a
// setup run consisted of rather than its full per-step text.
func setupStatusWord(kind setupStatusKind) string {
	switch kind {
	case setupStatusFailed:
		return "failed"
	case setupStatusCompleted:
		return "completed"
	default:
		return "skipped"
	}
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
	fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", fmt.Sprintf("Detected %s, running %s...", installer.Manifest, command)))
	if err := deps.Run(worktreePath, installer, out); err != nil {
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("dependency installation failed: %v", err)))
		return failedStatus(fmt.Sprintf("failed: %v", err))
	}

	fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Completed "+command+"."))
	return completedStatus(fmt.Sprintf("completed: installed with %s", installer.Binary))
}

func reportNestedModules(w io.Writer, render ui.Renderer, dir string, excluded ...string) {
	modules, err := deps.DiscoverNestedModules(dir, excluded...)
	if err != nil {
		fmt.Fprintln(w, render.Status(ui.ToneWarning, "!", fmt.Sprintf("could not discover nested modules: %v", err)))
		return
	}
	for _, module := range modules {
		fmt.Fprintln(w, render.Status(ui.ToneMuted, "○", fmt.Sprintf("Nested module %s (%s): skipped; not installed automatically.", module.Path, module.Manifest)))
	}
}
