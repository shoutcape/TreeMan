package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/shoutcape/treeman/internal/git"

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

// Merge status is not the question here -- `treeman clean` is the merge-aware
// command, and it has exact forge evidence where this would only have local
// ancestry. Survival is: a pushed branch leaves its commits on the remote, so
// an unmerged one the user named and confirmed is deleted.
func TestDeleteRemovesACleanPushedUnmergedTreebranch(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/unmerged")
	chdirForTest(t, repo)
	gitTest(t, worktree, "commit", "--allow-empty", "-m", "work nobody merged")
	gitTest(t, worktree, "push", "-u", "origin", "feature/unmerged")

	require.NoError(t, deleteWorktree(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/unmerged", repo, false))

	assert.NoDirExists(t, worktree)
	gitTestFails(t, repo, "show-ref", "--verify", "refs/heads/feature/unmerged")
}

// Committing work does not make it safe. Deleting the branch drops its reflog
// along with the worktree's, so a commit no remote and not the default branch
// can reach has nothing left pointing at it -- the same unrecoverable loss the
// dirty check refuses, one commit later.
func TestDeleteRefusesCommitsThatExistNowhereElse(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/unpushed")
	chdirForTest(t, repo)
	gitTest(t, worktree, "commit", "--allow-empty", "-m", "work only this branch has")

	err := deleteWorktree(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/unpushed", repo, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "has 1 commit on no remote and not on main")
	assert.Contains(t, err.Error(), "--force")
	assert.DirExists(t, worktree)
	gitTest(t, repo, "show-ref", "--verify", "refs/heads/feature/unpushed")

	require.NoError(t, deleteWorktree(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/unpushed", repo, true))
	assert.NoDirExists(t, worktree)
	gitTestFails(t, repo, "show-ref", "--verify", "refs/heads/feature/unpushed")
}

// Pushing once is not pushing. The commits added since the last push exist
// only here, and they are counted on their own.
func TestDeleteRefusesCommitsAddedAfterThePush(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/ahead")
	chdirForTest(t, repo)
	gitTest(t, worktree, "commit", "--allow-empty", "-m", "pushed")
	gitTest(t, worktree, "push", "-u", "origin", "feature/ahead")
	gitTest(t, worktree, "commit", "--allow-empty", "-m", "after the push")
	gitTest(t, worktree, "commit", "--allow-empty", "-m", "after that")

	err := deleteWorktree(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/ahead", repo, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "has 2 commits on no remote and not on main")
	assert.DirExists(t, worktree)
}

// The default branch is the other place work survives. A repository that
// pushes nowhere still deletes a branch whose commits main already reaches,
// so the gate does not make local-only repositories force every deletion.
func TestDeleteRemovesABranchMergedIntoALocalOnlyDefaultBranch(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/local-merge")
	chdirForTest(t, repo)
	gitTest(t, worktree, "commit", "--allow-empty", "-m", "work")
	gitTest(t, repo, "merge", "--no-ff", "-m", "merge feature/local-merge", "feature/local-merge")
	gitTest(t, repo, "remote", "remove", "origin")

	require.NoError(t, deleteWorktree(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/local-merge", repo, false))

	assert.NoDirExists(t, worktree)
	gitTestFails(t, repo, "show-ref", "--verify", "refs/heads/feature/local-merge")
}

// `clean` asks the forge whether the PR merged, which sees the squash merge
// local history cannot. That evidence outranks this gate, so a verified
// deletion is not sent back to --force over commits the remote already took.
func TestDeleteOfAVerifiedBranchIgnoresUnpushedCommits(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/verified")
	chdirForTest(t, repo)
	gitTest(t, worktree, "commit", "--allow-empty", "-m", "squashed upstream")
	verifiedSHA := gitRevParse(t, repo, "refs/heads/feature/verified")

	require.NoError(t, deleteVerifiedWorktree(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/verified", repo, false, verifiedSHA))

	assert.NoDirExists(t, worktree)
	gitTestFails(t, repo, "show-ref", "--verify", "refs/heads/feature/verified")
}

func TestDeleteRefusesADirtyTreebranchWithoutForce(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/dirty")
	chdirForTest(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "scratch.txt"), []byte("unsaved"), 0o644))

	err := deleteWorktree(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/dirty", repo, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "uncommitted or untracked changes")
	assert.DirExists(t, worktree)

	require.NoError(t, deleteWorktree(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/dirty", repo, true))
	assert.NoDirExists(t, worktree)
}

// The deletion decides on local state alone. An unreachable remote is the
// cheapest proof that nothing on this path talks to one.
func TestDeleteTouchesNoRemote(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/offline")
	chdirForTest(t, repo)
	gitTest(t, worktree, "commit", "--allow-empty", "-m", "work")
	gitTest(t, worktree, "push", "-u", "origin", "feature/offline")
	gitTest(t, repo, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))

	var stderr bytes.Buffer
	require.NoError(t, deleteWorktree(commandWithOutput(&bytes.Buffer{}, &stderr), worktree, "feature/offline", repo, false))

	assert.NoDirExists(t, worktree)
	gitTestFails(t, repo, "show-ref", "--verify", "refs/heads/feature/offline")
}

// The default-branch guard must not be the reason a repository cannot delete
// anything. Without origin/HEAD and without a remote named origin, the main
// worktree still names the branch worth protecting.
func TestDeleteFallsBackToTheMainWorktreeBranchWithoutOrigin(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/no-origin")
	chdirForTest(t, repo)
	gitTest(t, repo, "update-ref", "-d", "refs/remotes/origin/HEAD")
	gitTest(t, repo, "remote", "rename", "origin", "upstream")

	require.NoError(t, deleteWorktree(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/no-origin", repo, false))
	assert.NoDirExists(t, worktree)

}

// resolveDefaultBranch prefers origin/HEAD and falls back to the main
// worktree's branch, so the guard never depends on reaching a remote.
func TestResolveDefaultBranchFallsBackToTheMainWorktree(t *testing.T) {
	repo, _ := createTestWorktree(t, "feature/resolve")
	chdirForTest(t, repo)
	entries, err := git.WorktreeList()
	require.NoError(t, err)

	branch, err := resolveDefaultBranch(entries, repo)
	require.NoError(t, err)
	assert.Equal(t, "main", branch)

	// With origin/HEAD gone and no remote named origin, the main worktree is
	// the only thing left that names the branch to protect.
	gitTest(t, repo, "update-ref", "-d", "refs/remotes/origin/HEAD")
	gitTest(t, repo, "remote", "rename", "origin", "upstream")

	branch, err = resolveDefaultBranch(entries, repo)
	require.NoError(t, err)
	assert.Equal(t, "main", branch)
}

// The prompt is where unmerged work gets reported, since the deletion itself
// no longer refuses it.
func TestDeleteConfirmationReportsUnmergedCommits(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/counted")
	chdirForTest(t, repo)
	gitTest(t, worktree, "commit", "--allow-empty", "-m", "one")
	gitTest(t, worktree, "commit", "--allow-empty", "-m", "two")

	var stderr bytes.Buffer
	printDeleteConfirmation(commandWithOutput(&bytes.Buffer{}, &stderr), repo, worktree, "feature/counted")

	assert.Contains(t, stderr.String(), "(2 commits not on main)")
}

func TestDeleteConfirmationOmitsCountForAMergedBranch(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/merged")
	chdirForTest(t, repo)

	var stderr bytes.Buffer
	printDeleteConfirmation(commandWithOutput(&bytes.Buffer{}, &stderr), repo, worktree, "feature/merged")

	assert.NotContains(t, stderr.String(), "not on")
}

// A single commit reads as a commit, and the annotation never blocks the
// prompt when the default branch cannot be worked out at all.
func TestDeleteConfirmationSurvivesAnUnresolvableDefaultBranch(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/lonely")
	chdirForTest(t, repo)
	gitTest(t, worktree, "commit", "--allow-empty", "-m", "one")

	var withDefault bytes.Buffer
	printDeleteConfirmation(commandWithOutput(&bytes.Buffer{}, &withDefault), repo, worktree, "feature/lonely")
	assert.Contains(t, withDefault.String(), "(1 commit not on main)")

	gitTest(t, repo, "update-ref", "-d", "refs/remotes/origin/HEAD")
	gitTest(t, repo, "remote", "remove", "origin")
	gitTest(t, repo, "checkout", "--detach")

	var stderr bytes.Buffer
	printDeleteConfirmation(commandWithOutput(&bytes.Buffer{}, &stderr), repo, worktree, "feature/lonely")
	assert.Contains(t, stderr.String(), "feature/lonely")
	assert.NotContains(t, stderr.String(), "not on")
}

// A lock is the user asking for this worktree to survive, including when its
// directory is temporarily absent. It outranks --force.
func TestDeleteRefusesALockedWorktree(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/locked")
	chdirForTest(t, repo)
	gitTest(t, repo, "worktree", "lock", "--reason", "on a removable disk", worktree)

	for _, force := range []bool{false, true} {
		err := deleteWorktree(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/locked", repo, force)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "is locked")
		assert.Contains(t, err.Error(), "on a removable disk")
		assert.Contains(t, err.Error(), "worktree unlock")
		assert.DirExists(t, worktree)
	}
	gitTest(t, repo, "show-ref", "--verify", "refs/heads/feature/locked")
}

// A worktree directory removed outside TreeMan used to be a dead end: delete
// refused to inspect it and clean skipped it, so the registration and the
// branch survived with nothing able to remove them.
func TestDeleteCleansUpAWorktreeWhoseDirectoryIsGone(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/vanished")
	chdirForTest(t, repo)
	require.NoError(t, os.RemoveAll(worktree))

	require.NoError(t, deleteWorktree(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/vanished", repo, false))

	gitTestFails(t, repo, "show-ref", "--verify", "refs/heads/feature/vanished")
	assert.NotContains(t, gitTestOutput(t, repo, "worktree", "list", "--porcelain"), "feature/vanished")
}

// The stale path reaches the same compare-and-delete as a normal deletion, so
// a branch that moved since is still retained.
func TestDeleteOfAVanishedWorktreeStillRefusesAMovedBranch(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/vanished-moved")
	expectedSHA := gitRevParse(t, repo, "refs/heads/feature/vanished-moved")
	chdirForTest(t, repo)
	require.NoError(t, os.RemoveAll(worktree))
	movedSHA := advanceMainForTest(t, repo)
	gitTest(t, repo, "update-ref", "refs/heads/feature/vanished-moved", movedSHA)

	err := deleteVerifiedWorktree(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/vanished-moved", repo, false, expectedSHA)

	require.Error(t, err)
	assert.Equal(t, movedSHA, gitRevParse(t, repo, "refs/heads/feature/vanished-moved"))
}
