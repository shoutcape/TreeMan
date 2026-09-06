package cmd

import (
	"fmt"
	"io"

	"github.com/shoutcape/treeman/internal/database"
	"github.com/shoutcape/treeman/internal/ui"
)

// setupWorktreeDatabase provisions the branch database, or, on a rerun,
// verifies the one this worktree already owns.
func setupWorktreeDatabase(out io.Writer, render ui.Renderer, setup worktreeSetup) setupStatus {
	envKey := setup.projectConfig.DatabaseEnvKey()
	if envKey == "" {
		return setupStatus{
			text:    "Not configured. Configure database",
			kind:    setupStatusSkipped,
			linkURL: databaseDocsURL,
		}
	}
	result, err := database.Setup(database.SetupOptions{
		WorktreePath:        setup.worktreePath,
		Branch:              setup.branch,
		EnvKey:              envKey,
		ConfiguredContainer: setup.projectConfig.DatabaseContainer(),
		Rerun:               setup.environment != envReplace,
	})
	switch {
	case err != nil:
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("database setup failed: %v", err)))
		return failedStatus(fmt.Sprintf("failed: %v", err))
	case result.Skipped:
		return skippedStatus(fmt.Sprintf("skipped (no PostgreSQL URI found for %s)", envKey))
	case result.Reused:
		fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Verified database "+result.DBName+"."))
		return completedStatus(fmt.Sprintf("completed: reused %s", result.DBName))
	default:
		fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Created database "+result.DBName))
		return completedStatus(fmt.Sprintf("completed: created %s", result.DBName))
	}
}
