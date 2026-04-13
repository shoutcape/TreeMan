package queue

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withTempDataDir overrides XDG_DATA_HOME for the duration of a test.
func withTempDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	return dir
}

func TestDataDir_XDGOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	assert.Equal(t, filepath.Join(dir, "treeman"), DataDir())
}

func TestDataDir_DefaultFallback(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	home, _ := os.UserHomeDir()
	assert.Equal(t, filepath.Join(home, ".local", "share", "treeman"), DataDir())
}

func TestReadAll_NoFile(t *testing.T) {
	withTempDataDir(t)
	entries, err := readAll()
	require.NoError(t, err)
	assert.Nil(t, entries)
}

func TestWriteAll_CreatesParentDirs(t *testing.T) {
	dir := withTempDataDir(t)
	entries := []Entry{{Path: "/tmp/wt", Branch: "feat", RepoRoot: "/tmp", QueuedAt: time.Now().UTC()}}
	require.NoError(t, writeAll(entries))

	_, err := os.Stat(filepath.Join(dir, "treeman", fileName))
	require.NoError(t, err)
}

func TestWriteAll_ReadAll_Roundtrip(t *testing.T) {
	withTempDataDir(t)
	now := time.Now().UTC().Truncate(time.Second)
	entries := []Entry{
		{Path: "/home/user/repo.feat-a", Branch: "feat/a", RepoRoot: "/home/user/repo", QueuedAt: now},
		{Path: "/home/user/repo.fix-b", Branch: "fix/b", RepoRoot: "/home/user/repo", QueuedAt: now},
	}
	require.NoError(t, writeAll(entries))

	got, err := readAll()
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "/home/user/repo.feat-a", got[0].Path)
	assert.Equal(t, "feat/a", got[0].Branch)
	assert.Equal(t, "fix/b", got[1].Branch)
}

func TestWriteAll_EmptyRemovesFile(t *testing.T) {
	dir := withTempDataDir(t)
	require.NoError(t, writeAll([]Entry{{Path: "/tmp/wt", Branch: "b", RepoRoot: "/tmp", QueuedAt: time.Now().UTC()}}))
	require.NoError(t, writeAll(nil))

	_, err := os.Stat(filepath.Join(dir, "treeman", fileName))
	assert.True(t, os.IsNotExist(err))
}
