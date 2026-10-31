package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func commitTestFile(t *testing.T, dir, name, content, message string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	gitTest(t, dir, "add", name)
	gitTest(t, dir, "commit", "-m", message)
}

// changesRepo is a repository on main with a topic branch checked out in a
// worktree, both starting at the same commit.
func changesRepo(t *testing.T) (repo, topic string) {
	t.Helper()
	repo = createGitTestRepo(t)
	topic = filepath.Join(t.TempDir(), "topic")
	gitTest(t, repo, "worktree", "add", "-b", "topic", topic)
	return repo, topic
}

func changesMergedIn(t *testing.T, repo, target, tip string) (bool, error) {
	t.Helper()
	chdirAgentTest(t, repo)
	return ChangesMerged(target, tip)
}

func TestChangesMergedAfterASquashMerge(t *testing.T) {
	repo, topic := changesRepo(t)
	commitTestFile(t, topic, "a.txt", "a\n", "a")
	commitTestFile(t, topic, "a.txt", "a\nb\n", "b")
	commitTestFile(t, topic, "file", "base\nmore\n", "edit base file")
	gitTest(t, repo, "merge", "--squash", "topic")
	gitTest(t, repo, "commit", "-m", "squash")

	merged, err := changesMergedIn(t, repo, "main", "topic")

	require.NoError(t, err)
	assert.True(t, merged)
}

func TestChangesMergedAfterARebaseMergeWithOtherWorkOnTarget(t *testing.T) {
	repo, topic := changesRepo(t)
	commitTestFile(t, topic, "a.txt", "a\n", "a")
	commitTestFile(t, topic, "b.txt", "b\n", "b")
	commitTestFile(t, repo, "main.txt", "main\n", "main work")
	gitTest(t, repo, "cherry-pick", "main..topic")
	commitTestFile(t, repo, "later.txt", "later\n", "later main work")

	merged, err := changesMergedIn(t, repo, "main", "topic")

	require.NoError(t, err)
	assert.True(t, merged)
}

func TestChangesMergedAcceptsCommitIDs(t *testing.T) {
	repo, topic := changesRepo(t)
	commitTestFile(t, topic, "a.txt", "a\n", "a")
	gitTest(t, repo, "merge", "--squash", "topic")
	gitTest(t, repo, "commit", "-m", "squash")
	target := gitTestOutput(t, repo, "rev-parse", "main")
	tip := gitTestOutput(t, repo, "rev-parse", "topic")

	merged, err := changesMergedIn(t, repo, target, tip)

	require.NoError(t, err)
	assert.True(t, merged)
}

func TestChangesMergedIsFalseForAnUnmergedBranch(t *testing.T) {
	repo, topic := changesRepo(t)
	commitTestFile(t, topic, "a.txt", "a\n", "a")
	commitTestFile(t, repo, "main.txt", "main\n", "main work")

	merged, err := changesMergedIn(t, repo, "main", "topic")

	require.NoError(t, err)
	assert.False(t, merged)
}

func TestChangesMergedIsFalseForAPartialMerge(t *testing.T) {
	repo, topic := changesRepo(t)
	commitTestFile(t, topic, "a.txt", "a\n", "a")
	commitTestFile(t, topic, "b.txt", "b\n", "b")
	gitTest(t, repo, "cherry-pick", "topic~1")

	merged, err := changesMergedIn(t, repo, "main", "topic")

	require.NoError(t, err)
	assert.False(t, merged)
}

func TestChangesMergedIsFalseForOnlyEmptyCommits(t *testing.T) {
	repo, topic := changesRepo(t)
	gitTest(t, topic, "commit", "--allow-empty", "-m", "empty one")
	gitTest(t, topic, "commit", "--allow-empty", "-m", "empty two")
	commitTestFile(t, repo, "main.txt", "main\n", "main work")

	merged, err := changesMergedIn(t, repo, "main", "topic")

	require.NoError(t, err)
	assert.False(t, merged, "a branch that changes nothing is not shown merged by its content")
}

