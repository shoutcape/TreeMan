package git

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenOutsideARepositoryIsErrNotRepository(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	repo, err := Open(dir)

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotRepository), "got %v", err)
	assert.Equal(t, Repo{}, repo)
}

func TestOpenOfAMissingDirectoryFails(t *testing.T) {
	t.Parallel()
	_, err := Open(filepath.Join(t.TempDir(), "missing"))

	require.Error(t, err)
}

func TestOpenRequiresADirectory(t *testing.T) {
	t.Parallel()
	repo, err := Open("")

	require.Error(t, err, "an empty directory must not mean the current one")
	assert.Equal(t, Repo{}, repo)
}

func TestOpenFromTheMainWorktree(t *testing.T) {
	t.Parallel()
	dir := createGitTestRepo(t)

	repo, err := Open(dir)

	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(repo.Dir()))
	assert.True(t, sameRemovalPath(dir, repo.Dir()))
	assert.True(t, sameRemovalPath(dir, repo.MainRoot()))
	assert.True(t, sameRemovalPath(filepath.Join(dir, ".git"), repo.CommonDir()))
}

func TestOpenFromASubdirectoryResolvesTheRepository(t *testing.T) {
	t.Parallel()
	dir := createGitTestRepo(t)
	sub := filepath.Join(dir, "nested", "deeper")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	repo, err := Open(sub)

	require.NoError(t, err)
	assert.True(t, sameRemovalPath(sub, repo.Dir()), "Dir is where the repository was opened, not its root")
	assert.True(t, sameRemovalPath(dir, repo.MainRoot()))
	assert.True(t, sameRemovalPath(filepath.Join(dir, ".git"), repo.CommonDir()))
}

func TestOpenFromALinkedWorktreeResolvesTheMainWorktree(t *testing.T) {
	t.Parallel()
	main := createGitTestRepo(t)
	linked := addTestWorktree(t, main, "linked")

	repo, err := Open(linked)

	require.NoError(t, err)
	assert.True(t, sameRemovalPath(linked, repo.Dir()))
	assert.True(t, sameRemovalPath(main, repo.MainRoot()), "MainRoot %q is not the main worktree %q", repo.MainRoot(), main)
	assert.True(t, sameRemovalPath(filepath.Join(main, ".git"), repo.CommonDir()), "CommonDir %q is not the shared common dir", repo.CommonDir())
	assert.True(t, filepath.IsAbs(repo.CommonDir()))

	fromMain := openTestRepo(t, main)
	// Git reports the common dir relative to the main worktree but absolute
	// (and resolved) from a linked one, so the two name one directory through
	// different text wherever the temporary directory has a symlinked prefix.
	assert.Equal(t, CanonicalPath(fromMain.MainRoot()), CanonicalPath(repo.MainRoot()))
	assert.Equal(t, CanonicalPath(fromMain.CommonDir()), CanonicalPath(repo.CommonDir()))
}

func TestOpenOfARelativeDirectoryMakesDirAbsolute(t *testing.T) {
	t.Parallel()
	dir := createGitTestRepo(t)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	relative, err := filepath.Rel(cwd, dir)
	require.NoError(t, err)
	require.False(t, filepath.IsAbs(relative))

	repo, err := Open(relative)

	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(repo.Dir()))
	assert.True(t, sameRemovalPath(dir, repo.Dir()))
}

func TestRepoMethodsRunGitFromTheOpenedDirectory(t *testing.T) {
	t.Parallel()
	first := createGitTestRepo(t)
	second := createGitTestRepo(t)
	gitTest(t, first, "branch", "only-in-first")

	assert.True(t, openTestRepo(t, first).BranchExists("only-in-first"))
	assert.False(t, openTestRepo(t, second).BranchExists("only-in-first"))
}

// The test process runs inside this package's own Git checkout, so a zero
// Repo that fell back to the current directory would succeed here.
func TestZeroRepoFailsInsteadOfUsingTheCurrentDirectory(t *testing.T) {
	t.Parallel()
	var repo Repo

	_, err := repo.WorktreeList()
	assert.Error(t, err)

	_, err = repo.CurrentWorktreeRoot()
	assert.Error(t, err)

	_, err = repo.LocalBranchNames()
	assert.Error(t, err)

	never := filepath.Join(t.TempDir(), "never")
	_, err = repo.CreatePlannedWorktree(fixedPlan(never), "never", "HEAD")
	assert.Error(t, err)
	assert.NoDirExists(t, never)

	assert.False(t, repo.BranchExists("main"))

	// Methods that build their own exec.Cmd rather than going through
	// runInDir need the same guard.
	_, err = repo.HasCommits()
	assert.Error(t, err, "HasCommits")

	_, _, err = repo.ResolveCommit("HEAD")
	assert.Error(t, err, "ResolveCommit")

	_, _, err = repo.RemoteTrackingBranchSHA("main")
	assert.Error(t, err, "RemoteTrackingBranchSHA")

	_, err = repo.AnyCommitIsAncestor([]string{"HEAD"}, "HEAD")
	assert.Error(t, err, "AnyCommitIsAncestor")

	_, err = repo.ChangesMerged("HEAD", "HEAD")
	assert.Error(t, err, "ChangesMerged")

	_, err = repo.IsAncestor("HEAD", "HEAD")
	assert.Error(t, err, "IsAncestor")

	_, err = repo.CommitIsAncestorOfAny("HEAD~1", []string{"HEAD"})
	assert.Error(t, err, "CommitIsAncestorOfAny")

	cwd, err := os.Getwd()
	require.NoError(t, err)
	assert.Error(t, repo.EnsureHoldsWorktree(cwd), "EnsureHoldsWorktree")

	_, ok := repo.ConfigValue("core.bare")
	assert.False(t, ok, "ConfigValue")

	existing := filepath.Join(t.TempDir(), "existing")
	_, err = repo.CreatePlannedWorktreeForExistingBranch(fixedPlan(existing), "main")
	assert.Error(t, err, "CreatePlannedWorktreeForExistingBranch")
	assert.NoDirExists(t, existing)
}

func TestCommonDirRequiresADirectory(t *testing.T) {
	t.Parallel()
	_, err := CommonDir("")

	assert.Error(t, err)
}

func TestConfigValueReadsTheRepositorysConfiguration(t *testing.T) {
	t.Parallel()
	dir := createGitTestRepo(t)
	gitTest(t, dir, "config", "treeman.testkey", "ssh -i /keys/project")
	repo := openTestRepo(t, dir)

	value, ok := repo.ConfigValue("treeman.testkey")
	assert.True(t, ok)
	assert.Equal(t, "ssh -i /keys/project", value)

	value, ok = repo.ConfigValue("treeman.unsetkey")
	assert.False(t, ok)
	assert.Empty(t, value)

	// Another repository's configuration is not this one's.
	_, ok = openTestRepo(t, createGitTestRepo(t)).ConfigValue("treeman.testkey")
	assert.False(t, ok)
}
