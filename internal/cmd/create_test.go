package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
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
		environment:  setupStatus{description: "completed: copied 2 file(s)"},
		dependencies: setupStatus{description: "skipped"},
		database:     setupStatus{description: "failed: Docker is unavailable"},
		hooks:        setupStatus{description: "completed: 1 succeeded, 1 failed: \"make seed\": exit status 1"},
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
		environment:  setupStatus{description: "skipped (no environment files found)"},
		dependencies: setupStatus{description: "skipped"},
		database:     setupStatus{description: "skipped (database management not configured)"},
		hooks:        setupStatus{description: "skipped (no post-create hooks configured)"},
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

func TestRunCreateReportsUnsupportedDependencyManifests(t *testing.T) {
	repo, _ := createMergedCleanWorktree(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "Cargo.toml"), []byte("[package]\nname = \"example\"\n"), 0o644))
	runGitInDir(t, repo, "add", "Cargo.toml")
	runGitInDir(t, repo, "commit", "-m", "add cargo manifest")
	runGitInDir(t, repo, "push", "origin", "main")
	changeToDir(t, repo)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	cmd := commandWithOutput(stdout, stderr)
	require.NoError(t, runCreate(cmd, "cargo-project", creationSetupOptions{
		skipEnv: true, skipDatabase: true, skipHooks: true,
	}))

	output := ui.StripANSI(stderr.String())
	assert.Contains(t, output, "Dependencies   not bootstrapped: Cargo.toml")
	assert.Contains(t, output, "Unsupported dependency manifests were not bootstrapped: Cargo.toml")
	assert.NotContains(t, output, "No known dependency file detected")
	assert.Contains(t, output, "Worktree setup incomplete:")
	assert.NotContains(t, output, "Worktree ready:")
}
