package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func chdirAgentTest(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { require.NoError(t, os.Chdir(previous)) })
}

func fixedPlan(path string) WorktreePlan {
	return func([]WorktreeEntry) (string, error) { return path, nil }
}

func TestCreatePlannedWorktreeForExistingBranchAttachesWithoutCreatingABranch(t *testing.T) {
	repo := createGitTestRepo(t)
	gitTest(t, repo, "branch", "feature/existing")
	sha := branchSHAForRemoval(t, repo, "feature/existing")
	chdirAgentTest(t, repo)
	path := filepath.Join(t.TempDir(), "existing")

	created, err := CreatePlannedWorktreeForExistingBranch(fixedPlan(path), "feature/existing")

	require.NoError(t, err)
	assert.Equal(t, "feature/existing", created.Branch)
	assert.Equal(t, sha, created.SHA)
	assert.Equal(t, "feature/existing", strings.TrimSpace(gitTestOutput(t, path, "branch", "--show-current")))
	// The branch's history is not rewritten by attaching.
	assert.Equal(t, sha, branchSHAForRemoval(t, repo, "feature/existing"))
}

func TestCreatePlannedWorktreeForExistingBranchRefusesABranchCheckedOutElsewhere(t *testing.T) {
	repo := createGitTestRepo(t)
	addTestWorktree(t, repo, "busy")
	chdirAgentTest(t, repo)
	path := filepath.Join(t.TempDir(), "second")

	_, err := CreatePlannedWorktreeForExistingBranch(fixedPlan(path), "busy")

	require.Error(t, err)
	assert.NoDirExists(t, path)
}

func TestCreatePlannedWorktreeForExistingBranchFailureKeepsTheBranch(t *testing.T) {
	repo := createGitTestRepo(t)
	gitTest(t, repo, "branch", "keepme")
	chdirAgentTest(t, repo)
	occupied := filepath.Join(t.TempDir(), "occupied")
	require.NoError(t, os.MkdirAll(occupied, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(occupied, "user-file"), []byte("mine"), 0o644))

	_, err := CreatePlannedWorktreeForExistingBranch(fixedPlan(occupied), "keepme")

	require.Error(t, err)
	gitTest(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/keepme")
	assert.FileExists(t, filepath.Join(occupied, "user-file"))
	assert.NotContains(t, gitTestOutput(t, repo, "worktree", "list", "--porcelain"), "refs/heads/keepme")
}

func TestCreatePlannedWorktreeForExistingBranchRefusesAMissingBranch(t *testing.T) {
	repo := createGitTestRepo(t)
	chdirAgentTest(t, repo)
	path := filepath.Join(t.TempDir(), "missing")

	_, err := CreatePlannedWorktreeForExistingBranch(fixedPlan(path), "no-such-branch")

	require.Error(t, err)
	assert.NoDirExists(t, path)
	gitTestFails(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/no-such-branch")
}

func TestRemoveWorktreeKeepingBranchRemovesADirtyWorktreeWithForceAndKeepsTheBranch(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "kept")
	sha := branchSHAForRemoval(t, repo, "kept")
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "scratch"), []byte("x"), 0o644))
	captureStagedRemoval(t)

	result, err := RemoveWorktreeKeepingBranch(repo, worktree, "kept", sha, true)

	require.NoError(t, err)
	assert.True(t, result.WorktreeUnregistered)
	assert.False(t, result.BranchDeleted)
	assert.NoDirExists(t, worktree)
	assert.Equal(t, sha, branchSHAForRemoval(t, repo, "kept"))
}

func TestRemoveWorktreeKeepingBranchRefusesADirtyWorktreeWithoutForce(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "dirty")
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "scratch"), []byte("x"), 0o644))
	captureStagedRemoval(t)

	result, err := RemoveWorktreeKeepingBranch(repo, worktree, "dirty", "", false)

	require.Error(t, err)
	assert.False(t, result.WorktreeUnregistered)
	assert.FileExists(t, filepath.Join(worktree, "scratch"))
	gitTest(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/dirty")
}

func TestRemoveWorktreeKeepingBranchRemovesADetachedWorktree(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := filepath.Join(t.TempDir(), "detached")
	gitTest(t, repo, "worktree", "add", "--detach", worktree)
	captureStagedRemoval(t)

	result, err := RemoveWorktreeKeepingBranch(repo, worktree, "", "", false)

	require.NoError(t, err)
	assert.True(t, result.WorktreeUnregistered)
	assert.False(t, result.BranchDeleted)
	assert.NoDirExists(t, worktree)
	assert.Equal(t, "main", strings.TrimSpace(gitTestOutput(t, repo, "branch", "--show-current")))
}

