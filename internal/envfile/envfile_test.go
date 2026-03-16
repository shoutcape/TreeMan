package envfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopy_CopiesEnvFiles(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()

	// Create some .env* files and a non-env file.
	require.NoError(t, os.WriteFile(filepath.Join(src, ".env"), []byte("KEY=val"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(src, ".env.local"), []byte("LOCAL=1"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(src, "README.md"), []byte("readme"), 0644))

	result, err := Copy(src, dest)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{".env", ".env.local"}, result.Copied)

	// Verify file contents.
	got, err := os.ReadFile(filepath.Join(dest, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "KEY=val", string(got))

	got2, err := os.ReadFile(filepath.Join(dest, ".env.local"))
	require.NoError(t, err)
	assert.Equal(t, "LOCAL=1", string(got2))

	// Non-env file must NOT have been copied.
	_, err = os.Stat(filepath.Join(dest, "README.md"))
	assert.True(t, os.IsNotExist(err))
}

func TestCopy_NoEnvFiles(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(src, "go.mod"), []byte("module x"), 0644))

	result, err := Copy(src, dest)
	require.NoError(t, err)
	assert.Empty(t, result.Copied)
}

func TestCopy_EmptySourceDir(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()

	result, err := Copy(src, dest)
	require.NoError(t, err)
	assert.Empty(t, result.Copied)
}
