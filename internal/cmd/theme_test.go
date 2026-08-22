package cmd

import (
	"testing"

	"github.com/shoutcape/treeman/internal/ui"
	"github.com/stretchr/testify/assert"
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
