package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
		environment:  completedStatus("completed: copied 2 file(s)"),
		dependencies: skippedStatus("skipped"),
		database:     failedStatus("failed: Docker is unavailable"),
		hooks:        failedStatus("completed: 1 succeeded, 1 failed: \"make seed\": exit status 1"),
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

	assert.Equal(t, failedStatus("completed: 1 succeeded, 1 failed: \"make seed\": exit status 1"), status)
}

func TestSummarizeHooks_AllSucceed(t *testing.T) {
	status := summarizeHooks([]hooks.RunResult{{Command: "make build"}})

	assert.Equal(t, completedStatus("completed: 1 succeeded"), status)
}

func TestPrintSetupSummary_DatabaseSkippedIncludesConfigurationLink(t *testing.T) {
	var output bytes.Buffer

	printSetupSummary(&output, ui.NewRenderer(&output, terminal.Capabilities{}), setupSummary{
		environment:  skippedStatus("skipped (no environment files found)"),
		dependencies: skippedStatus("skipped"),
		database: setupStatus{
			text:    "Not configured. Configure database",
			kind:    setupStatusSkipped,
			linkURL: databaseDocsURL,
		},
		hooks: skippedStatus("skipped (no post-create hooks configured)"),
	})

	assert.Contains(t, ui.StripANSI(output.String()), "  ○  Database       Not configured. Configure database\n")
}

func TestPrintSetupSummary_DatabaseRequestedSkipDoesNotIncludeConfigurationLink(t *testing.T) {
	var output bytes.Buffer

	printSetupSummary(&output, ui.NewRenderer(&output, terminal.Capabilities{}), setupSummary{
		environment:  skippedStatus("skipped (requested)"),
		dependencies: skippedStatus("skipped (requested)"),
		database:     skippedStatus("skipped (requested)"),
		hooks:        skippedStatus("skipped (requested)"),
	})

	text := ui.StripANSI(output.String())
	assert.Contains(t, text, "  ○  Database       skipped (requested)\n")
	assert.NotContains(t, text, "Configure database")
}

func TestRunCreateKeepsCapturedStatusOffPathStdout(t *testing.T) {
	repo, _ := createMergedCleanWorktree(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".envrc"), []byte("use nix\n"), 0o644))
	module := filepath.Join(repo, "apps", "web", "package-lock.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(module), 0o755))
	require.NoError(t, os.WriteFile(module, []byte("{}\n"), 0o644))
	runGitInDir(t, repo, "add", "apps/web/package-lock.json")
	runGitInDir(t, repo, "commit", "-m", "add nested module")
	runGitInDir(t, repo, "push", "origin", "main")
	changeToDir(t, repo)
	pathWithOnlyGit(t)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	cmd := commandWithOutput(stdout, stderr)
	require.NoError(t, runCreate(cmd, "captured-output", creationSetupOptions{
		skipEnv: true, skipDatabase: true, skipHooks: true,
	}, worktreeLaunchOptions{}))

	assert.Equal(t, repo+"/.worktrees/captured-output\n", stdout.String())
	assert.NotContains(t, stdout.String(), "\x1b")
	assert.NotContains(t, stderr.String(), "\x1b")
	assert.Contains(t, stderr.String(), "direnv")
	assert.Contains(t, stderr.String(), "Nix")
	assert.Contains(t, stderr.String(), "Worktree ready:")
	assert.Contains(t, stderr.String(), "Nested module apps/web (package-lock.json): skipped; not installed automatically.")
	assert.NotContains(t, stderr.String(), "npm is not installed")
	assert.Equal(t, 3, strings.Count(stderr.String(), "skipped (requested)"))
	assert.NotContains(t, stderr.String(), "Skipped:")
}

func TestRunCreateSeparatesCollidingBranchSlugs(t *testing.T) {
	repo, _ := createMergedCleanWorktree(t)
	changeToDir(t, repo)
	pathWithOnlyGit(t)

	// Each step runs against the worktrees the earlier steps created, so the
	// order of the table is significant.
	tests := []struct {
		name   string
		branch string
		want   string
	}{
		{
			name:   "first branch keeps the plain slug",
			branch: "topic/login",
			want:   ".worktrees/topic-login",
		},
		{
			name:   "colliding branch gets a suffixed slug",
			branch: "topic-login",
			want:   ".worktrees/topic-login-25de00",
		},
		{
			name:   "branch that does not collide keeps the plain slug",
			branch: "fix/bug-123",
			want:   ".worktrees/fix-bug-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			require.NoError(t, runCreate(commandWithOutput(stdout, stderr), tt.branch, creationSetupOptions{
				skipEnv: true, skipDatabase: true, skipHooks: true,
			}, worktreeLaunchOptions{}))

			want := filepath.Join(repo, tt.want)
			assert.Equal(t, want+"\n", stdout.String())
			assert.DirExists(t, want)
		})
	}

	// switch resolves each branch back to the worktree that holds it.
	for _, tt := range tests {
		t.Run("switch "+tt.branch, func(t *testing.T) {
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			require.NoError(t, runSwitch(commandWithOutput(stdout, stderr), tt.branch, worktreeLaunchOptions{}))
			assert.Equal(t, filepath.Join(repo, tt.want)+"\n", stdout.String())
		})
	}
}

func TestRunCreateReportsAnOccupiedDirectory(t *testing.T) {
	repo, _ := createMergedCleanWorktree(t)
	changeToDir(t, repo)
	pathWithOnlyGit(t)
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".worktrees", "topic-login"), 0o755))

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	err := runCreate(commandWithOutput(stdout, stderr), "topic/login", creationSetupOptions{
		skipEnv: true, skipDatabase: true, skipHooks: true,
	}, worktreeLaunchOptions{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
	assert.Empty(t, stdout.String())
}
