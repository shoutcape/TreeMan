package fsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shoutcape/treeman/internal/fsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalPathResolvesSymlinkedExistingPrefix(t *testing.T) {
	realParent := filepath.Join(t.TempDir(), "real")
	require.NoError(t, os.Mkdir(realParent, 0o755))
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(realParent, link))

	got, err := fsutil.CanonicalPath(filepath.Join(link, "missing", "tree"))

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(realParent, "missing", "tree"), got)
}

func TestCanonicalPathMakesRelativePathAbsolute(t *testing.T) {
	got, err := fsutil.CanonicalPath(filepath.Join("missing", "tree"))

	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(got))
}
