package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// siblingWorktreesDir is the default worktree parent for the repository at
// repo: "<project>-worktrees" beside it. The expectation is spelled out here
// rather than taken from worktree.SiblingDir, so a mistake in that function
// is not matched by the same mistake in the test.
func siblingWorktreesDir(repo string) string {
	return filepath.Join(filepath.Dir(repo), filepath.Base(repo)+"-worktrees")
}

// siblingWorktreePath is where the worktree for slug is created by default.
func siblingWorktreePath(repo, slug string) string {
	return filepath.Join(siblingWorktreesDir(repo), slug)
}

// worktreeDirRepo returns a repository whose path has no symbolic links, so
// paths Git prints can be compared with paths the test builds.
func worktreeDirRepo(t *testing.T) string {
	t.Helper()
	repo, err := filepath.EvalSymlinks(createRemoteRepoWithNestedModule(t))
	require.NoError(t, err)
	chdirForTest(t, repo)
	pathWithOnlyGit(t)
	return repo
}

// runWorktreeCommand runs one of the worktree-creating commands through cobra,
// the way the binary does, so the --worktree-dir flag is parsed rather than
// set by hand. Setup steps are skipped; they are not what these tests cover.
func runWorktreeCommand(t *testing.T, cmd string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := New("", "", "")
	outBuf, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	root.SetOut(outBuf)
	root.SetErr(errBuf)
	root.SetArgs(append([]string{cmd, "--skip-env", "--skip-database", "--skip-deps", "--skip-hooks"}, args...))
	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

func TestDefaultWorktreeDirIsBesideTheProject(t *testing.T) {
	repo := worktreeDirRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte("update_gitignore = true\n"), 0o600))

	stdout, _, err := runWorktreeCommand(t, "create", "feature/beside")
	require.NoError(t, err)

	want := siblingWorktreePath(repo, "feature-beside")
	assert.Equal(t, want+"\n", stdout)
	assert.DirExists(t, want)
	assert.NoDirExists(t, filepath.Join(repo, ".worktrees"), "the default no longer creates the in-repo directory")
	// The sibling is outside the repository, so there is no entry to write,
	// even with update_gitignore enabled.
	assert.NoFileExists(t, filepath.Join(repo, ".gitignore"))
}

func TestCreateWorktreeDirFlagPlacesTheWorktreeThere(t *testing.T) {
	repo := worktreeDirRepo(t)
	external := filepath.Join(t.TempDir(), "flagged trees")

	stdout, _, err := runWorktreeCommand(t, "create", "feature/flagged", "--worktree-dir", external)
	require.NoError(t, err)

	want := filepath.Join(external, "feature-flagged")
	assert.Equal(t, want+"\n", stdout)
	assert.DirExists(t, want)
	assert.NoDirExists(t, siblingWorktreesDir(repo))
}

func TestBranchWorktreeDirFlagPlacesTheWorktreeThere(t *testing.T) {
	repo := worktreeDirRepo(t)
	gitTest(t, repo, "push", "origin", "main:feature/by-branch")
	external := filepath.Join(t.TempDir(), "branch-trees")

	stdout, _, err := runWorktreeCommand(t, "branch", "feature/by-branch", "--worktree-dir", external)
	require.NoError(t, err)

	want := filepath.Join(external, "feature-by-branch")
	assert.Equal(t, want+"\n", stdout)
	assert.DirExists(t, want)
	assert.NoDirExists(t, siblingWorktreesDir(repo))
}

// A relative --worktree-dir is taken from the directory the command runs in,
// not from the repository root. The subdirectory makes the two differ.
func TestWorktreeDirFlagResolvesRelativeToTheCurrentDirectory(t *testing.T) {
	repo := worktreeDirRepo(t)
	subdirectory := filepath.Join(repo, "apps", "web")
	require.NoError(t, os.MkdirAll(subdirectory, 0o755))
	chdirForTest(t, subdirectory)

	stdout, _, err := runWorktreeCommand(t, "create", "feature/relative", "--worktree-dir", "../../../flag-relative")
	require.NoError(t, err)

	// From apps/web, "../../../" is the parent of the repository.
	want := filepath.Join(filepath.Dir(repo), "flag-relative", "feature-relative")
	assert.Equal(t, want+"\n", stdout)
	assert.DirExists(t, want)
}

// The flag places one worktree; it overrides worktree_dir for that command
// and leaves the project's configuration alone.
func TestWorktreeDirFlagOverridesConfiguredDirectory(t *testing.T) {
	repo := worktreeDirRepo(t)
	configured := filepath.Join(t.TempDir(), "configured")
	flagged := filepath.Join(t.TempDir(), "flagged")
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte("worktree_dir = \""+configured+"\"\n"), 0o600))

	stdout, _, err := runWorktreeCommand(t, "create", "feature/override", "--worktree-dir", flagged)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(flagged, "feature-override")+"\n", stdout)
	assert.NoDirExists(t, filepath.Join(configured, "feature-override"))

	stdout, _, err = runWorktreeCommand(t, "create", "feature/configured")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(configured, "feature-configured")+"\n", stdout)
}

func TestEmptyWorktreeDirFlagIsAnError(t *testing.T) {
	repo := worktreeDirRepo(t)

	_, _, err := runWorktreeCommand(t, "create", "feature/empty", "--worktree-dir", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--worktree-dir")

	assert.NoDirExists(t, siblingWorktreesDir(repo), "a rejected flag must not fall back to creating anything")
	gitTestFails(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/feature/empty")
}
