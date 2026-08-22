package cmd

import (
	"io"
	"testing"

	"github.com/shoutcape/treeman/internal/terminal"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/stretchr/testify/assert"
)

func TestPickerSelectionIndex_DuplicateDisplayRows(t *testing.T) {
	display := ui.NewRenderer(io.Discard, terminal.Capabilities{}).WorktreeRow("/home/user/repo.feature-a", "feature/a")
	first := pickerRow(display, 0)
	second := pickerRow(display, 1)

	assert.Equal(t, 0, pickerSelectionIndex(first, 2))
	assert.Equal(t, 1, pickerSelectionIndex(second, 2))
}

func TestPickerSelectionIndex_InvalidIdentity(t *testing.T) {
	assert.Equal(t, -1, pickerSelectionIndex("worktree", 2))
	assert.Equal(t, -1, pickerSelectionIndex("worktree\t2", 2))
	assert.Equal(t, -1, pickerSelectionIndex("worktree\tnot-a-number", 2))
}

func TestPickerArgs_PreservePrompt(t *testing.T) {
	colorArgs := pickerArgs(true, " worktrees ", "switch > ")
	assert.Contains(t, colorArgs, "--prompt=switch > ")
	assert.Contains(t, colorArgs, "--ansi")
	assert.Contains(t, colorArgs, "--color="+ui.FZFColors())
	assert.Contains(t, colorArgs, "--height=40%")
	assert.Contains(t, colorArgs, "--layout=reverse")

	plainArgs := pickerArgs(false, " worktrees ", "switch > ")
	assert.Contains(t, plainArgs, "--no-color")
	assert.NotContains(t, plainArgs, "--ansi")
	assert.NotContains(t, plainArgs, "--color="+ui.FZFColors())
}
