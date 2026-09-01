package cmd

import (
	"bytes"
	"io"
	"path/filepath"
	"testing"

	"github.com/shoutcape/treeman/internal/terminal"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPickerSelectionIndex_DuplicateDisplayRows(t *testing.T) {
	display := ui.NewRenderer(io.Discard, terminal.Capabilities{}).WorktreeRow("/home/user/repo.feature-a", "feature/a")
	first := pickerRow(display, 0)
	second := pickerRow(display, 1)

	assert.Equal(t, 0, pickerSelectionIndex(first, 2))
	assert.Equal(t, 1, pickerSelectionIndex(second, 2))
}

func TestPickerSelectionIndex_InvalidIdentity(t *testing.T) {
	assert.Equal(t, -1, pickerSelectionIndex("worktree", 2))
	assert.Equal(t, -1, pickerSelectionIndex("worktree\t2", 2))
	assert.Equal(t, -1, pickerSelectionIndex("worktree\tnot-a-number", 2))
}

func TestPickerArgs_PreservePrompt(t *testing.T) {
	colorArgs := pickerArgs(true, " worktrees ", "switch > ")
	assert.Contains(t, colorArgs, "--prompt=switch > ")
	assert.Contains(t, colorArgs, "--ansi")
	assert.Contains(t, colorArgs, "--color="+ui.FZFColors())
	assert.Contains(t, colorArgs, "--height=40%")
	assert.Contains(t, colorArgs, "--layout=reverse")

	plainArgs := pickerArgs(false, " worktrees ", "switch > ")
	assert.Contains(t, plainArgs, "--no-color")
	assert.NotContains(t, plainArgs, "--ansi")
	assert.NotContains(t, plainArgs, "--color="+ui.FZFColors())
}

// A branch whose commits are on the remote is safe to delete, but the merge
// check reads a remote-tracking ref that only a fetch keeps current. These
// tests describe what the deletion decides on.
func TestDeleteFetchesTheUpstreamBeforeDecidingItIsUnmerged(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/pushed")
	chdirForTest(t, repo)
	gitTest(t, worktree, "commit", "--allow-empty", "-m", "work")
	gitTest(t, worktree, "push", "-u", "origin", "feature/pushed")

	// The work is on the remote, but this checkout has not seen it there since:
	// the same state a machine is in after someone else pushes, or after
	// pushing from elsewhere.
	gitTest(t, repo, "update-ref", "-d", "refs/remotes/origin/feature/pushed")

	var stderr bytes.Buffer
	require.NoError(t, deleteWorktree(commandWithOutput(&bytes.Buffer{}, &stderr), worktree, "feature/pushed", repo, false))

	assert.NoDirExists(t, worktree)
	gitTestFails(t, repo, "show-ref", "--verify", "refs/heads/feature/pushed")
	assert.NotContains(t, stderr.String(), "could not refresh")
}

func TestDeleteWarnsAndUsesLocalStateWhenTheFetchFails(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/offline")
	chdirForTest(t, repo)
	gitTest(t, worktree, "commit", "--allow-empty", "-m", "work")
	gitTest(t, worktree, "push", "-u", "origin", "feature/offline")
	// Default-branch detection reads this ref before asking the remote, so
	// setting it keeps the deletion offline apart from the fetch under test.
	gitTest(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	// An unreachable remote stands in for being offline.
	gitTest(t, repo, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))

	var stderr bytes.Buffer
	require.NoError(t, deleteWorktree(commandWithOutput(&bytes.Buffer{}, &stderr), worktree, "feature/offline", repo, false))

	// The last fetched state still contains the branch tip, so the deletion
	// proceeds -- but it says which state it decided on.
	assert.Contains(t, stderr.String(), `could not refresh branch "feature/offline" from its remote`)
	assert.NoDirExists(t, worktree)
	gitTestFails(t, repo, "show-ref", "--verify", "refs/heads/feature/offline")
}

func TestDeleteSkipsTheFetchWhenItDoesNotDecide(t *testing.T) {
	previous := fetchUpstreamBranch
	fetches := 0
	fetchUpstreamBranch = func(dir, branch string) (bool, error) {
		fetches++
		return previous(dir, branch)
	}
	t.Cleanup(func() { fetchUpstreamBranch = previous })

	// --force deletes without a merge check, and an untracked branch has no
	// remote state to read, so neither pays for a network round trip.
	forced, forcedWorktree := createTestWorktree(t, "feature/forced")
	chdirForTest(t, forced)
	gitTest(t, forcedWorktree, "commit", "--allow-empty", "-m", "unmerged")
	require.NoError(t, deleteWorktree(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), forcedWorktree, "feature/forced", forced, true))
	assert.Equal(t, 0, fetches)

	local, localWorktree := createTestWorktree(t, "feature/local")
	chdirForTest(t, local)
	require.NoError(t, deleteWorktree(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), localWorktree, "feature/local", local, false))
	tracked, err := previous(local, "feature/local")
	require.NoError(t, err)
	assert.False(t, tracked, "an untracked branch has no upstream to refresh")
	assert.Equal(t, 1, fetches)
}
