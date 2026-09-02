package git

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureStagedRemoval replaces the detached unlink with a synchronous one and
// records what it was handed, so tests can assert on both the staging and the
// deletion without racing a background process.
func captureStagedRemoval(t *testing.T) *[]string {
	t.Helper()
	staged := []string{}
	previous := detachRemoval
	detachRemoval = func(path string) error {
		staged = append(staged, path)
		return os.RemoveAll(path)
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

func TestWorktreeRemoveStagesThroughTheTrashDirectory(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "scratch"), []byte("x"), 0o644))
	staged := captureStagedRemoval(t)

	require.NoError(t, WorktreeRemove(repo, worktree, false))

	require.Len(t, *staged, 1, "removal should have gone through the trash directory")
	assert.Contains(t, (*staged)[0], filepath.Join(".git", "treeman", "trash"))
	assert.NoDirExists(t, worktree)
	assert.NoDirExists(t, (*staged)[0])
	assert.NotContains(t, gitTestOutput(t, repo, "worktree", "list", "--porcelain"), "feature")
}

// A worktree whose directory is already gone cannot be renamed, so it takes
// the direct path and still unregisters.
func TestWorktreeRemoveFallsBackWhenThereIsNothingToStage(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")
	require.NoError(t, os.RemoveAll(worktree))
	staged := captureStagedRemoval(t)

	require.NoError(t, WorktreeRemove(repo, worktree, false))

	assert.Empty(t, *staged, "a missing directory has nothing to stage")
	assert.NotContains(t, gitTestOutput(t, repo, "worktree", "list", "--porcelain"), "feature")
}

// The rename is what makes removal fast, so it must not be waiting on the
// unlink. Staging a large tree stays far cheaper than deleting it.
func TestWorktreeRemoveReturnsBeforeUnlinking(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")
	deps := filepath.Join(worktree, "node_modules")
	for i := 0; i < 40; i++ {
		pkg := filepath.Join(deps, "pkg", string(rune('a'+i%26)), string(rune('a'+i/26)))
		require.NoError(t, os.MkdirAll(pkg, 0o755))
		for j := 0; j < 25; j++ {
			require.NoError(t, os.WriteFile(filepath.Join(pkg, string(rune('a'+j))+".js"), []byte("x"), 0o644))
		}
	}

	var stagedPath string
	previous := detachRemoval
	detachRemoval = func(path string) error { stagedPath = path; return nil }
	t.Cleanup(func() {
		detachRemoval = previous
		_ = os.RemoveAll(stagedPath)
	})

	start := time.Now()
	require.NoError(t, WorktreeRemove(repo, worktree, false))
	elapsed := time.Since(start)

	assert.NoDirExists(t, worktree, "the workspace is clear as soon as the call returns")
	assert.DirExists(t, stagedPath, "the files still exist, staged out of the way")
	assert.Less(t, elapsed, 2*time.Second, "removal should not be waiting on the unlink")
}

func TestEnsureHoldsWorktreeAcceptsItsOwnWorktrees(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")

	require.NoError(t, EnsureHoldsWorktree(repo, worktree))
	require.NoError(t, EnsureHoldsWorktree(repo, repo), "the main worktree is its own common directory")
}

// The check that Git would have made itself. An unrelated clone sitting at a
// stale registration's path holds work of its own, and renaming it away would
// be unrecoverable.
func TestEnsureHoldsWorktreeRefusesAForeignOccupant(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")

	require.NoError(t, os.RemoveAll(worktree))
	foreign := createGitTestRepo(t)
	require.NoError(t, os.Rename(foreign, worktree))

	err := EnsureHoldsWorktree(repo, worktree)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "different repository")
}

// A sibling worktree of the same repository is the case a repository-level
// ownership test would wrongly accept: its Git directory lives under the same
// common directory, and only the registration's own record rules it out.
func TestEnsureHoldsWorktreeRefusesASiblingMovedOntoThePath(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")
	sibling := addTestWorktree(t, repo, "sibling")

	require.NoError(t, os.RemoveAll(worktree))
	require.NoError(t, os.Rename(sibling, worktree))

	err := EnsureHoldsWorktree(repo, worktree)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "records")
}

func TestEnsureHoldsWorktreeRefusesADirectoryThatIsNotAWorktree(t *testing.T) {
	repo := createGitTestRepo(t)
	worktree := addTestWorktree(t, repo, "feature")

	require.NoError(t, os.RemoveAll(worktree))
	require.NoError(t, os.MkdirAll(worktree, 0o755))

	err := EnsureHoldsWorktree(repo, worktree)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no longer looks like a Git worktree")
}

// The argument to an unsupervised recursive delete is checked against the
// directory this process stages into, not trusted from the caller.
func TestDetachRemoveAllRefusesAPathOutsideTheTrashDirectory(t *testing.T) {
	outside := t.TempDir()

	err := detachRemoveAll(filepath.Join(outside, "something"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a staged worktree")
	assert.DirExists(t, outside)
}

func TestWorktreeDirectoryMissing(t *testing.T) {
	dir := t.TempDir()
	assert.False(t, WorktreeDirectoryMissing(dir))
	assert.True(t, WorktreeDirectoryMissing(filepath.Join(dir, "absent")))

	file := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
	assert.True(t, WorktreeDirectoryMissing(file), "a file is not a worktree directory")
}
