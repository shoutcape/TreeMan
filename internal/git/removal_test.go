package git

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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
	assert.False(t, CleanupPending(result.CleanupJob))
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
	assert.True(t, CleanupPending(result.CleanupJob))
	assert.True(t, result.CleanupStarted)
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

	// The job, then the lock, then the diagnostic: a run that got all the way
	// through leaves the queue with nothing to retry.
	require.Eventually(t, func() bool {
		_, err := os.Stat(job)
		return os.IsNotExist(err)
	}, 2*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		_, err := os.Stat(cleanupLockPath(trashRoot, job))
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
	assert.True(t, CleanupPending(result.CleanupJob))
	// Queued for a later removal to retry, but nothing is working on it.
	assert.False(t, result.CleanupStarted)
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
	require.True(t, CleanupPending(firstResult.CleanupJob))

	second := addTestWorktree(t, repo, "second")
	secondSHA := branchSHAForRemoval(t, repo, "second")
	var cleaned []string
	detachRemoval = func(_ string, job string) error {
		cleaned = append(cleaned, job)
		return os.RemoveAll(job)
	}

	secondResult, err := RemoveWorktreeAndBranch(repo, second, "second", secondSHA, false)

	require.NoError(t, err)
	assert.False(t, CleanupPending(secondResult.CleanupJob))
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

func TestRetryTrashCleanupSkipsUnreadyJobWhileOriginalWorktreeIsRegistered(t *testing.T) {
	trashRoot := filepath.Join(t.TempDir(), "treeman", "trash")
	job := filepath.Join(trashRoot, "restoration-failed")
	originalPath := filepath.Join(t.TempDir(), "worktree")
	require.NoError(t, os.MkdirAll(job, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(job, cleanupOriginalPathFile), []byte(originalPath), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(job, stagedWorktreeName), 0o700))

	called := false
	previous := detachRemoval
	detachRemoval = func(string, string) error {
		called = true
		return nil
	}
	t.Cleanup(func() { detachRemoval = previous })

	jobs, err := inspectCleanupQueue(trashRoot)
	require.NoError(t, err)
	err = retryTrashCleanup(trashRoot, jobs)

	require.NoError(t, err)
	assert.False(t, called)
	assert.DirExists(t, job)
}

func TestRetryTrashCleanupPreservesUnreadyJobAfterOriginalWorktreeIsUnregistered(t *testing.T) {
	trashRoot := filepath.Join(t.TempDir(), "treeman", "trash")
	job := filepath.Join(trashRoot, "restoration-failed")
	originalPath := filepath.Join(t.TempDir(), "worktree")
	require.NoError(t, os.MkdirAll(job, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(job, cleanupOriginalPathFile), []byte(originalPath), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(job, stagedWorktreeName), 0o700))

	var cleaned string
	previous := detachRemoval
	detachRemoval = func(_ string, gotJob string) error {
		cleaned = gotJob
		return os.RemoveAll(gotJob)
	}
	t.Cleanup(func() { detachRemoval = previous })

	jobs, err := inspectCleanupQueue(trashRoot)
	require.NoError(t, err)
	err = retryTrashCleanup(trashRoot, jobs)

	require.NoError(t, err)
	assert.Empty(t, cleaned)
	assert.DirExists(t, job)
}

func TestRetryTrashCleanupSkipsAnActiveJob(t *testing.T) {
	trashRoot := filepath.Join(t.TempDir(), "treeman", "trash")
	job := filepath.Join(trashRoot, "active")
	require.NoError(t, os.MkdirAll(job, 0o700))
	lock, err := os.OpenFile(cleanupLockPath(trashRoot, job), os.O_CREATE|os.O_RDWR, 0o600)
	require.NoError(t, err)
	require.NoError(t, syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	t.Cleanup(func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	})

	called := false
	previous := detachRemoval
	detachRemoval = func(string, string) error {
		called = true
		return nil
	}
	t.Cleanup(func() { detachRemoval = previous })

	jobs, err := inspectCleanupQueue(trashRoot)
	require.NoError(t, err)
	err = retryTrashCleanup(trashRoot, jobs)

	require.NoError(t, err)
	assert.False(t, called)
	assert.DirExists(t, job)
}

// removalFailureOf reads the classification a failed removal carried. Every
// failure has to carry one: it is what tells a batch whether the run may go on.
func removalFailureOf(t *testing.T, err error) *RemovalError {
	t.Helper()
	require.Error(t, err)
	var failure *RemovalError
	require.ErrorAs(t, err, &failure)
	return failure
}

// A refusal restores the capture, so the location the checks ran against is
// gone by the time anyone reads why. The message has to name the path the user
// can still go and look at.
// A crash can leave staging metadata created but empty on disk while the
// rename that follows it succeeded, so a job can hold real data and not say
// where it belongs. That must not be disposable, and it must not stop every
// other removal in the repository either.
// The cleanup script removes its job directory, then its lock, then its
// diagnostic. If either of the last two fails, queue inspection cannot see what
// is left -- it only walks directories -- so the files would sit there forever
// with a recorded failure nobody ever reads.
func TestRemovalReclaimsOrphanedCleanupBookkeeping(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")
	sha := branchSHAForRemoval(t, repo, "feature")
	commonDir, err := CommonDir(repo)
	require.NoError(t, err)
	trashRoot := filepath.Join(commonDir, filepath.FromSlash(trashDirName))
	require.NoError(t, os.MkdirAll(trashRoot, 0o700))
	orphan := filepath.Join(trashRoot, "worktree-gone")
	require.NoError(t, os.WriteFile(cleanupLockPath(trashRoot, orphan), nil, 0o600))
	require.NoError(t, os.WriteFile(cleanupErrorPath(trashRoot, orphan), []byte("rmdir: Directory not empty"), 0o600))
	captureStagedRemoval(t)

	result, err := RemoveWorktreeAndBranch(repo, worktree, "feature", sha, false)

	require.NoError(t, err)
	require.Error(t, result.CleanupError)
	assert.Contains(t, result.CleanupError.Error(), "rmdir: Directory not empty")
	assert.NoFileExists(t, cleanupLockPath(trashRoot, orphan))
	assert.NoFileExists(t, cleanupErrorPath(trashRoot, orphan))
}

// A cleanup that is running has already removed its job directory and still
// holds its lock, which must not be mistaken for an orphan.
func TestReclaimOrphanedCleanupFilesLeavesARunningCleanupAlone(t *testing.T) {
	trashRoot := t.TempDir()
	job := filepath.Join(trashRoot, "worktree-running")
	lockPath := cleanupLockPath(trashRoot, job)
	require.NoError(t, os.WriteFile(lockPath, nil, 0o600))
	lock, err := os.OpenFile(lockPath, os.O_RDWR, 0)
	require.NoError(t, err)
	defer lock.Close()
	require.NoError(t, syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))

	require.NoError(t, reclaimOrphanedCleanupFiles(trashRoot))
	assert.FileExists(t, lockPath)
}

