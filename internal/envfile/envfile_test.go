package envfile

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
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

func TestCopyWith_PreservesExistingDestination(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, ".env"), []byte("KEY=main"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dest, ".env"), []byte("KEY=branch"), 0600))

	result, err := CopyWith(src, dest, CopyOptions{})
	require.NoError(t, err)

	assert.Empty(t, result.Copied)
	assert.Equal(t, []string{".env"}, result.Preserved)
	assert.Empty(t, result.Failed)

	got, err := os.ReadFile(filepath.Join(dest, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "KEY=branch", string(got))
}

func TestCopyWith_CopiesMissingFileAlongsidePreservedOne(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, ".env"), []byte("KEY=main"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(src, ".env.local"), []byte("LOCAL=main"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dest, ".env"), []byte("KEY=branch"), 0600))

	result, err := CopyWith(src, dest, CopyOptions{})
	require.NoError(t, err)

	assert.Equal(t, []string{".env.local"}, result.Copied)
	assert.Equal(t, []string{".env"}, result.Preserved)

	got, err := os.ReadFile(filepath.Join(dest, ".env.local"))
	require.NoError(t, err)
	assert.Equal(t, "LOCAL=main", string(got))
}

func TestCopyWith_RefreshReplacesExistingDestination(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, ".env"), []byte("KEY=main"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dest, ".env"), []byte("KEY=branch"), 0600))

	result, err := CopyWith(src, dest, CopyOptions{Refresh: true})
	require.NoError(t, err)

	assert.Equal(t, []string{".env"}, result.Copied)
	assert.Empty(t, result.Preserved)

	got, err := os.ReadFile(filepath.Join(dest, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "KEY=main", string(got))
}

func TestCopyWith_KeepsDestinationOnlyFiles(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		t.Run(fmt.Sprintf("refresh=%t", refresh), func(t *testing.T) {
			src, dest := t.TempDir(), t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(src, ".env"), []byte("KEY=main"), 0600))
			require.NoError(t, os.WriteFile(filepath.Join(dest, ".env.branch"), []byte("ONLY=here"), 0600))

			_, err := CopyWith(src, dest, CopyOptions{Refresh: refresh})
			require.NoError(t, err)

			got, err := os.ReadFile(filepath.Join(dest, ".env.branch"))
			require.NoError(t, err)
			assert.Equal(t, "ONLY=here", string(got))
		})
	}
}

func TestCopyWith_RejectsSymlinkedSource(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	secret := filepath.Join(t.TempDir(), "secret")
	require.NoError(t, os.WriteFile(secret, []byte("SECRET=1"), 0600))
	require.NoError(t, os.Symlink(secret, filepath.Join(src, ".env")))

	result, err := CopyWith(src, dest, CopyOptions{Refresh: true})
	require.NoError(t, err)

	assert.Empty(t, result.Copied)
	require.Len(t, result.Failed, 1)
	assert.Equal(t, ".env", result.Failed[0].Name)
	assert.ErrorIs(t, result.Failed[0].Err, ErrSourceNotRegular)

	_, err = os.Stat(filepath.Join(dest, ".env"))
	assert.True(t, os.IsNotExist(err))
}

func TestCopyWith_RejectsSymlinkedDestinationAndLeavesTargetIntact(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, ".env"), []byte("KEY=main"), 0600))
	outside := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.WriteFile(outside, []byte("KEY=outside"), 0600))
	require.NoError(t, os.Symlink(outside, filepath.Join(dest, ".env")))

	result, err := CopyWith(src, dest, CopyOptions{Refresh: true})
	require.NoError(t, err)

	require.Len(t, result.Failed, 1)
	assert.ErrorIs(t, result.Failed[0].Err, ErrDestinationNotRegular)

	got, err := os.ReadFile(outside)
	require.NoError(t, err)
	assert.Equal(t, "KEY=outside", string(got))
}

func TestCopyWith_RejectsNonRegularSource(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	requireFIFO(t, filepath.Join(src, ".env.pipe"))

	result, err := CopyWith(src, dest, CopyOptions{Refresh: true})
	require.NoError(t, err)

	require.Len(t, result.Failed, 1)
	assert.ErrorIs(t, result.Failed[0].Err, ErrSourceNotRegular)
}

func TestCopyWith_CreatedFileCarriesSourcePermissions(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, ".env"), []byte("KEY=main"), 0640))
	require.NoError(t, os.Chmod(filepath.Join(src, ".env"), 0640))

	_, err := CopyWith(src, dest, CopyOptions{})
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(dest, ".env"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0640), info.Mode().Perm())
}

func TestCopyWith_RefreshKeepsDestinationPermissions(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, ".env"), []byte("KEY=main"), 0644))
	require.NoError(t, os.Chmod(filepath.Join(src, ".env"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dest, ".env"), []byte("KEY=branch"), 0600))
	require.NoError(t, os.Chmod(filepath.Join(dest, ".env"), 0600))

	_, err := CopyWith(src, dest, CopyOptions{Refresh: true})
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(dest, ".env"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestCopyWith_OneFailureDoesNotStopTheOtherFiles(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, ".env"), []byte("KEY=main"), 0600))
	require.NoError(t, os.Symlink("/nowhere", filepath.Join(src, ".env.broken")))
	require.NoError(t, os.WriteFile(filepath.Join(src, ".env.local"), []byte("LOCAL=main"), 0600))

	result, err := CopyWith(src, dest, CopyOptions{Refresh: true})
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{".env", ".env.local"}, result.Copied)
	require.Len(t, result.Failed, 1)
	assert.Equal(t, ".env.broken", result.Failed[0].Name)
}

func TestCopy_ReplacesForCreation(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, ".env"), []byte("KEY=main"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dest, ".env"), []byte("KEY=stale"), 0600))

	result, err := Copy(src, dest)
	require.NoError(t, err)

	assert.Equal(t, []string{".env"}, result.Copied)
	got, err := os.ReadFile(filepath.Join(dest, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "KEY=main", string(got))
}

func TestCopyWith_UnreadableSourceDirectoryIsAWholeRunError(t *testing.T) {
	_, err := CopyWith(filepath.Join(t.TempDir(), "missing"), t.TempDir(), CopyOptions{})
	assert.Error(t, err)
}

// requireFIFO creates a named pipe, the simplest file that is neither regular
// nor a symlink.
func requireFIFO(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, syscall.Mkfifo(path, 0600))
}
