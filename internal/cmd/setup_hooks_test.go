package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/shoutcape/treeman/internal/hooks"
	"github.com/shoutcape/treeman/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupHookRepo configures one post-create hook that leaves a file behind, so
// a test can tell whether it ran and where.
func setupHookRepo(t *testing.T, command string) (string, string) {
	t.Helper()
	repo, worktree := setupTestRepoWithTools(t, "feature/hooks", "sh", "touch", "false")
	configureHookApprovalRepo(t, repo, command)
	chdirForTest(t, worktree)
	return repo, worktree
}

func hookMarker() string { return "touch hook-ran" }

// saveHookApproval grants the approval a rerun looks for, in the same scope
// creation would have written.
func saveHookApproval(t *testing.T, repo string, commands []string) {
	t.Helper()
	commonDir := filepath.Join(repo, ".git")
	scope, err := hooks.NewApprovalScope(commonDir, filepath.Join(repo, ".treeman.toml"), hooks.PostCreatePhase, commands)
	require.NoError(t, err)
	store, err := state.NewHookApprovalStore(commonDir)
	require.NoError(t, err)
	require.NoError(t, store.Approve(scope))
}

func hookRerunOptions() rerunSetupOptions {
	return rerunSetupOptions{skipEnv: true, skipDatabase: true, skipDeps: true, rerunHooks: true}
}

func TestSetupSkipsHooksByDefaultDespiteSavedApproval(t *testing.T) {
	repo, worktree := setupHookRepo(t, hookMarker())
	saveHookApproval(t, repo, []string{hookMarker()})

	_, stderr, err := runSetupIn(t, "", rerunSetupOptions{skipEnv: true, skipDatabase: true, skipDeps: true})
	require.NoError(t, err)

	assert.NoFileExists(t, filepath.Join(worktree, "hook-ran"))
	assert.Contains(t, stderr, "Hooks")
	assert.Contains(t, stderr, "skipped")
}

func TestSetupRunsHooksWithSavedApproval(t *testing.T) {
	repo, worktree := setupHookRepo(t, hookMarker())
	saveHookApproval(t, repo, []string{hookMarker()})

	_, stderr, err := runSetupIn(t, "", hookRerunOptions())
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(worktree, "hook-ran"))
	assert.Contains(t, stderr, "Ran: "+hookMarker())
}

func TestSetupRunsHooksInTheSelectedWorktree(t *testing.T) {
	repo, worktree := setupHookRepo(t, hookMarker())
	saveHookApproval(t, repo, []string{hookMarker()})
	chdirForTest(t, repo)

	_, _, err := runSetupIn(t, "feature/hooks", hookRerunOptions())
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(worktree, "hook-ran"))
	assert.NoFileExists(t, filepath.Join(repo, "hook-ran"))
}

func TestSetupPromptsForHookApprovalWhenNoneIsSaved(t *testing.T) {
	_, worktree := setupHookRepo(t, hookMarker())
	stderr := &bytes.Buffer{}

	err := runSetup(interactiveApprovalCommandTo("y\n", &bytes.Buffer{}, stderr), "", hookRerunOptions())
	require.NoError(t, err)

	assert.Contains(t, stderr.String(), "existing worktree")
	assert.FileExists(t, filepath.Join(worktree, "hook-ran"))
}

func TestSetupRefusesHooksWhenTheSessionCannotAnswer(t *testing.T) {
	_, worktree := setupHookRepo(t, hookMarker())

	_, _, err := runSetupIn(t, "", hookRerunOptions())

	require.ErrorContains(t, err, "hook approval required")
	assert.ErrorContains(t, err, "--trust-hooks")
	assert.ErrorContains(t, err, "--skip-hooks")
	assert.NoFileExists(t, filepath.Join(worktree, "hook-ran"))
}

func TestSetupTrustAuthorizesOneInvocationAndSavesNothing(t *testing.T) {
	repo, worktree := setupHookRepo(t, hookMarker())
	options := hookRerunOptions()
	options.trustHooks = true

	_, _, err := runSetupIn(t, "", options)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(worktree, "hook-ran"))

	require.NoError(t, os.Remove(filepath.Join(worktree, "hook-ran")))
	store, err := state.NewHookApprovalStore(filepath.Join(repo, ".git"))
	require.NoError(t, err)
	records, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, records, "trusting one invocation must not persist an approval")

	// The next run has nothing to fall back on.
	_, _, err = runSetupIn(t, "", hookRerunOptions())
	assert.ErrorContains(t, err, "hook approval required")
}

func TestSetupApprovalDoesNotCoverAnEditedHookCommand(t *testing.T) {
	repo, worktree := setupHookRepo(t, hookMarker())
	saveHookApproval(t, repo, []string{hookMarker()})
	configureHookApprovalRepo(t, repo, "touch hook-edited")

	_, _, err := runSetupIn(t, "", hookRerunOptions())

	assert.ErrorContains(t, err, "hook approval required")
	assert.NoFileExists(t, filepath.Join(worktree, "hook-edited"))
}

func TestSetupHookFailureIsWarningOnly(t *testing.T) {
	repo, _ := setupHookRepo(t, "false")
	saveHookApproval(t, repo, []string{"false"})

	_, stderr, err := runSetupIn(t, "", hookRerunOptions())
	require.NoError(t, err)

	assert.Contains(t, stderr, `hook "false" failed`)
}
