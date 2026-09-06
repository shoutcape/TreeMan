package cmd

import (
	"fmt"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/database"
	"github.com/shoutcape/treeman/internal/deps"
	"github.com/shoutcape/treeman/internal/envfile"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/shoutcape/treeman/internal/worktree"
	"github.com/spf13/cobra"
)

type preflightStatus struct {
	id      string
	name    string
	message string
	status  CheckStatus
}

func newPreflightCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Report setup compatibility without creating a worktree",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPreflight(cmd, jsonOutput)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print compatibility report as JSON")
	return cmd
}

func runPreflight(cmd *cobra.Command, jsonOutput bool) error {
	if !git.IsInsideRepo() {
		err := fmt.Errorf("not inside a git repository")
		return writePreflightSetupFailure(cmd, jsonOutput, "repository", err)
	}

	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return writePreflightSetupFailure(cmd, jsonOutput, "worktree", err)
	}
	configResult := config.Load(mainRoot)
	var worktreeDir string
	if configResult.Warning == "" {
		worktreeDir, err = worktree.ResolveDir(mainRoot, configResult.Config.WorktreeDirSetting())
		if err != nil {
			configResult.Warning = err.Error()
		}
	}
	statuses := []preflightStatus{
		preflightEnvironmentStatus(mainRoot),
	}
	statuses = append(statuses, preflightDependenciesStatuses(mainRoot, worktreeDir)...)
	statuses = append(statuses, preflightConfigurationStatus(configResult))
	statuses = append(statuses,
		preflightDatabaseStatus(mainRoot, configResult),
		preflightHooksStatus(configResult),
	)
	if jsonOutput {
		return writeJSONReport(cmd, newReport(preflightChecks(statuses)))
	}
	writePreflight(cmd.ErrOrStderr(), commandRenderer(cmd), statuses)
	return nil
}

func writePreflightSetupFailure(cmd *cobra.Command, jsonOutput bool, name string, err error) error {
	if jsonOutput {
		if writeErr := writeJSONReport(cmd, newReport([]Check{{Name: name, Status: CheckFail, Message: err.Error(), Hint: ""}})); writeErr != nil {
			return writeErr
		}
	}
	return err
}

func preflightChecks(statuses []preflightStatus) []Check {
	checks := make([]Check, 0, len(statuses))
	for _, status := range statuses {
		checks = append(checks, Check{Name: status.id, Status: status.status, Message: status.message, Hint: ""})
	}
	return checks
}

func preflightEnvironmentStatus(dir string) preflightStatus {
	files, err := envfile.Files(dir)
	if err != nil {
		return preflightStatus{id: "environment", name: "Environment", message: fmt.Sprintf("unavailable: could not read .env* files: %v", err), status: CheckFail}
	}
	if len(files) == 0 {
		return preflightStatus{id: "environment", name: "Environment", message: "not configured: no .env* files found", status: CheckInfo}
	}
	return preflightStatus{id: "environment", name: "Environment", message: fmt.Sprintf("ready: %d .env file(s) will be copied", len(files)), status: CheckPass}
}

func preflightDependenciesStatuses(dir string, excluded ...string) []preflightStatus {
	detection, err := deps.Detect(dir)
	if err != nil {
		return []preflightStatus{{id: "dependencies", name: "Dependencies", message: fmt.Sprintf("unavailable: %v", err), status: CheckFail}}
	}

	statuses := []preflightStatus{preflightDependenciesStatus(detection)}
	modules, err := deps.DiscoverNestedModules(dir, excluded...)
	if err != nil {
		return append(statuses, preflightStatus{id: "nested_modules", name: "Nested modules", message: fmt.Sprintf("unavailable: %v", err), status: CheckFail})
	}
	for _, module := range modules {
		statuses = append(statuses, preflightStatus{id: "nested_module", name: "Nested module", message: fmt.Sprintf("%s (%s): skipped; not installed automatically.", module.Path, module.Manifest), status: CheckInfo})
	}
	return statuses
}

func preflightDependenciesStatus(detection deps.Detection) preflightStatus {
	if detection.Installer == nil {
		if detection.Python {
			return preflightStatus{id: "dependencies", name: "Dependencies", message: "manual setup: Python virtualenv activation required", status: CheckInfo}
		}
		return preflightStatus{id: "dependencies", name: "Dependencies", message: "not configured at repository root", status: CheckInfo}
	}
	installer := detection.Installer
	if err := deps.InstallerAvailable(installer); err != nil {
		return preflightStatus{id: "dependencies", name: "Dependencies", message: fmt.Sprintf("limited: %s detected but %s is not installed", installer.Manifest, installer.Binary), status: CheckWarn}
	}
	return preflightStatus{id: "dependencies", name: "Dependencies", message: fmt.Sprintf("ready: %s detected; will run %s %s", installer.Manifest, installer.Binary, joinArgs(installer.Args)), status: CheckPass}
}

func preflightConfigurationStatus(result config.LoadResult) preflightStatus {
	if result.Warning != "" {
		return preflightStatus{id: "configuration", name: "Configuration", message: "unavailable: " + result.Warning, status: CheckFail}
	}
	if result.Path == "" {
		return preflightStatus{id: "configuration", name: "Configuration", message: "not configured: .treeman.toml not found", status: CheckInfo}
	}
	return preflightStatus{id: "configuration", name: "Configuration", message: "ready: .treeman.toml loaded", status: CheckPass}
}

func preflightDatabaseStatus(mainRoot string, result config.LoadResult) preflightStatus {
	if result.Warning != "" {
		return preflightStatus{id: "database", name: "Database", message: "unavailable: configuration is invalid", status: CheckFail}
	}
	envKey := result.Config.DatabaseEnvKey()
	if envKey == "" {
		return preflightStatus{id: "database", name: "Database", message: "not configured: add [database] to .treeman.toml", status: CheckInfo}
	}
	probe, err := database.Probe(mainRoot, envKey, result.Config.DatabaseContainer())
	if err != nil {
		return preflightStatus{id: "database", name: "Database", message: fmt.Sprintf("limited: %v", err), status: CheckWarn}
	}
	if probe.Skipped {
		return preflightStatus{id: "database", name: "Database", message: fmt.Sprintf("not configured: no supported PostgreSQL URI found for %s", envKey), status: CheckInfo}
	}
	return preflightStatus{id: "database", name: "Database", message: fmt.Sprintf("ready: %s has a PostgreSQL URI", envKey), status: CheckPass}
}

func preflightHooksStatus(result config.LoadResult) preflightStatus {
	if result.Warning != "" {
		return preflightStatus{id: "hooks", name: "Hooks", message: "unavailable: configuration is invalid", status: CheckFail}
	}
	count := len(result.Config.PostCreateHooks())
	if count == 0 {
		return preflightStatus{id: "hooks", name: "Hooks", message: "not configured: no post-create hooks", status: CheckInfo}
	}
	return preflightStatus{id: "hooks", name: "Hooks", message: fmt.Sprintf("ready: %d post-create hook(s) will run", count), status: CheckPass}
}

func writePreflight(out interface{ Write([]byte) (int, error) }, render ui.Renderer, statuses []preflightStatus) {
	fmt.Fprintf(out, "\n%s\n\n", render.Title("COMPATIBILITY PREFLIGHT"))
	for _, status := range statuses {
		symbol, tone := diagnosticAppearance(status.status)
		fmt.Fprintf(out, "  %s  %-14s %s\n", render.Tone(tone, symbol), status.name, render.Tone(tone, status.message))
	}
}
