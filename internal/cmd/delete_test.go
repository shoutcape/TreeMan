package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/shoutcape/treeman/internal/git"
	"github.com/spf13/cobra"

	"github.com/shoutcape/treeman/internal/terminal"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deleteForTest is the delete command's own flow with the prompt left out:
// plan, then carry the plan out. Tests that exercise the prompt call
// planDeletion and printDeleteConfirmation directly.
func deleteForTest(t *testing.T, cmd *cobra.Command, dest, branch, mainRoot string, force bool) error {
	t.Helper()
	plan, err := planDeletion(dest, branch, mainRoot, deletionGuards{force: force}, "")
	if err != nil {
		return err
	}
	return runDeletionPlan(cmd, plan)
}

// deleteVerifiedWorktreeForTest gives a verified deletion its own cleanup
// batch, the way `clean` gives it a shared one.
func deleteVerifiedWorktreeForTest(t *testing.T, cmd *cobra.Command, dest, branch, mainRoot, expectedSHA string) error {
	t.Helper()
	_, err := deleteVerifiedWorktree(cmd, dest, branch, mainRoot, expectedSHA, newCleanupBatch())
	return err
}

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

	require.NoError(t, deleteForTest(t, commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/unmerged", repo, false))

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

	err := deleteForTest(t, commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/unpushed", repo, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "has 1 commit on no remote and not on main")
	assert.Contains(t, err.Error(), "--force")
	assert.DirExists(t, worktree)
	gitTest(t, repo, "show-ref", "--verify", "refs/heads/feature/unpushed")

	require.NoError(t, deleteForTest(t, commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/unpushed", repo, true))
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

	err := deleteForTest(t, commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/ahead", repo, false)

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

	require.NoError(t, deleteForTest(t, commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/local-merge", repo, false))

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

	require.NoError(t, deleteVerifiedWorktreeForTest(t, commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/verified", repo, verifiedSHA))

	assert.NoDirExists(t, worktree)
	gitTestFails(t, repo, "show-ref", "--verify", "refs/heads/feature/verified")
}

func TestDeleteRefusesADirtyTreebranchWithoutForce(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/dirty")
	chdirForTest(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "scratch.txt"), []byte("unsaved"), 0o644))

	err := deleteForTest(t, commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/dirty", repo, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "uncommitted or untracked changes")
	assert.DirExists(t, worktree)

	require.NoError(t, deleteForTest(t, commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/dirty", repo, true))
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
	require.NoError(t, deleteForTest(t, commandWithOutput(&bytes.Buffer{}, &stderr), worktree, "feature/offline", repo, false))

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

	require.NoError(t, deleteForTest(t, commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/no-origin", repo, false))
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

// The prompt reports what --force waived, and nothing else: a plan that got
// this far cannot be refused, so the only work still about to be destroyed is
// the work the flag let through.
func TestDeleteConfirmationReportsCommitsThatExistNowhereElse(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/counted")
	chdirForTest(t, repo)
	gitTest(t, worktree, "commit", "--allow-empty", "-m", "one")
	gitTest(t, worktree, "commit", "--allow-empty", "-m", "two")

	plan, err := planDeletion(worktree, "feature/counted", repo, deletionGuards{force: true}, "")
	require.NoError(t, err)

	var stderr bytes.Buffer
	printDeleteConfirmation(commandWithOutput(&bytes.Buffer{}, &stderr), plan)

	assert.Contains(t, stderr.String(), "(2 commits on no remote and not on main)")
}

// A pushed branch loses nothing to the deletion, so the prompt says nothing
// about its commits. The count the prompt reports is the count the guards
// weighed -- there is no second estimate to disagree with.
func TestDeleteConfirmationStaysQuietWhenNothingIsLost(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/pushed")
	chdirForTest(t, repo)
	gitTest(t, worktree, "commit", "--allow-empty", "-m", "one")
	gitTest(t, worktree, "push", "-u", "origin", "feature/pushed")

	plan, err := planDeletion(worktree, "feature/pushed", repo, deletionGuards{}, "")
	require.NoError(t, err)

	var stderr bytes.Buffer
	printDeleteConfirmation(commandWithOutput(&bytes.Buffer{}, &stderr), plan)

	assert.Contains(t, stderr.String(), "feature/pushed")
	assert.NotContains(t, stderr.String(), "on no remote")
	assert.NotContains(t, stderr.String(), "Discards")
}

// The other thing --force waives is named too, so a forced deletion says both
// of the things it is about to destroy.
func TestDeleteConfirmationNamesDiscardedChanges(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/forced-dirty")
	chdirForTest(t, repo)
	gitTest(t, worktree, "push", "-u", "origin", "feature/forced-dirty")
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "scratch.txt"), []byte("unsaved"), 0o644))

	plan, err := planDeletion(worktree, "feature/forced-dirty", repo, deletionGuards{force: true}, "")
	require.NoError(t, err)

	var stderr bytes.Buffer
	printDeleteConfirmation(commandWithOutput(&bytes.Buffer{}, &stderr), plan)

	assert.Contains(t, stderr.String(), "Discards: uncommitted and untracked changes")
}

// Every refusal now happens before the prompt, so a deletion that cannot
// proceed says so instead of asking the user to confirm it first.
func TestPlanRefusesAnUnresolvableDefaultBranchBeforeAnyPrompt(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/lonely")
	chdirForTest(t, repo)
	gitTest(t, repo, "update-ref", "-d", "refs/remotes/origin/HEAD")
	gitTest(t, repo, "remote", "remove", "origin")
	gitTest(t, repo, "checkout", "--detach")

	_, err := planDeletion(worktree, "feature/lonely", repo, deletionGuards{}, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "default branch could not be detected")
}

// A prompt is human-sized. Work committed while it was open would otherwise be
// deleted under an answer given before it existed -- unpushed, the guards
// catch it on the way back through; pushed, the confirmed tip is what catches
// it.
func TestReplanRefusesABranchThatMovedWhileThePromptWasOpen(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/moved-at-prompt")
	chdirForTest(t, repo)
	gitTest(t, worktree, "push", "-u", "origin", "feature/moved-at-prompt")
	plan, err := planDeletion(worktree, "feature/moved-at-prompt", repo, deletionGuards{}, "")
	require.NoError(t, err)

	gitTest(t, worktree, "commit", "--allow-empty", "-m", "committed while the prompt waited")
	// Pushed, so the unreachable-commit guard has nothing to say and the only
	// thing left to catch the change is the tip the user actually confirmed.
	gitTest(t, worktree, "push", "origin", "feature/moved-at-prompt")

	_, err = replanAfterPrompt(plan)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "moved while the prompt was open")
	assert.DirExists(t, worktree)
	gitTest(t, repo, "show-ref", "--verify", "refs/heads/feature/moved-at-prompt")
}

// A lock is the user asking for this worktree to survive, including when its
// directory is temporarily absent. It outranks --force.
func TestDeleteRefusesALockedWorktree(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/locked")
	chdirForTest(t, repo)
	gitTest(t, repo, "worktree", "lock", "--reason", "on a removable disk", worktree)

	for _, force := range []bool{false, true} {
		err := deleteForTest(t, commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/locked", repo, force)
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

	require.NoError(t, deleteForTest(t, commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/vanished", repo, false))

	gitTestFails(t, repo, "show-ref", "--verify", "refs/heads/feature/vanished")
	assert.NotContains(t, gitTestOutput(t, repo, "worktree", "list", "--porcelain"), "feature/vanished")
}

// A vanished directory takes the working tree with it, not the branch. The
// commits are still the branch's, so the guard that protects them still runs.
func TestDeleteOfAVanishedWorktreeStillRefusesUnreachableCommits(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/vanished-unpushed")
	chdirForTest(t, repo)
	gitTest(t, worktree, "commit", "--allow-empty", "-m", "work only this branch has")
	require.NoError(t, os.RemoveAll(worktree))

	err := deleteForTest(t, commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/vanished-unpushed", repo, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "on no remote and not on main")
	gitTest(t, repo, "show-ref", "--verify", "refs/heads/feature/vanished-unpushed")

	require.NoError(t, deleteForTest(t, commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/vanished-unpushed", repo, true))
	gitTestFails(t, repo, "show-ref", "--verify", "refs/heads/feature/vanished-unpushed")
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

	err := deleteVerifiedWorktreeForTest(t, commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), worktree, "feature/vanished-moved", repo, expectedSHA)

	require.Error(t, err)
	assert.Equal(t, movedSHA, gitRevParse(t, repo, "refs/heads/feature/vanished-moved"))
}
