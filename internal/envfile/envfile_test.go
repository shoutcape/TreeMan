package envfile

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/shoutcape/treeman/internal/fsutil"
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

func TestCopyWith_SkipNeverWritesDestination(t *testing.T) {
	for _, present := range []bool{false, true} {
		for _, refresh := range []bool{false, true} {
			t.Run(fmt.Sprintf("present=%t/refresh=%t", present, refresh), func(t *testing.T) {
				src, dest := t.TempDir(), t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(src, ".env"), []byte("KEY=main"), 0o600))
				require.NoError(t, os.WriteFile(filepath.Join(src, ".env.local"), []byte("LOCAL=main"), 0o600))
				if present {
					require.NoError(t, os.WriteFile(filepath.Join(dest, ".env"), []byte("KEY=branch"), 0o600))
				}

				result, err := CopyWith(src, dest, CopyOptions{Refresh: refresh, Skip: []string{".env"}})
				require.NoError(t, err)

				assert.Equal(t, []string{".env"}, result.Skipped)
				assert.Equal(t, []string{".env.local"}, result.Copied)
				assert.Empty(t, result.Preserved)
				assert.Empty(t, result.Failed)
				if present {
					data, err := os.ReadFile(filepath.Join(dest, ".env"))
					require.NoError(t, err)
					assert.Equal(t, "KEY=branch", string(data))
				} else {
					assert.NoFileExists(t, filepath.Join(dest, ".env"))
				}
			})
		}
	}
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

func TestCopyWith_PrepareFailureDoesNotWriteAndContinues(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, ".env"), []byte("URI=one"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(src, ".env.local"), []byte("URI=two"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dest, ".env"), []byte("old"), 0o600))

	result, err := CopyWith(src, dest, CopyOptions{
		Refresh: true,
		Prepare: func(name string, data []byte) ([]byte, error) {
			if name == ".env" {
				return nil, fmt.Errorf("prepare %s", name)
			}
			return append(data, []byte(" prepared")...), nil
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Failed, 1)
	assert.Equal(t, ".env", result.Failed[0].Name)
	assert.Equal(t, []string{".env.local"}, result.Copied)

	data, err := os.ReadFile(filepath.Join(dest, ".env"))
	require.NoError(t, err)
	assert.Equal(t, []byte("old"), data)
	data, err = os.ReadFile(filepath.Join(dest, ".env.local"))
	require.NoError(t, err)
	assert.Equal(t, []byte("URI=two prepared"), data)
}

func TestCopyWith_PrepareReadsSourceOnceAndWritesPreparedDataOnce(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, ".env"), []byte("URI=source"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dest, ".env"), []byte("old"), 0o600))

	writeCalls := 0
	result, err := copyWith(src, dest, CopyOptions{
		Refresh: true,
		Prepare: func(name string, data []byte) ([]byte, error) {
			require.Equal(t, ".env", name)
			require.Equal(t, []byte("URI=source"), data)
			require.NoError(t, os.WriteFile(filepath.Join(src, ".env"), []byte("URI=mutated"), 0o600))
			return []byte("URI=prepared"), nil
		},
	}, func(path string, data []byte, mode os.FileMode) error {
		writeCalls++
		return fsutil.AtomicWriteFile(path, data, mode)
	})
	require.NoError(t, err)
	assert.Equal(t, []string{".env"}, result.Copied)
	assert.Equal(t, 1, writeCalls)

	data, err := os.ReadFile(filepath.Join(dest, ".env"))
	require.NoError(t, err)
	assert.Equal(t, []byte("URI=prepared"), data)
}

func TestCopyWith_PublicationFailuresNeverPublishUnpreparedSource(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	source := []byte("DATABASE_URL=postgres://app@localhost/main\nSETTING=new\n")
	previous := []byte("DATABASE_URL=postgres://app@localhost/owned\nSETTING=old\n")
	prepared := []byte("DATABASE_URL=postgres://app@localhost/owned\nSETTING=new\n")
	require.NoError(t, os.WriteFile(filepath.Join(src, ".env"), source, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dest, ".env"), previous, 0o600))

	writeErr := fmt.Errorf("writer failed")
	result, err := copyWith(src, dest, CopyOptions{
		Refresh: true,
		Prepare: func(string, []byte) ([]byte, error) { return prepared, nil },
	}, func(string, []byte, os.FileMode) error {
		return writeErr
	})
	require.NoError(t, err)
	require.Len(t, result.Failed, 1)
	assert.ErrorIs(t, result.Failed[0].Err, writeErr)
	data, err := os.ReadFile(filepath.Join(dest, ".env"))
	require.NoError(t, err)
	assert.Equal(t, previous, data)

	result, err = copyWith(src, dest, CopyOptions{
		Refresh: true,
		Prepare: func(string, []byte) ([]byte, error) { return prepared, nil },
	}, func(path string, data []byte, mode os.FileMode) error {
		require.Equal(t, prepared, data)
		require.NoError(t, fsutil.AtomicWriteFile(path, data, mode))
		return writeErr
	})
	require.NoError(t, err)
	require.Len(t, result.Failed, 1)
	assert.ErrorIs(t, result.Failed[0].Err, writeErr)
	data, err = os.ReadFile(filepath.Join(dest, ".env"))
	require.NoError(t, err)
	assert.Equal(t, prepared, data)
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

func TestCreateFile_ConcurrentDestinationIsNeverOverwritten(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		t.Run(fmt.Sprintf("refresh=%t", refresh), func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), ".env")
			require.NoError(t, os.WriteFile(dest, []byte("KEY=concurrent"), 0o600))

			// Simulate a destination arriving after copyFile's initial Lstat.
			outcome, err := createFile(dest, []byte("KEY=main"), 0o644, refresh)
			if refresh {
				assert.ErrorIs(t, err, os.ErrExist)
			} else {
				require.NoError(t, err)
				assert.Equal(t, outcomePreserved, outcome)
			}
			data, err := os.ReadFile(dest)
			require.NoError(t, err)
			assert.Equal(t, "KEY=concurrent", string(data))
			info, err := os.Stat(dest)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		})
	}
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
