package cmd

import (
	"bytes"
	"errors"
	"testing"

	"github.com/shoutcape/treeman/internal/hooks"
	"github.com/shoutcape/treeman/internal/terminal"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrintSetupSummary(t *testing.T) {
	var output bytes.Buffer

	printSetupSummary(&output, ui.NewRenderer(&output, terminal.Capabilities{}), setupSummary{
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

	printSetupSummary(&output, ui.NewRenderer(&output, terminal.Capabilities{}), setupSummary{
		environment:  "skipped (no environment files found)",
		dependencies: "skipped",
		database:     "skipped (database management not configured)",
		hooks:        "skipped (no post-create hooks configured)",
		databaseDocs: true,
	})

	assert.Contains(t, ui.StripANSI(output.String()), "  ○  Database       Not configured. Configure database\n")
}

func TestRunCreateKeepsCapturedStatusOffPathStdout(t *testing.T) {
	repo, _ := createMergedCleanWorktree(t)
	changeToDir(t, repo)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	cmd := commandWithOutput(stdout, stderr)
	require.NoError(t, runCreate(cmd, "captured-output", creationSetupOptions{
		skipEnv: true, skipDatabase: true, skipDeps: true, skipHooks: true,
	}))

	assert.Equal(t, repo+"/.worktrees/captured-output\n", stdout.String())
	assert.NotContains(t, stdout.String(), "\x1b")
	assert.NotContains(t, stderr.String(), "\x1b")
	assert.Contains(t, stderr.String(), "Worktree ready:")
}
