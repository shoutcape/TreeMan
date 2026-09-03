package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureStagedRemoval makes cleanup synchronous so filesystem assertions do
// not race the detached process.
func captureStagedRemoval(t *testing.T) *[]string {
	t.Helper()
	staged := []string{}
	previous := detachRemoval
	detachRemoval = func(_ string, job string) error {
		staged = append(staged, filepath.Join(job, stagedWorktreeName))
		return os.RemoveAll(job)
	}
	t.Cleanup(func() { detachRemoval = previous })
	return &staged
}

func addTestWorktree(t *testing.T, repo, branch string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), branch)
	gitTest(t, repo, "worktree", "add", "-b", branch, path)
	return path
}

func branchSHAForRemoval(t *testing.T, repo, branch string) string {
	t.Helper()
	return strings.TrimSpace(gitTestOutput(t, repo, "rev-parse", "refs/heads/"+branch))
}

func TestRemoveWorktreeAndBranchStagesThroughTheTrashDirectory(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")
	expectedSHA := branchSHAForRemoval(t, repo, "feature")
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "scratch"), []byte("x"), 0o644))
	staged := captureStagedRemoval(t)

	result, err := RemoveWorktreeAndBranch(repo, worktree, "feature", expectedSHA, true)

	require.NoError(t, err)
	assert.True(t, result.WorktreeUnregistered)
	assert.True(t, result.BranchDeleted)
	assert.False(t, result.CleanupPending)
	require.Len(t, *staged, 1)
	assert.Contains(t, (*staged)[0], filepath.Join(".git", "treeman", "trash"))
	assert.NoFileExists(t, worktree)
	assert.NotContains(t, gitTestOutput(t, repo, "worktree", "list", "--porcelain"), "feature")
	gitTestFails(t, repo, "show-ref", "--verify", "refs/heads/feature")
}

func TestRemoveWorktreeAndBranchUnregistersAMissingDirectory(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")
	expectedSHA := branchSHAForRemoval(t, repo, "feature")
	require.NoError(t, os.RemoveAll(worktree))
	staged := captureStagedRemoval(t)

	result, err := RemoveWorktreeAndBranch(repo, worktree, "feature", expectedSHA, false)

	require.NoError(t, err)
	assert.True(t, result.WorktreeUnregistered)
	assert.True(t, result.BranchDeleted)
	assert.Empty(t, *staged)
	assert.NotContains(t, gitTestOutput(t, repo, "worktree", "list", "--porcelain"), "feature")
}

func TestRemoveWorktreeAndBranchReturnsWhileCleanupIsPending(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")
	expectedSHA := branchSHAForRemoval(t, repo, "feature")
	deps := filepath.Join(worktree, "node_modules")
	for i := 0; i < 40; i++ {
		pkg := filepath.Join(deps, "pkg", string(rune('a'+i%26)), string(rune('a'+i/26)))
		require.NoError(t, os.MkdirAll(pkg, 0o755))
		for j := 0; j < 25; j++ {
			require.NoError(t, os.WriteFile(filepath.Join(pkg, string(rune('a'+j))+".js"), []byte("x"), 0o644))
		}
	}

	var cleanupJob string
	previous := detachRemoval
	detachRemoval = func(_ string, job string) error { cleanupJob = job; return nil }
	t.Cleanup(func() {
		detachRemoval = previous
		_ = os.RemoveAll(cleanupJob)
	})

	start := time.Now()
	result, err := RemoveWorktreeAndBranch(repo, worktree, "feature", expectedSHA, true)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.True(t, result.WorktreeUnregistered)
	assert.True(t, result.BranchDeleted)
	assert.True(t, result.CleanupPending)
	assert.NoDirExists(t, worktree)
	assert.DirExists(t, cleanupJob)
	assert.Less(t, elapsed, 2*time.Second)
}

func TestRemoveWorktreeAndBranchRestoresDirtyWorktreeWithoutForce(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")
	expectedSHA := branchSHAForRemoval(t, repo, "feature")
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "scratch"), []byte("unsaved"), 0o644))
	captureStagedRemoval(t)

	result, err := RemoveWorktreeAndBranch(repo, worktree, "feature", expectedSHA, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "uncommitted or untracked changes")
	assert.False(t, result.WorktreeUnregistered)
	assert.False(t, result.BranchDeleted)
	assert.FileExists(t, filepath.Join(worktree, "scratch"))
	gitTest(t, repo, "show-ref", "--verify", "refs/heads/feature")
}