func TestRemovalSurvivesCaptureMetadataThatRecordsNoPath(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")
	sha := branchSHAForRemoval(t, repo, "feature")
	commonDir, err := CommonDir(repo)
	require.NoError(t, err)
	trashRoot := filepath.Join(commonDir, filepath.FromSlash(trashDirName))
	stranded := filepath.Join(trashRoot, "worktree-stranded")
	captured := filepath.Join(stranded, stagedWorktreeName)
	require.NoError(t, os.MkdirAll(captured, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(captured, "payload"), []byte("irreplaceable"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(stranded, cleanupOriginalPathFile), nil, 0o600))
	cleaned := captureStagedRemoval(t)

	result, err := RemoveWorktreeAndBranch(repo, worktree, "feature", sha, false)

	require.NoError(t, err, "one unreadable capture must not stop the repository")
	assert.True(t, result.WorktreeUnregistered)
	assert.True(t, result.BranchDeleted)
	// Protected, not disposed of, and reported rather than kept in silence.
	assert.FileExists(t, filepath.Join(captured, "payload"))
	assert.NotContains(t, *cleaned, filepath.Join(stranded, stagedWorktreeName))
	require.Error(t, result.CleanupError)
	assert.Contains(t, result.CleanupError.Error(), "does not record where it came from")
}

// An empty original path must never be compared against a candidate: made
// absolute it becomes the working directory, which could match one.
func TestRemovalDoesNotMatchAPathlessCaptureAgainstTheWorkingDirectory(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")
	sha := branchSHAForRemoval(t, repo, "feature")
	commonDir, err := CommonDir(repo)
	require.NoError(t, err)
	trashRoot := filepath.Join(commonDir, filepath.FromSlash(trashDirName))
	stranded := filepath.Join(trashRoot, "worktree-stranded")
	require.NoError(t, os.MkdirAll(filepath.Join(stranded, stagedWorktreeName), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(stranded, cleanupOriginalPathFile), []byte("   "), 0o600))
	restore, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(worktree))
	t.Cleanup(func() { _ = os.Chdir(restore) })
	captureStagedRemoval(t)

	_, err = RemoveWorktreeAndBranch(repo, worktree, "feature", sha, false)

	require.NoError(t, err, "a capture with no recorded path belongs to no candidate")
}

