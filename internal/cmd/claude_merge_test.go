package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func commitFile(t *testing.T, dir, name, content, message string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	gitTest(t, dir, "add", name)
	gitTest(t, dir, "commit", "-m", message)
}

// squashMerge squash-merges branch into the branch checked out in dir.
func squashMerge(t *testing.T, dir, branch string) {
	t.Helper()
	gitTest(t, dir, "merge", "--squash", branch)
	gitTest(t, dir, "commit", "-m", "squash "+branch)
}

func assertOwnedBranchDeleted(t *testing.T, repo, path, branch string) {
	t.Helper()
	result := claudeRemove(t, path)
	require.NoErrorf(t, result.err, "stderr: %s", result.stderr)
	assert.NoDirExists(t, path)
	assert.False(t, hasBranch(repo, branch), "a squash- or rebase-merged branch TreeMan created is deleted")
	assert.Empty(t, recordsForBranch(t, repo, branch))
}

func assertBranchKept(t *testing.T, repo, path, branch string) {
	t.Helper()
	tip := revParse(t, repo, branch)
	result := claudeRemove(t, path)
	require.NoErrorf(t, result.err, "stderr: %s", result.stderr)
	assert.NoDirExists(t, path)
	require.True(t, hasBranch(repo, branch), "the branch is kept")
	assert.Equal(t, tip, revParse(t, repo, branch))
}

func TestClaudeWorktreeRemoveDeletesAnOwnedSquashMergedBranch(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "squashed", repo)
	commitFile(t, path, "feature.txt", "one\n", "first")
	commitFile(t, path, "feature.txt", "one\ntwo\n", "second")
	commitFile(t, path, "other/new.txt", "new\n", "third")
	squashMerge(t, repo, "feature/squashed")

	assertOwnedBranchDeleted(t, repo, path, "feature/squashed")
}

func TestClaudeWorktreeRemoveDeletesAnOwnedSquashMergedBranchAfterLaterDefaultWork(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "squashlater", repo)
	commitFile(t, path, "feature.txt", "feature\n", "feature")
	commitFile(t, repo, "unrelated.txt", "before\n", "main work before the merge")
	squashMerge(t, repo, "feature/squashlater")
	commitFile(t, repo, "unrelated2.txt", "after\n", "main work after the merge")

	assertOwnedBranchDeleted(t, repo, path, "feature/squashlater")
}

func TestClaudeWorktreeRemoveDeletesAnOwnedRebaseMergedBranch(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "rebased", repo)
	commitFile(t, path, "a.txt", "a\n", "a")
	commitFile(t, path, "b.txt", "b\n", "b")
	commitFile(t, repo, "main.txt", "main\n", "main moves on")
	// A rebase merge replays the branch's commits onto the default branch.
	gitTest(t, repo, "cherry-pick", "main..feature/rebased")

	assertOwnedBranchDeleted(t, repo, path, "feature/rebased")
}

func TestClaudeWorktreeRemoveDeletesAnOwnedBranchSquashMergedOnOrigin(t *testing.T) {
	repo, origin := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "remote", repo)
	commitFile(t, path, "feature.txt", "feature\n", "feature")
	gitTest(t, path, "push", "origin", "feature/remote")
	// Someone else squash-merges on the forge; locally only origin/main
	// learns of it.
	other := filepath.Join(canonicalTestDir(t), "other")
	gitTest(t, filepath.Dir(other), "clone", origin, other)
	gitTest(t, other, "fetch", "origin", "feature/remote")
	squashMerge(t, other, "origin/feature/remote")
	gitTest(t, other, "push", "origin", "main")
	gitTest(t, repo, "fetch", "origin")
	require.NotEqual(t, revParse(t, repo, "main"), revParse(t, repo, "origin/main"))

	assertOwnedBranchDeleted(t, repo, path, "feature/remote")
}

func TestClaudeWorktreeRemoveKeepsAnOwnedBranchWithOnlyEmptyCommits(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "empty", repo)
	gitTest(t, path, "commit", "--allow-empty", "-m", "nothing")
	gitTest(t, path, "commit", "--allow-empty", "-m", "still nothing")
	commitFile(t, repo, "main.txt", "main\n", "main moves on")

	assertBranchKept(t, repo, path, "feature/empty")
}

