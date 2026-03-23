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
	require.NoError(t, worktree.EnsureIgnored(dir))
	content := readGitignore(t, dir)
	assert.Contains(t, content, ".worktrees/")
}

func TestEnsureIgnored_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	existing := "node_modules/\ndist/\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(existing), 0644))

	require.NoError(t, worktree.EnsureIgnored(dir))

	content := readGitignore(t, dir)
	assert.Contains(t, content, "node_modules/")
	assert.Contains(t, content, ".worktrees/")
}

func TestEnsureIgnored_NoDuplicate(t *testing.T) {
	dir := t.TempDir()
	existing := "node_modules/\n.worktrees/\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(existing), 0644))

	require.NoError(t, worktree.EnsureIgnored(dir))
	require.NoError(t, worktree.EnsureIgnored(dir))

	content := readGitignore(t, dir)
	count := strings.Count(content, ".worktrees/")
	assert.Equal(t, 1, count, "expected exactly one .worktrees/ entry")
}

func TestEnsureIgnored_NoTrailingNewlineInExisting(t *testing.T) {
	dir := t.TempDir()
	// File exists but has no trailing newline.
	existing := "node_modules/"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(existing), 0644))

	require.NoError(t, worktree.EnsureIgnored(dir))

	content := readGitignore(t, dir)
	assert.Contains(t, content, "\n.worktrees/")
}
