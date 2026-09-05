package worktree_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shoutcape/treeman/internal/worktree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateDestinationAllowsOrdinaryRepositoryDescendant(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	require.NoError(t, os.Mkdir(gitDir, 0o755))

	err := worktree.ValidateDestination(worktree.Protected{MainRoot: root, CommonDir: gitDir}, filepath.Join(root, ".worktrees", "feature"))

	require.NoError(t, err)
}

func TestValidateDestinationRejectsProtectedPaths(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	require.NoError(t, os.MkdirAll(filepath.Join(gitDir, "worktrees"), 0o755))
	protected := worktree.Protected{MainRoot: root, CommonDir: gitDir}

	for _, destination := range []string{
		root,
		filepath.Join(gitDir, "new-worktree"),
		filepath.Join(gitDir, "worktrees", "admin", "checkout"),
	} {
		t.Run(destination, func(t *testing.T) {
			err := worktree.ValidateDestination(protected, destination)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "cannot place a worktree")
		})
	}
}

func TestValidateDestinationRejectsExistingEntries(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	dir := filepath.Join(root, "dir")
	link := filepath.Join(root, "dangling")
	require.NoError(t, os.WriteFile(file, nil, 0o644))
	require.NoError(t, os.Mkdir(dir, 0o755))
	require.NoError(t, os.Symlink(filepath.Join(root, "missing"), link))

	for _, destination := range []string{file, dir, link} {
		err := worktree.ValidateDestination(worktree.Protected{}, destination)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	}
}

func TestValidateDestinationRejectsInvalidAncestors(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	broken := filepath.Join(root, "broken")
	require.NoError(t, os.WriteFile(file, nil, 0o644))
	require.NoError(t, os.Symlink(filepath.Join(root, "missing"), broken))

	for _, destination := range []string{filepath.Join(file, "tree"), filepath.Join(broken, "tree")} {
		err := worktree.ValidateDestination(worktree.Protected{}, destination)
		require.Error(t, err)
	}
}

func TestValidateDestinationRejectsSymlinkAliasOfGitDirectory(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	require.NoError(t, os.Mkdir(gitDir, 0o755))
	alias := filepath.Join(t.TempDir(), "git-alias")
	require.NoError(t, os.Symlink(gitDir, alias))

	err := worktree.ValidateDestination(worktree.Protected{MainRoot: root, CommonDir: gitDir}, filepath.Join(alias, "worktrees", "new"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Git directory")
}

func TestEnsureParentDirCreatesNestedMissingParents(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "trees", "nested", "feature")

	require.NoError(t, worktree.EnsureParentDir(destination))
	assert.DirExists(t, filepath.Dir(destination))
	assert.NoFileExists(t, destination)
}