func TestRemoveWorktreeAndBranchRefusesAgainstTheRegisteredPath(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")
	sha := branchSHAForRemoval(t, repo, "feature")
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "scratch"), []byte("unsaved"), 0o600))
	captureStagedRemoval(t)

	_, err := RemoveWorktreeAndBranch(repo, worktree, "feature", sha, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), strconv.Quote(worktree))
	assert.NotContains(t, err.Error(), filepath.FromSlash(trashDirName), "the staged location no longer exists")
	assert.DirExists(t, worktree)
}

func TestRemoveWorktreeAndBranchClassifiesFailuresByWhatSurvived(t *testing.T) {
	t.Run("a restored capture refuses this worktree alone", func(t *testing.T) {
		repo := createGitTestRepo(t)
		worktree := addTestWorktree(t, repo, "feature")
		sha := branchSHAForRemoval(t, repo, "feature")
		require.NoError(t, os.WriteFile(filepath.Join(worktree, "scratch"), []byte("unsaved"), 0o644))
		captureStagedRemoval(t)

		_, err := RemoveWorktreeAndBranch(repo, worktree, "feature", sha, false)

		failure := removalFailureOf(t, err)
		assert.Equal(t, RemovalScopeCandidate, failure.Scope)
		assert.Empty(t, failure.Capture, "a worktree that is back at its path needs no recovery")
		assert.FileExists(t, filepath.Join(worktree, "scratch"))
	})

	t.Run("an unusable cleanup queue is the repository's failure", func(t *testing.T) {
		repo := createGitTestRepo(t)
		worktree := addTestWorktree(t, repo, "feature")
		sha := branchSHAForRemoval(t, repo, "feature")
		commonDir, err := CommonDir(repo)
		require.NoError(t, err)
		trashRoot := filepath.Join(commonDir, filepath.FromSlash(trashDirName))
		require.NoError(t, os.MkdirAll(filepath.Dir(trashRoot), 0o700))
		// A file where the queue belongs blocks it for every candidate, and
		// does so whoever is running: no privilege waives ENOTDIR.
		require.NoError(t, os.WriteFile(trashRoot, []byte("not a queue"), 0o600))

		_, err = RemoveWorktreeAndBranch(repo, worktree, "feature", sha, false)

		failure := removalFailureOf(t, err)
		assert.Equal(t, RemovalScopeRepository, failure.Scope)
		assert.DirExists(t, worktree)
		gitTest(t, repo, "show-ref", "--verify", "refs/heads/feature")
	})

	t.Run("an unreadable cleanup queue is the repository's failure", func(t *testing.T) {
		repo := createGitTestRepo(t)
		worktree := addTestWorktree(t, repo, "feature")
		sha := branchSHAForRemoval(t, repo, "feature")
		commonDir, err := CommonDir(repo)
		require.NoError(t, err)
		trashRoot := filepath.Join(commonDir, filepath.FromSlash(trashDirName))
		require.NoError(t, os.MkdirAll(trashRoot, 0o700))
		require.NoError(t, os.Chmod(trashRoot, 0o000))
		t.Cleanup(func() { _ = os.Chmod(trashRoot, 0o700) })
		if _, err := os.ReadDir(trashRoot); err == nil {
			t.Skip("the cleanup queue is readable despite its mode; this needs an unprivileged user")
		}

		_, err = RemoveWorktreeAndBranch(repo, worktree, "feature", sha, false)

		// Every candidate reads this queue, so blaming the worktree that
		// reached it first would repeat one repository problem per worktree.
		failure := removalFailureOf(t, err)
		assert.Equal(t, RemovalScopeRepository, failure.Scope)
		assert.DirExists(t, worktree)
		gitTest(t, repo, "show-ref", "--verify", "refs/heads/feature")
	})

	t.Run("a branch that outlives its worktree is not a refusal", func(t *testing.T) {
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

		failure := removalFailureOf(t, err)
		assert.Equal(t, RemovalScopeBranchRetained, failure.Scope)
		assert.True(t, result.WorktreeUnregistered)
	})
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
	assert.False(t, CleanupPending(result.CleanupJob))
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
	assert.Contains(t, err.Error(), "remains at "+strconv.Quote(stagedPath))
	// The registration survives, so nothing about what completed distinguishes
	// this from a refusal; the scope and the capture it names are what do.
	failure := removalFailureOf(t, err)
	assert.Equal(t, RemovalScopeCaptureRetained, failure.Scope)
	assert.Equal(t, stagedPath, failure.Capture)
	replacement, readErr := os.ReadFile(original)
	require.NoError(t, readErr)
	assert.Equal(t, "replacement", string(replacement))
	captured, readErr := os.ReadFile(stagedPath)
	require.NoError(t, readErr)
	assert.Equal(t, "captured", string(captured))
}

