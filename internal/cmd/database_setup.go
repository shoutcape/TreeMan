package cmd

import (
	"fmt"
	"io"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/database"
	"github.com/shoutcape/treeman/internal/ui"
)

func setupCreatedDatabase(out io.Writer, render ui.Renderer, cfg config.Config, worktreePath, branch string) string {
	envKey := cfg.DatabaseEnvKey()
	if envKey == "" {
		return "skipped (database management not configured)"
	}
	result, err := database.SetupBranchDB(worktreePath, branch, envKey, cfg.DatabaseContainer())
	switch {
	case err != nil:
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("database setup failed: %v", err)))
		return fmt.Sprintf("failed: %v", err)
	case result.Skipped:
		return fmt.Sprintf("skipped (no PostgreSQL URI found for %s)", envKey)
	default:
		fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Created database "+result.DBName))
		return fmt.Sprintf("completed: created %s", result.DBName)
	}
}
