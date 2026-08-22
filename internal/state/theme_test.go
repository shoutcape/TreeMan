package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThemeReadsSavedValue(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	assert.Equal(t, "", Theme())
	require.NoError(t, SaveTheme("nord"))
	assert.Equal(t, "nord", Theme())

	data, err := os.ReadFile(filepath.Join(os.Getenv("XDG_STATE_HOME"), "treeman", themeFileName))
	require.NoError(t, err)
	assert.Equal(t, "nord\n", string(data))
}

func TestThemeFallsBackToUserStateDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", home)

	require.NoError(t, SaveTheme("dracula"))
	assert.Equal(t, "dracula", Theme())
	assert.FileExists(t, filepath.Join(home, ".local", "state", "treeman", themeFileName))
}
