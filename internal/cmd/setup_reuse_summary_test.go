package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/terminal"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func stepNames(summary setupSummary) []string {
	var names []string
	for _, step := range summary.steps() {
		names = append(names, step.name)
	}
	return names
}

func TestSetupSummaryStepsWithoutReuseAreUnchanged(t *testing.T) {
	assert.Equal(t, []string{"Environment", "Dependencies", "Database", "Hooks"}, stepNames(setupSummary{}))
}

func TestSetupSummaryStepsPlaceWarmStartsBetweenEnvironmentAndDependencies(t *testing.T) {
	status := completedStatus("completed: 1 copied, 0 skipped")
	summary := setupSummary{reuse: &status}
	assert.Equal(t, []string{"Environment", "Warm starts", "Dependencies", "Database", "Hooks"}, stepNames(summary))
	step := summary.steps()[1]
	assert.Equal(t, "skip-deps", step.skipFlag)
	assert.Equal(t, status, step.status)
}

// runReuseSetup runs setup with only dependency steps enabled and an empty
// PATH, so no installer can run.
func runReuseSetup(t *testing.T, mainRoot, worktreePath string, cfg config.Config, skipDeps bool) (setupSummary, string) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	var output bytes.Buffer
	render := ui.NewRenderer(&output, terminal.Capabilities{})
	summary := runWorktreeSetup(&output, render, worktreeSetup{
		mainRoot: mainRoot, worktreePath: worktreePath, projectConfig: cfg,
		options: creationSetupOptions{skipEnv: true, skipDatabase: true, skipHooks: true, skipDeps: skipDeps},
	})
	printSetupSummary(&output, render, summary)
	return summary, ui.StripANSI(output.String())
}

func reuseConfig(paths ...string) config.Config {
	return config.Config{Deps: &config.DepsConfig{Reuse: paths}}
}

func TestRunWorktreeSetup_NoReuseConfiguredHasNoWarmStartsStep(t *testing.T) {
	for name, cfg := range map[string]config.Config{
		"no deps":     {},
		"no reuse":    {Deps: &config.DepsConfig{}},
		"empty reuse": reuseConfig(),
	} {
		t.Run(name, func(t *testing.T) {
			mainRoot, worktreePath := t.TempDir(), t.TempDir()
			require.NoError(t, os.Mkdir(filepath.Join(mainRoot, "node_modules"), 0o755))
			summary, text := runReuseSetup(t, mainRoot, worktreePath, cfg, false)
			assert.Nil(t, summary.reuse)
			assert.NotContains(t, stepNames(summary), "Warm starts")
			assert.NotContains(t, text, "Warm starts")
			assert.NoDirExists(t, filepath.Join(worktreePath, "node_modules"))
		})
	}
}

func TestRunWorktreeSetup_WarmStartsSkippedWhenRequested(t *testing.T) {
	mainRoot, worktreePath := t.TempDir(), t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(mainRoot, "node_modules"), 0o755))
	summary, text := runReuseSetup(t, mainRoot, worktreePath, reuseConfig("node_modules"), true)
	require.NotNil(t, summary.reuse)
	assert.Equal(t, skippedStatus("skipped (requested)"), *summary.reuse)
	assert.Equal(t, []string{"Environment", "Warm starts", "Dependencies", "Database", "Hooks"}, stepNames(summary))
	assert.Contains(t, text, "Warm starts")
	assert.NoDirExists(t, filepath.Join(worktreePath, "node_modules"))
}