func TestRemoveWorktreeAndBranchRestoresAForeignOccupant(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")
	expectedSHA := branchSHAForRemoval(t, repo, "feature")
	require.NoError(t, os.RemoveAll(worktree))
	foreign := createGitTestRepo(t)
	require.NoError(t, os.Rename(foreign, worktree))
	captureStagedRemoval(t)

	result, err := RemoveWorktreeAndBranch(repo, worktree, "feature", expectedSHA, true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not the expected linked-worktree file")
	assert.False(t, result.WorktreeUnregistered)
	assert.DirExists(t, worktree)
	assert.FileExists(t, filepath.Join(worktree, ".git", "HEAD"))
	gitTest(t, repo, "show-ref", "--verify", "refs/heads/feature")
}

func TestRemoveWorktreeAndBranchRestoresASiblingMovedOntoThePath(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")
	sibling := addTestWorktree(t, repo, "sibling")
	expectedSHA := branchSHAForRemoval(t, repo, "feature")
	require.NoError(t, os.RemoveAll(worktree))
	require.NoError(t, os.Rename(sibling, worktree))
	captureStagedRemoval(t)

	result, err := RemoveWorktreeAndBranch(repo, worktree, "feature", expectedSHA, true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "different registration")
	assert.False(t, result.WorktreeUnregistered)
	assert.DirExists(t, worktree)
	gitTest(t, repo, "show-ref", "--verify", "refs/heads/feature")
}

func TestRemoveWorktreeAndBranchRejectsMainRepositoryMetadataAtLinkedPath(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")
	expectedSHA := branchSHAForRemoval(t, repo, "feature")
	commonDir, err := CommonDir(repo)
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(worktree))
	require.NoError(t, os.Mkdir(worktree, 0o755))
	require.NoError(t, os.Symlink(commonDir, filepath.Join(worktree, ".git")))
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "unrelated"), []byte("keep"), 0o644))
	captureStagedRemoval(t)

	result, err := RemoveWorktreeAndBranch(repo, worktree, "feature", expectedSHA, true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), ".git entry is not the expected linked-worktree file")
	assert.False(t, result.WorktreeUnregistered)
	assert.FileExists(t, filepath.Join(worktree, "unrelated"))
	gitTest(t, repo, "show-ref", "--verify", "refs/heads/feature")
}

func TestDetachRemoveAllRefusesMatchingDirectoryNamesOutsideActualTrash(t *testing.T) {
	actualTrash := filepath.Join(t.TempDir(), "repo", "treeman", "trash")
	outsideJob := filepath.Join(t.TempDir(), "treeman", "trash", "job")
	require.NoError(t, os.MkdirAll(actualTrash, 0o700))
	require.NoError(t, os.MkdirAll(outsideJob, 0o700))

	err := detachRemoveAll(actualTrash, outsideJob)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not contained in trash root")
	assert.DirExists(t, outsideJob)
}

func TestDetachRemoveAllCompletesQueuedJob(t *testing.T) {
	trashRoot := filepath.Join(t.TempDir(), "treeman", "trash")
	job := filepath.Join(trashRoot, "job")
	require.NoError(t, os.MkdirAll(job, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(job, "payload"), []byte("data"), 0o600))

	require.NoError(t, detachRemoveAll(trashRoot, job))

	require.Eventually(t, func() bool {
		_, err := os.Stat(job)
		return os.IsNotExist(err)
	}, 2*time.Second, 10*time.Millisecond)
	assert.NoFileExists(t, cleanupErrorPath(trashRoot, job))
}

func TestRemoveWorktreeAndBranchRestoresAFileAtTheRegisteredPath(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")
	expectedSHA := branchSHAForRemoval(t, repo, "feature")
	require.NoError(t, os.RemoveAll(worktree))
	require.NoError(t, os.WriteFile(worktree, []byte("unrelated data"), 0o644))
	captureStagedRemoval(t)

	result, err := RemoveWorktreeAndBranch(repo, worktree, "feature", expectedSHA, false)

	require.Error(t, err)
	assert.False(t, result.WorktreeUnregistered)
	assert.False(t, result.BranchDeleted)
	contents, readErr := os.ReadFile(worktree)
	require.NoError(t, readErr)
	assert.Equal(t, "unrelated data", string(contents))
	assert.Contains(t, gitTestOutput(t, repo, "worktree", "list", "--porcelain"), worktree)
	gitTest(t, repo, "show-ref", "--verify", "refs/heads/feature")
}

