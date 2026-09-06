//go:build linux || darwin

package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAtomicWriteFileWritesContentsModeAndCleansTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state")
	require.NoError(t, os.WriteFile(path, []byte("previous"), 0o600))

	require.NoError(t, AtomicWriteFile(path, []byte("updated"), 0o640))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, []byte("updated"), data)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, "state", entries[0].Name())
}

func TestAtomicWriteFileRenameFailurePreservesTargetAndCleansTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	// rename(2) refuses to replace a non-empty directory, which is a failure
	// the write has to survive without leaving its temporary file behind.
	path := filepath.Join(dir, "state")
	require.NoError(t, os.Mkdir(path, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(path, "kept"), []byte("valid"), 0o600))

	err := AtomicWriteFile(path, []byte("replacement"), 0o640)

	require.Error(t, err)
	data, readErr := os.ReadFile(filepath.Join(path, "kept"))
	require.NoError(t, readErr)
	assert.Equal(t, []byte("valid"), data)
	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	assert.Len(t, entries, 1)
	assert.Equal(t, "state", entries[0].Name())
}

func TestSyncDirectory(t *testing.T) {
	require.NoError(t, SyncDirectory(t.TempDir()))
}

func TestAtomicWriteFile_DirectorySyncFailureKeepsReplacementAndCleansTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state")
	require.NoError(t, os.WriteFile(path, []byte("previous"), 0o600))
	syncErr := fmt.Errorf("directory sync failed")

	err := atomicWriteFile(path, []byte("prepared"), 0o640, func(string) error {
		return syncErr
	})

	require.ErrorIs(t, err, syncErr)
	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, []byte("prepared"), data)
	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	assert.Len(t, entries, 1)
	assert.Equal(t, "state", entries[0].Name())
}