func TestChangesMergedIsFalseForCommitsThatCancelOut(t *testing.T) {
	repo, topic := changesRepo(t)
	commitTestFile(t, topic, "a.txt", "a\n", "a")
	gitTest(t, topic, "revert", "--no-edit", "HEAD")

	merged, err := changesMergedIn(t, repo, "main", "topic")

	require.NoError(t, err)
	assert.False(t, merged)
}

func TestChangesMergedIsFalseForAConflictAndLeavesTheCheckoutAlone(t *testing.T) {
	repo, topic := changesRepo(t)
	commitTestFile(t, topic, "file", "topic\n", "topic edit")
	commitTestFile(t, repo, "file", "main\n", "main edit")
	head := gitTestOutput(t, repo, "rev-parse", "HEAD")

	merged, err := changesMergedIn(t, repo, "main", "topic")

	assert.NoError(t, err, "a conflict is an answer, not a failure")
	assert.False(t, merged)
	assert.Equal(t, head, gitTestOutput(t, repo, "rev-parse", "HEAD"))
	assert.Empty(t, gitTestOutput(t, repo, "status", "--porcelain"))
	content, readErr := os.ReadFile(filepath.Join(repo, "file"))
	require.NoError(t, readErr)
	assert.Equal(t, "main\n", string(content))
}

func TestChangesMergedIsFalseWhenTheMergedChangeWasLaterEditedOnTarget(t *testing.T) {
	repo, topic := changesRepo(t)
	commitTestFile(t, topic, "a.txt", "a\n", "a")
	gitTest(t, repo, "merge", "--squash", "topic")
	gitTest(t, repo, "commit", "-m", "squash")
	commitTestFile(t, repo, "a.txt", "edited\n", "edit on main")

	merged, _ := changesMergedIn(t, repo, "main", "topic")

	assert.False(t, merged, "merging again would conflict, so it is not shown merged")
}

func TestChangesMergedFailsForUnrelatedHistories(t *testing.T) {
	repo := createGitTestRepo(t)
	gitTest(t, repo, "checkout", "--orphan", "unrelated")
	commitTestFile(t, repo, "file", "base\n", "same content, no shared history")
	gitTest(t, repo, "checkout", "main")

	merged, err := changesMergedIn(t, repo, "main", "unrelated")

	assert.Error(t, err)
	assert.False(t, merged)
}

func TestChangesMergedFailsForAMissingRef(t *testing.T) {
	repo := createGitTestRepo(t)

	merged, err := changesMergedIn(t, repo, "main", "does-not-exist")

	assert.Error(t, err)
	assert.False(t, merged)
}

// TestChangesMergedIsFalseForACrissCrossHistory follows a history with two
// best common ancestors. Against either one alone, topic's changes look
// already in main, but topic has work main lacks: it set f back to v0 after
// main took v1.
func TestChangesMergedIsFalseForACrissCrossHistory(t *testing.T) {
	repo := createGitTestRepo(t)
	commitTestFile(t, repo, "f", "v0\n", "A")
	topic := filepath.Join(t.TempDir(), "topic")
	gitTest(t, repo, "worktree", "add", "-b", "topic", topic)
	commitTestFile(t, repo, "m.txt", "m\n", "B1")
	b1 := gitTestOutput(t, repo, "rev-parse", "HEAD")
	commitTestFile(t, topic, "f", "v1\n", "B2")
	gitTest(t, repo, "merge", "--no-ff", "--no-edit", "topic")
	gitTest(t, topic, "merge", "--no-ff", "--no-edit", b1)
	commitTestFile(t, topic, "f", "v0\n", "topic back to v0")
	commitTestFile(t, topic, "t", "t\n", "topic adds t")
	commitTestFile(t, repo, "t", "t\n", "main adds t")
	bases := strings.Fields(gitTestOutput(t, repo, "merge-base", "--all", "main", "topic"))
	require.Len(t, bases, 2, "the history must be criss-cross for this test to apply")

	merged, err := changesMergedIn(t, repo, "main", "topic")

	require.NoError(t, err)
	assert.False(t, merged)
}