func TestRunWorktreeSetup_WarmStartsSkippedInMainWorktree(t *testing.T) {
	mainRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(mainRoot, "node_modules", "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mainRoot, "node_modules", "pkg", "file"), []byte("main"), 0o644))
	alias := filepath.Join(t.TempDir(), "alias")
	require.NoError(t, os.Symlink(mainRoot, alias))

	for name, target := range map[string]string{"same path": mainRoot, "symlinked path": alias} {
		t.Run(name, func(t *testing.T) {
			summary, text := runReuseSetup(t, mainRoot, target, reuseConfig("node_modules"), false)
			require.NotNil(t, summary.reuse)
			assert.Equal(t, skippedStatus("skipped (main worktree)"), *summary.reuse)
			assert.NotContains(t, text, "overlap")
			assert.NotContains(t, text, "could not reuse")
			assert.NotContains(t, text, "Reused")
			assert.Equal(t, "main", readTestFile(t, filepath.Join(mainRoot, "node_modules", "pkg", "file")))
			entries, err := os.ReadDir(filepath.Join(mainRoot, "node_modules"))
			require.NoError(t, err)
			assert.Len(t, entries, 1)
		})
	}
}

func TestRunWorktreeSetup_WarmStartsCounts(t *testing.T) {
	t.Run("copied and skipped", func(t *testing.T) {
		mainRoot, worktreePath := t.TempDir(), t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(mainRoot, "node_modules"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(mainRoot, "existing"), 0o755))
		require.NoError(t, os.Mkdir(filepath.Join(worktreePath, "existing"), 0o755))
		summary, _ := runReuseSetup(t, mainRoot, worktreePath, reuseConfig("node_modules", "missing", "existing"), false)
		require.NotNil(t, summary.reuse)
		assert.Equal(t, completedStatus("completed: 1 copied, 2 skipped"), *summary.reuse)
	})
	t.Run("nothing copied", func(t *testing.T) {
		mainRoot, worktreePath := t.TempDir(), t.TempDir()
		summary, _ := runReuseSetup(t, mainRoot, worktreePath, reuseConfig("missing"), false)
		require.NotNil(t, summary.reuse)
		assert.Equal(t, skippedStatus("completed: 0 copied, 1 skipped"), *summary.reuse)
	})
	t.Run("failure", func(t *testing.T) {
		mainRoot, worktreePath := t.TempDir(), t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(mainRoot, "node_modules"), 0o755))
		require.NoError(t, os.Mkdir(filepath.Join(mainRoot, "broken"), 0o755))
		require.NoError(t, unix.Mkfifo(filepath.Join(mainRoot, "broken", "fifo"), 0o644))
		summary, text := runReuseSetup(t, mainRoot, worktreePath, reuseConfig("node_modules", "broken", "missing"), false)
		require.NotNil(t, summary.reuse)
		assert.Equal(t, failedStatus("completed: 1 copied, 1 skipped, 1 failed"), *summary.reuse)
		assert.Contains(t, text, "could not reuse broken")
		assert.NoDirExists(t, filepath.Join(worktreePath, "broken"))
		assert.DirExists(t, filepath.Join(worktreePath, "node_modules"))
		assert.Contains(t, summary.failures(), "warm starts completed: 1 copied, 1 skipped, 1 failed")
	})
}

func TestPrintSetupSummary_WarmStartsLineOrder(t *testing.T) {
	mainRoot, worktreePath := t.TempDir(), t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(mainRoot, "node_modules"), 0o755))
	_, text := runReuseSetup(t, mainRoot, worktreePath, reuseConfig("node_modules"), false)
	summaryText := text[strings.Index(text, "SETUP"):]
	env := strings.Index(summaryText, "Environment")
	warm := strings.Index(summaryText, "Warm starts")
	depsLine := strings.Index(summaryText, "Dependencies")
	require.True(t, env >= 0 && warm >= 0 && depsLine >= 0, summaryText)
	assert.Less(t, env, warm)
	assert.Less(t, warm, depsLine)
	assert.Contains(t, summaryText, "completed: 1 copied, 0 skipped")
}

func TestFailedSetupFlagsNamesSkipDepsOnce(t *testing.T) {
	warm := failedStatus("completed: 0 copied, 0 skipped, 1 failed")
	summary := setupSummary{
		environment:  failedStatus("failed"),
		reuse:        &warm,
		dependencies: failedStatus("failed"),
		database:     failedStatus("failed"),
		hooks:        failedStatus("failed"),
	}
	assert.Equal(t, []string{"--skip-env", "--skip-deps", "--skip-database", "--skip-hooks"}, failedSetupFlags(summary))

	summary = setupSummary{reuse: &warm, dependencies: failedStatus("failed"), hooks: failedStatus("failed")}
	assert.Equal(t, []string{"--skip-deps", "--skip-hooks"}, failedSetupFlags(summary))

	summary = setupSummary{reuse: &warm, database: failedStatus("failed")}
	assert.Equal(t, []string{"--skip-deps", "--skip-database"}, failedSetupFlags(summary))

	ok := completedStatus("completed: 1 copied, 0 skipped")
	summary = setupSummary{reuse: &ok, dependencies: failedStatus("failed")}
	assert.Equal(t, []string{"--skip-deps"}, failedSetupFlags(summary))

	assert.Empty(t, failedSetupFlags(setupSummary{reuse: &ok}))
}