func TestRemovalPreservesUnresolvedCaptureAcrossRetries(t *testing.T) {
	for _, refused := range []bool{false, true} {
		t.Run(strconv.FormatBool(refused), func(t *testing.T) {
			repo := createGitTestRepo(t)
			worktree := addTestWorktree(t, repo, "feature")
			sha := branchSHAForRemoval(t, repo, "feature")
			require.NoError(t, os.WriteFile(filepath.Join(worktree, "scratch"), []byte("protected"), 0o600))
			commonDir, err := CommonDir(repo)
			require.NoError(t, err)
			registration, err := linkedRegistrationDir(commonDir, worktree)
			require.NoError(t, err)
			trashRoot := filepath.Join(commonDir, filepath.FromSlash(trashDirName))
			require.NoError(t, os.MkdirAll(trashRoot, 0o700))
			staged, missing, err := stageWorktreeRemoval(trashRoot, worktree)
			require.NoError(t, err)
			require.False(t, missing)
			if refused {
				cause := validateStagedWorktree(repo, worktree, staged.path, registration, false)
				require.ErrorContains(t, cause, "uncommitted or untracked")
				require.NoError(t, os.Mkdir(worktree, 0o700))
				retained := restoreStagedWorktree(worktree, staged, cause)
				require.ErrorContains(t, retained, "remains at")
				require.Equal(t, RemovalScopeCaptureRetained, removalFailureOf(t, retained).Scope)
				require.NoError(t, os.Remove(worktree))
			}
			cleaned := captureStagedRemoval(t)
			for i := 0; i < 2; i++ {
				result, err := RemoveWorktreeAndBranch(repo, worktree, "feature", sha, true)
				require.ErrorContains(t, err, staged.path)
				assert.False(t, result.WorktreeUnregistered)
				assert.False(t, result.BranchDeleted)
				assert.Contains(t, gitTestOutput(t, repo, "worktree", "list", "--porcelain"), worktree)
				assert.Equal(t, sha, branchSHAForRemoval(t, repo, "feature"))
			}
			// External registration removal must not authorize the capture either.
			gitTest(t, repo, "worktree", "remove", worktree)
			jobs, err := inspectCleanupQueue(trashRoot)
			require.NoError(t, err)
			require.NoError(t, retryTrashCleanup(trashRoot, jobs))
			assert.Empty(t, *cleaned)
			assert.FileExists(t, filepath.Join(staged.path, "scratch"))
		})
	}
}

