package cmd

import (
	"bytes"
	"testing"

	"github.com/shoutcape/treeman/internal/state"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThemePickerArgsRespectRichUICapability(t *testing.T) {
	richArgs := themePickerArgs(true, "/tmp/treeman")
	assert.Contains(t, richArgs, "--ansi")
	assert.Contains(t, richArgs, "--color="+ui.TransparentFZFColors())
	assert.Contains(t, richArgs, "--preview='/tmp/treeman' theme preview {1}")

	plainArgs := themePickerArgs(false, "")
	assert.Contains(t, plainArgs, "--no-color")
	assert.NotContains(t, plainArgs, "--ansi")
	assert.NotContains(t, plainArgs, "--color="+ui.TransparentFZFColors())
	assert.NotContains(t, plainArgs, "--preview-window=right:55%:wrap")
}

func TestSetThemePersistsInUserState(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Cleanup(func() { ui.SetTheme("forest") })

	cmd := &cobra.Command{}
	cmd.SetErr(&bytes.Buffer{})

	require.NoError(t, setTheme(cmd, "nord"))
	assert.Equal(t, "nord", state.Theme())
}