func TestValidateWorktreeRemovalRequiresABranchUnlessKeepingIt(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := filepath.Join(t.TempDir(), "detached")
	gitTest(t, repo, "worktree", "add", "--detach", worktree)
	chdirAgentTest(t, repo)
	entries, err := WorktreeList()
	require.NoError(t, err)

	_, _, err = ValidateWorktreeRemoval(repo, entries, worktree, "", "")
	assert.Error(t, err, "deleting a branch needs a branch")

	entry, _, err := ValidateWorktreeRemovalKeepingBranch(repo, entries, worktree, "", "")
	require.NoError(t, err)
	assert.True(t, sameRemovalPath(entry.Path, worktree))
}

func TestRemoveWorktreeKeepingBranchRefusesAMovedBranch(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "moved")
	old := branchSHAForRemoval(t, repo, "moved")
	gitTest(t, worktree, "commit", "--allow-empty", "-m", "later")
	captureStagedRemoval(t)

	_, err := RemoveWorktreeKeepingBranch(repo, worktree, "moved", old, true)

	require.Error(t, err)
	assert.DirExists(t, worktree)
}

func TestPruneWorktreesDropsRegistrationsWhoseDirectoryIsGone(t *testing.T) {
	repo := createGitTestRepo(t)
	gone := addTestWorktree(t, repo, "gone")
	kept := addTestWorktree(t, repo, "kept")
	require.NoError(t, os.RemoveAll(gone))
	chdirAgentTest(t, repo)

	require.NoError(t, PruneWorktrees())

	list := gitTestOutput(t, repo, "worktree", "list", "--porcelain")
	assert.NotContains(t, list, "refs/heads/gone")
	assert.Contains(t, list, "refs/heads/kept")
	assert.DirExists(t, kept)
	gitTest(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/gone")
}

func TestResolveCommitAndIsAncestor(t *testing.T) {
	repo := createGitTestRepo(t)
	base := branchSHAForRemoval(t, repo, "main")
	gitTest(t, repo, "commit", "--allow-empty", "-m", "next")
	head := branchSHAForRemoval(t, repo, "main")
	chdirAgentTest(t, repo)

	sha, ok, err := ResolveCommit("HEAD")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, head, sha)

	_, ok, err = ResolveCommit("refs/remotes/origin/nope")
	require.NoError(t, err)
	assert.False(t, ok, "a missing ref is absent, not an error")

	yes, err := IsAncestor(base, head)
	require.NoError(t, err)
	assert.True(t, yes)
	no, err := IsAncestor(head, base)
	require.NoError(t, err)
	assert.False(t, no)
	self, err := IsAncestor(head, head)
	require.NoError(t, err)
	assert.True(t, self, "a commit is its own ancestor")
}

func TestBranchBirthIsTheBranchCreationEvenAfterLaterCommits(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "born")
	birthSHA := branchSHAForRemoval(t, repo, "born")
	before := time.Now().Add(-2 * time.Second).Unix()
	gitTest(t, worktree, "commit", "--allow-empty", "-m", "one")
	gitTest(t, worktree, "commit", "--allow-empty", "-m", "two")
	chdirAgentTest(t, repo)

	sha, born, ok, err := BranchBirth("born")

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, birthSHA, sha)
	assert.GreaterOrEqual(t, born, before)
	assert.LessOrEqual(t, born, time.Now().Unix())
}

func TestBranchBirthOfARecreatedBranchIsTheNewCreation(t *testing.T) {
	repo := createGitTestRepo(t)
	gitTest(t, repo, "branch", "again")
	gitTest(t, repo, "branch", "-D", "again")
	later := time.Now().Add(time.Hour).Unix()
	cmd := exec.Command("git", "-C", repo, "branch", "again")
	cmd.Env = append(os.Environ(), "GIT_COMMITTER_DATE=@"+strconv.FormatInt(later, 10)+" +0000")
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "%s", out)
	chdirAgentTest(t, repo)

	_, born, ok, err := BranchBirth("again")

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, later, born)
}

func TestBranchBirthWithoutAReflogIsAbsentNotAnError(t *testing.T) {
	repo := createGitTestRepo(t)
	gitTest(t, repo, "config", "core.logAllRefUpdates", "false")
	sha := branchSHAForRemoval(t, repo, "main")
	gitTest(t, repo, "update-ref", "refs/heads/nolog", sha)
	chdirAgentTest(t, repo)

	_, _, ok, err := BranchBirth("nolog")

	require.NoError(t, err)
	assert.False(t, ok)
}

func TestBranchBirthOfAMissingBranchIsAnError(t *testing.T) {
	repo := createGitTestRepo(t)
	chdirAgentTest(t, repo)

	_, _, ok, err := BranchBirth("does-not-exist")

	assert.False(t, ok)
	assert.Error(t, err)
}