func TestRemoveWorktreeAndBranchQueuesCleanupAfterDetachFailure(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")
	expectedSHA := branchSHAForRemoval(t, repo, "feature")
	previous := detachRemoval
	detachRemoval = func(string, string) error { return assert.AnError }
	t.Cleanup(func() { detachRemoval = previous })

	result, err := RemoveWorktreeAndBranch(repo, worktree, "feature", expectedSHA, false)

	require.NoError(t, err)
	assert.True(t, result.WorktreeUnregistered)
	assert.True(t, result.BranchDeleted)
	assert.True(t, result.CleanupPending)
	assert.ErrorIs(t, result.CleanupError, assert.AnError)
}

func TestRemoveWorktreeAndBranchRetriesQueuedCleanup(t *testing.T) {
	repo := createGitTestRepo(t)
	first := addTestWorktree(t, repo, "first")
	firstSHA := branchSHAForRemoval(t, repo, "first")
	previous := detachRemoval
	detachRemoval = func(string, string) error { return assert.AnError }
	t.Cleanup(func() { detachRemoval = previous })

	firstResult, err := RemoveWorktreeAndBranch(repo, first, "first", firstSHA, false)
	require.NoError(t, err)
	require.True(t, firstResult.CleanupPending)

	second := addTestWorktree(t, repo, "second")
	secondSHA := branchSHAForRemoval(t, repo, "second")
	var cleaned []string
	detachRemoval = func(_ string, job string) error {
		cleaned = append(cleaned, job)
		return os.RemoveAll(job)
	}

	secondResult, err := RemoveWorktreeAndBranch(repo, second, "second", secondSHA, false)

	require.NoError(t, err)
	assert.False(t, secondResult.CleanupPending)
	assert.Len(t, cleaned, 2, "the queued job and current removal should both be cleaned")
}

func TestRemoveWorktreeAndBranchReportsDurableCleanupDiagnostic(t *testing.T) {
	repo := createGitTestRepo(t)
	commonDir, err := CommonDir(repo)
	require.NoError(t, err)
	trashRoot := filepath.Join(commonDir, filepath.FromSlash(trashDirName))
	job := filepath.Join(trashRoot, "leftover")
	require.NoError(t, os.MkdirAll(job, 0o700))
	require.NoError(t, os.WriteFile(cleanupErrorPath(trashRoot, job), []byte("permission denied"), 0o600))

	worktree := addTestWorktree(t, repo, "feature")
	expectedSHA := branchSHAForRemoval(t, repo, "feature")
	captureStagedRemoval(t)

	result, err := RemoveWorktreeAndBranch(repo, worktree, "feature", expectedSHA, false)

	require.NoError(t, err)
	require.Error(t, result.CleanupError)
	assert.Contains(t, result.CleanupError.Error(), "previous file cleanup")
	assert.Contains(t, result.CleanupError.Error(), "permission denied")
	assert.NoDirExists(t, job)
}

func TestRemoveWorktreeAndBranchReportsUnregisterBeforeCompareDeleteFailure(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")
	expectedSHA := branchSHAForRemoval(t, repo, "feature")
	gitTest(t, repo, "commit", "--allow-empty", "-m", "advance main")
	mainSHA := branchSHAForRemoval(t, repo, "main")
	previous := detachRemoval
	detachRemoval = func(_ string, job string) error {
		require.NoError(t, os.RemoveAll(job))
		gitTest(t, repo, "update-ref", "refs/heads/feature", mainSHA)
		return nil
	}
	t.Cleanup(func() { detachRemoval = previous })

	result, err := RemoveWorktreeAndBranch(repo, worktree, "feature", expectedSHA, false)

	require.Error(t, err)
	assert.True(t, result.WorktreeUnregistered)
	assert.False(t, result.BranchDeleted)
	assert.False(t, result.CleanupPending)
	assert.Equal(t, mainSHA, branchSHAForRemoval(t, repo, "feature"))
}

func TestRestoreStagedWorktreeDoesNotReplaceARecreatedPath(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "worktree")
	job := filepath.Join(root, "trash", "job")
	stagedPath := filepath.Join(job, stagedWorktreeName)
	require.NoError(t, os.MkdirAll(job, 0o700))
	require.NoError(t, os.WriteFile(stagedPath, []byte("captured"), 0o600))
	require.NoError(t, os.WriteFile(original, []byte("replacement"), 0o600))

	err := restoreStagedWorktree(original, &stagedWorktree{job: job, path: stagedPath}, assert.AnError)

	require.Error(t, err)
	replacement, readErr := os.ReadFile(original)
	require.NoError(t, readErr)
	assert.Equal(t, "replacement", string(replacement))
	captured, readErr := os.ReadFile(stagedPath)
	require.NoError(t, readErr)
	assert.Equal(t, "captured", string(captured))
}
