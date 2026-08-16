package cmd

import (
	"testing"

	"github.com/shoutcape/treeman/internal/ui"
	"github.com/stretchr/testify/assert"
)

func TestPickerSelectionIndex_DuplicateDisplayRows(t *testing.T) {
	display := ui.WorktreeRow("/home/user/repo.feature-a", "feature/a")
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
	assert.Contains(t, pickerArgs(" worktrees ", "switch > "), "--prompt=switch > ")
}