func TestClaudeWorktreeRemoveKeepsAnOwnedBranchWhoseCommitsCancelOut(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "revert", repo)
	commitFile(t, path, "feature.txt", "feature\n", "add")
	gitTest(t, path, "revert", "--no-edit", "HEAD")

	assertBranchKept(t, repo, path, "feature/revert")
}

func TestClaudeWorktreeRemoveKeepsAnOwnedPartiallyMergedBranch(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "partial", repo)
	commitFile(t, path, "a.txt", "a\n", "a")
	commitFile(t, path, "b.txt", "b\n", "b")
	// Only the first change reached main.
	gitTest(t, repo, "cherry-pick", "feature/partial~1")

	assertBranchKept(t, repo, path, "feature/partial")
}

func TestClaudeWorktreeRemoveKeepsAnOwnedBranchThatConflicts(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "conflict", repo)
	commitFile(t, path, "README.md", "branch version\n", "branch edit")
	commitFile(t, repo, "README.md", "main version\n", "main edit")

	assertBranchKept(t, repo, path, "feature/conflict")
	assert.Equal(t, "main version\n", readFile(t, filepath.Join(repo, "README.md")), "the comparison leaves the main worktree alone")
	assert.Empty(t, gitTestOutput(t, repo, "status", "--porcelain"))
}

func TestClaudeWorktreeRemoveKeepsAnOwnedBranchWhoseSquashedChangeWasLaterEdited(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "edited", repo)
	commitFile(t, path, "feature.txt", "feature\n", "feature")
	squashMerge(t, repo, "feature/edited")
	commitFile(t, repo, "feature.txt", "changed on main\n", "main edits the merged file")

	// Merging again would conflict, so the rule cannot tell: keep.
	assertBranchKept(t, repo, path, "feature/edited")
}

func TestClaudeWorktreeRemoveKeepsAnUnmergedOwnedBranch(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "unmerged", repo)
	commitFile(t, path, "feature.txt", "feature\n", "feature")

	assertBranchKept(t, repo, path, "feature/unmerged")
}

func TestClaudeWorktreeRemoveKeepsAnUnownedSquashMergedBranch(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	userWorktree := filepath.Join(canonicalTestDir(t), "user-squash")
	gitTest(t, repo, "worktree", "add", "-b", "feature/user-squash", userWorktree)
	commitFile(t, userWorktree, "feature.txt", "feature\n", "feature")
	squashMerge(t, repo, "feature/user-squash")

	assertBranchKept(t, repo, userWorktree, "feature/user-squash")
}

func TestClaudeWorktreeRemoveKeepsAnAttachedSquashMergedBranch(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	userWorktree := filepath.Join(canonicalTestDir(t), "attach-src")
	gitTest(t, repo, "worktree", "add", "-b", "feature/attached", userWorktree)
	commitFile(t, userWorktree, "feature.txt", "feature\n", "feature")
	gitTest(t, repo, "worktree", "remove", userWorktree)
	squashMerge(t, repo, "feature/attached")

	path := mustClaudeCreate(t, "attached", repo)
	records := recordsForBranch(t, repo, "feature/attached")
	require.Len(t, records, 1)
	require.False(t, records[0].BranchCreated)

	assertBranchKept(t, repo, path, "feature/attached")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func TestClaudeWorktreeRemoveKeepsAnOwnedBranchWithACrissCrossHistory(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	commitFile(t, repo, "f", "v0\n", "A")
	gitTest(t, repo, "push", "origin", "main")
	path := mustClaudeCreate(t, "criss", repo)
	commitFile(t, repo, "m.txt", "m\n", "B1")
	b1 := revParse(t, repo, "HEAD")
	commitFile(t, path, "f", "v1\n", "B2")
	gitTest(t, repo, "merge", "--no-ff", "--no-edit", "feature/criss")
	gitTest(t, path, "merge", "--no-ff", "--no-edit", b1)
	commitFile(t, path, "f", "v0\n", "back to v0")
	commitFile(t, path, "t", "t\n", "branch adds t")
	commitFile(t, repo, "t", "t\n", "main adds t")
	bases := strings.Fields(gitTestOutput(t, repo, "merge-base", "--all", "main", "feature/criss"))
	require.Len(t, bases, 2, "the history must be criss-cross for this test to apply")

	assertBranchKept(t, repo, path, "feature/criss")
}
