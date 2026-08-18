package cmd

import (
	"bytes"
	"errors"
	"testing"

	"github.com/shoutcape/treeman/internal/hooks"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/stretchr/testify/assert"
)

func TestPrintSetupSummary(t *testing.T) {
	var output bytes.Buffer

	printSetupSummary(&output, setupSummary{
		environment:  "completed: copied 2 file(s)",
		dependencies: "skipped",
		database:     "failed: Docker is unavailable",
		hooks:        "completed: 1 succeeded, 1 failed: \"make seed\": exit status 1",
	})

	assert.Equal(t, "SETUP\n"+
		"  ✓  Environment    completed: copied 2 file(s)\n"+
		"  ○  Dependencies   skipped\n"+
		"  ✗  Database       failed: Docker is unavailable\n"+
		"  ✗  Hooks          completed: 1 succeeded, 1 failed: \"make seed\": exit status 1\n", ui.StripANSI(output.String()))
}

func TestSummarizeHooks(t *testing.T) {
	status := summarizeHooks([]hooks.RunResult{
		{Command: "make build"},
		{Command: "make seed", Err: errors.New("exit status 1")},
	})

	assert.Equal(t, "completed: 1 succeeded, 1 failed: \"make seed\": exit status 1", status)
}

func TestSummarizeHooks_AllSucceed(t *testing.T) {
	status := summarizeHooks([]hooks.RunResult{{Command: "make build"}})

	assert.Equal(t, "completed: 1 succeeded", status)
}

func TestPrintSetupSummary_DatabaseSkippedIncludesConfigurationLink(t *testing.T) {
	var output bytes.Buffer

	printSetupSummary(&output, setupSummary{
		environment:  "skipped (no environment files found)",
		dependencies: "skipped",
		database:     "skipped (database management not configured)",
		hooks:        "skipped (no post-create hooks configured)",
		databaseDocs: true,
	})

	assert.Contains(t, ui.StripANSI(output.String()), "  →  Configure      PostgreSQL setup guide\n")
}
