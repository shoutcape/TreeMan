package cmd

import (
	"fmt"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/database"
	"github.com/shoutcape/treeman/internal/deps"
	"github.com/shoutcape/treeman/internal/envfile"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
)

type preflightStatus struct {
	name    string
	message string
	tone    ui.Tone
	symbol  string
}

func newPreflightCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "preflight",
		Short: "Report setup compatibility without creating a worktree",
		Args:  cobra.NoArgs,
		RunE:  runPreflight,
	}
}

func runPreflight(cmd *cobra.Command, _ []string) error {
	if !git.IsInsideRepo() {
		return fmt.Errorf("not inside a git repository")
	}

	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return err
	}
	configResult := config.Load(mainRoot)
	statuses := []preflightStatus{
		preflightEnvironmentStatus(mainRoot),
	}
	statuses = append(statuses, preflightDependenciesStatuses(mainRoot)...)
	statuses = append(statuses, preflightConfigurationStatus(configResult))
	statuses = append(statuses,
		preflightDatabaseStatus(mainRoot, configResult),
		preflightHooksStatus(configResult),
	)
	writePreflight(cmd.ErrOrStderr(), commandRenderer(cmd), statuses)
	return nil
}

func preflightEnvironmentStatus(dir string) preflightStatus {
	files, err := envfile.Files(dir)
	if err != nil {
		return preflightStatus{name: "Environment", message: fmt.Sprintf("unavailable: could not read .env* files: %v", err), tone: ui.ToneFailure, symbol: "✗"}
	}
	if len(files) == 0 {
		return preflightStatus{name: "Environment", message: "not configured: no .env* files found", tone: ui.ToneMuted, symbol: "○"}
	}
	return preflightStatus{name: "Environment", message: fmt.Sprintf("ready: %d .env file(s) will be copied", len(files)), tone: ui.ToneSuccess, symbol: "✓"}
}

func preflightDependenciesStatuses(dir string) []preflightStatus {
	detection, err := deps.Detect(dir)
	if err != nil {
		return []preflightStatus{{name: "Dependencies", message: fmt.Sprintf("unavailable: %v", err), tone: ui.ToneFailure, symbol: "✗"}}
	}

	statuses := []preflightStatus{preflightDependenciesStatus(detection)}
	modules, err := deps.DiscoverNestedModules(dir)
	if err != nil {
		return append(statuses, preflightStatus{name: "Nested modules", message: fmt.Sprintf("unavailable: %v", err), tone: ui.ToneFailure, symbol: "✗"})
	}
	for _, module := range modules {
		statuses = append(statuses, preflightStatus{name: "Nested module", message: fmt.Sprintf("%s (%s): skipped; not installed automatically.", module.Path, module.Manifest), tone: ui.ToneMuted, symbol: "○"})
	}
	return statuses
}

func preflightDependenciesStatus(detection deps.Detection) preflightStatus {
	if detection.Installer == nil {
		if detection.Python {
			return preflightStatus{name: "Dependencies", message: "manual setup: Python virtualenv activation required", tone: ui.ToneMuted, symbol: "○"}
		}
		return preflightStatus{name: "Dependencies", message: "not configured at repository root", tone: ui.ToneMuted, symbol: "○"}
	}
	installer := detection.Installer
	if err := deps.InstallerAvailable(installer); err != nil {
		return preflightStatus{name: "Dependencies", message: fmt.Sprintf("limited: %s detected but %s is not installed", installer.Manifest, installer.Binary), tone: ui.ToneWarning, symbol: "!"}
	}
	return preflightStatus{name: "Dependencies", message: fmt.Sprintf("ready: %s detected; will run %s %s", installer.Manifest, installer.Binary, joinArgs(installer.Args)), tone: ui.ToneSuccess, symbol: "✓"}
}

func preflightConfigurationStatus(result config.LoadResult) preflightStatus {
	if result.Warning != "" {
		return preflightStatus{name: "Configuration", message: "unavailable: " + result.Warning, tone: ui.ToneFailure, symbol: "✗"}
	}
	if result.Path == "" {
		return preflightStatus{name: "Configuration", message: "not configured: .treeman.toml not found", tone: ui.ToneMuted, symbol: "○"}
	}
	return preflightStatus{name: "Configuration", message: "ready: .treeman.toml loaded", tone: ui.ToneSuccess, symbol: "✓"}
}

func preflightDatabaseStatus(mainRoot string, result config.LoadResult) preflightStatus {
	if result.Warning != "" {
		return preflightStatus{name: "Database", message: "unavailable: configuration is invalid", tone: ui.ToneFailure, symbol: "✗"}
	}
	envKey := result.Config.DatabaseEnvKey()
	if envKey == "" {
		return preflightStatus{name: "Database", message: "not configured: add [database] to .treeman.toml", tone: ui.ToneMuted, symbol: "○"}
	}
	probe, err := database.Probe(mainRoot, envKey, result.Config.DatabaseContainer())
	if err != nil {
		return preflightStatus{name: "Database", message: fmt.Sprintf("limited: %v", err), tone: ui.ToneWarning, symbol: "!"}
	}
	if probe.Skipped {
		return preflightStatus{name: "Database", message: fmt.Sprintf("not configured: no supported PostgreSQL URI found for %s", envKey), tone: ui.ToneMuted, symbol: "○"}
	}
	return preflightStatus{name: "Database", message: fmt.Sprintf("ready: %s has a PostgreSQL URI", envKey), tone: ui.ToneSuccess, symbol: "✓"}
}

func preflightHooksStatus(result config.LoadResult) preflightStatus {
	if result.Warning != "" {
		return preflightStatus{name: "Hooks", message: "unavailable: configuration is invalid", tone: ui.ToneFailure, symbol: "✗"}
	}
	count := len(result.Config.PostCreateHooks())
	if count == 0 {
		return preflightStatus{name: "Hooks", message: "not configured: no post-create hooks", tone: ui.ToneMuted, symbol: "○"}
	}
	return preflightStatus{name: "Hooks", message: fmt.Sprintf("ready: %d post-create hook(s) will run", count), tone: ui.ToneSuccess, symbol: "✓"}
}

func writePreflight(out interface{ Write([]byte) (int, error) }, render ui.Renderer, statuses []preflightStatus) {
	fmt.Fprintf(out, "\n%s\n\n", render.Title("COMPATIBILITY PREFLIGHT"))
	for _, status := range statuses {
		fmt.Fprintf(out, "  %s  %-14s %s\n", render.Tone(status.tone, status.symbol), status.name, render.Tone(status.tone, status.message))
	}
}