func TestRemovalWithRelativeGitReferences(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")
	sha := branchSHAForRemoval(t, repo, "feature")
	commonDir, err := CommonDir(repo)
	require.NoError(t, err)
	registration, err := linkedRegistrationDir(commonDir, worktree)
	require.NoError(t, err)
	relative, err := filepath.Rel(worktree, registration)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+relative+"\n"), 0o600))
	backlink, err := filepath.Rel(registration, filepath.Join(worktree, ".git"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(registration, "gitdir"), []byte(backlink+"\n"), 0o600))
	require.NoError(t, EnsureHoldsWorktree(repo, worktree))
	captureStagedRemoval(t)
	result, err := RemoveWorktreeAndBranch(repo, worktree, "feature", sha, false)
	require.NoError(t, err)
	assert.True(t, result.WorktreeUnregistered)
	assert.True(t, result.BranchDeleted)
}

func TestRemovalRecoversAJobThatCapturedNothing(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")
	sha := branchSHAForRemoval(t, repo, "feature")
	commonDir, err := CommonDir(repo)
	require.NoError(t, err)
	trashRoot := filepath.Join(commonDir, filepath.FromSlash(trashDirName))
	require.NoError(t, os.MkdirAll(trashRoot, 0o700))
	// Staging records the original path before the rename; a process stopped
	// in that window leaves metadata with nothing behind it.
	job, err := os.MkdirTemp(trashRoot, "worktree-")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(job, cleanupOriginalPathFile), []byte(worktree), 0o600))

	cleaned := captureStagedRemoval(t)
	result, err := RemoveWorktreeAndBranch(repo, worktree, "feature", sha, false)

	require.NoError(t, err, "an empty capture must not refuse removal of its own worktree")
	assert.True(t, result.WorktreeUnregistered)
	assert.True(t, result.BranchDeleted)
	assert.NoDirExists(t, job, "metadata protecting nothing is disposable")
	assert.Contains(t, *cleaned, filepath.Join(job, stagedWorktreeName))
}

func TestRemovalStillProtectsACaptureWhoseDirectoryExists(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")
	sha := branchSHAForRemoval(t, repo, "feature")
	commonDir, err := CommonDir(repo)
	require.NoError(t, err)
	trashRoot := filepath.Join(commonDir, filepath.FromSlash(trashDirName))
	require.NoError(t, os.MkdirAll(trashRoot, 0o700))
	job, err := os.MkdirTemp(trashRoot, "worktree-")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(job, cleanupOriginalPathFile), []byte(worktree), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(job, stagedWorktreeName), 0o700))

	_, err = RemoveWorktreeAndBranch(repo, worktree, "feature", sha, false)

	require.ErrorContains(t, err, "unresolved captured data remains")
	assert.DirExists(t, filepath.Join(job, stagedWorktreeName))
}

func TestForcedRemovalSkipsTheUntrackedFileScan(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")
	commonDir, err := CommonDir(repo)
	require.NoError(t, err)
	registration, err := linkedRegistrationDir(commonDir, worktree)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "scratch"), []byte("x"), 0o600))
	// An unreadable index makes `git status` fail, so a call that still ran it
	// would surface that failure rather than the waived answer.
	require.NoError(t, os.WriteFile(filepath.Join(registration, "index"), []byte("not an index"), 0o600))

	require.ErrorContains(t, validateStagedWorktree(repo, worktree, worktree, registration, false), "could not inspect worktree")
	assert.NoError(t, validateStagedWorktree(repo, worktree, worktree, registration, true))
}
