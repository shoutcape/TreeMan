package git

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// renameCheckedNoReplace is what Linux falls back to when a filesystem rejects
// RENAME_NOREPLACE, and what every other platform uses outright, so its
// guarantee is exercised here rather than only through the platform in use.
func TestRenameCheckedNoReplace(t *testing.T) {
	t.Run("moves into a free path", func(t *testing.T) {
		root := t.TempDir()
		source := filepath.Join(root, "source")
		destination := filepath.Join(root, "destination")
		require.NoError(t, os.WriteFile(source, []byte("captured"), 0o600))

		require.NoError(t, renameCheckedNoReplace(source, destination))

		moved, err := os.ReadFile(destination)
		require.NoError(t, err)
		assert.Equal(t, "captured", string(moved))
		assert.NoFileExists(t, source)
	})

	t.Run("refuses an occupied path", func(t *testing.T) {
		root := t.TempDir()
		source := filepath.Join(root, "source")
		destination := filepath.Join(root, "destination")
		require.NoError(t, os.WriteFile(source, []byte("captured"), 0o600))
		require.NoError(t, os.WriteFile(destination, []byte("occupant"), 0o600))

		err := renameCheckedNoReplace(source, destination)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "an object already exists at")
		occupant, readErr := os.ReadFile(destination)
		require.NoError(t, readErr)
		assert.Equal(t, "occupant", string(occupant), "the occupant must survive")
		assert.FileExists(t, source, "the capture must survive")
	})
}

// The exported behaviour must hold whichever implementation the build selects.
func TestRenameNoReplaceRefusesAnOccupiedPath(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	destination := filepath.Join(root, "destination")
	require.NoError(t, os.Mkdir(source, 0o700))
	require.NoError(t, os.Mkdir(destination, 0o700))

	require.Error(t, renameNoReplace(source, destination))
	assert.DirExists(t, source)
	assert.DirExists(t, destination)
}
