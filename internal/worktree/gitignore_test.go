package worktree_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shoutcape/treeman/internal/worktree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readGitignore(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	require.NoError(t, err)
	return string(data)
}

func TestEnsureIgnored_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, worktree.EnsureIgnored(dir, filepath.Join(dir, worktree.DefaultDir)))
	content := readGitignore(t, dir)
	assert.Contains(t, content, ".worktrees/")
}

func TestEnsureIgnored_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	existing := "node_modules/\ndist/\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(existing), 0644))

	require.NoError(t, worktree.EnsureIgnored(dir, filepath.Join(dir, worktree.DefaultDir)))

	content := readGitignore(t, dir)
	assert.Contains(t, content, "node_modules/")
	assert.Contains(t, content, ".worktrees/")
}

func TestEnsureIgnored_NoDuplicate(t *testing.T) {
	dir := t.TempDir()
	existing := "node_modules/\n.worktrees/\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(existing), 0644))

	require.NoError(t, worktree.EnsureIgnored(dir, filepath.Join(dir, worktree.DefaultDir)))
	require.NoError(t, worktree.EnsureIgnored(dir, filepath.Join(dir, worktree.DefaultDir)))

	content := readGitignore(t, dir)
	count := strings.Count(content, ".worktrees/")
	assert.Equal(t, 1, count, "expected exactly one .worktrees/ entry")
}

func TestEnsureIgnored_NoTrailingNewlineInExisting(t *testing.T) {
	dir := t.TempDir()
	// File exists but has no trailing newline.
	existing := "node_modules/"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(existing), 0644))

	require.NoError(t, worktree.EnsureIgnored(dir, filepath.Join(dir, worktree.DefaultDir)))

	content := readGitignore(t, dir)
	assert.Contains(t, content, "\n.worktrees/")
}

func TestEnsureIgnored_CustomInternalDirectory(t *testing.T) {
	dir := t.TempDir()
	worktreeDir := filepath.Join(dir, "build", "trees with [metadata]")

	require.NoError(t, worktree.EnsureIgnored(dir, worktreeDir))
	require.NoError(t, worktree.EnsureIgnored(dir, worktreeDir))

	assert.Equal(t, "/build/trees\\ with\\ \\[metadata\\]/\n", readGitignore(t, dir))
}

func TestEnsureIgnored_ExternalDirectoryDoesNothing(t *testing.T) {
	dir := t.TempDir()
	external := filepath.Join(t.TempDir(), "trees")

	require.NoError(t, worktree.EnsureIgnored(dir, external))
	assert.NoFileExists(t, filepath.Join(dir, ".gitignore"))
}

func TestIgnoreEntryRecognizesSymlinkedInternalDirectory(t *testing.T) {
	dir := t.TempDir()
	realParent := filepath.Join(dir, "real")
	require.NoError(t, os.Mkdir(realParent, 0o755))
	link := filepath.Join(dir, "linked")
	require.NoError(t, os.Symlink(realParent, link))

	entry, inside, err := worktree.IgnoreEntry(dir, filepath.Join(link, "trees"))

	require.NoError(t, err)
	assert.True(t, inside)
	assert.Equal(t, "/real/trees/", entry)
}
