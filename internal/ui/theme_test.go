package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestThemeRenderersPreserveText(t *testing.T) {
	tests := []struct {
		name     string
		rendered string
		want     string
	}{
		{name: "branch", rendered: RenderBranch("feature/forest"), want: "feature/forest"},
		{name: "path", rendered: RenderPath("/repo/.worktrees/forest"), want: "/repo/.worktrees/forest"},
		{name: "pr", rendered: RenderPR("#42"), want: "#42"},
		{name: "success", rendered: RenderTone(ToneSuccess, "CLEAN"), want: "CLEAN"},
		{name: "warning", rendered: RenderTone(ToneWarning, "DIRTY"), want: "DIRTY"},
		{name: "failure", rendered: RenderTone(ToneFailure, "FAILED"), want: "FAILED"},
		{name: "status", rendered: RenderStatus(ToneSuccess, "✓", "Worktree created"), want: "✓ Worktree created"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, StripANSI(test.rendered))
		})
	}
}

func TestPickerRowsStripToLookupText(t *testing.T) {
	assert.Equal(t, "worktrees/feature-forest                  feature/forest", StripANSI(WorktreeRow("/repo/worktrees/feature-forest", "feature/forest")))
	assert.Equal(t, "#42       feature/forest                    Improve picker styling", StripANSI(PRRow(42, "feature/forest", "Improve picker styling")))
	assert.Equal(t, "feature/forest                                      2026-08-18      #42", StripANSI(BranchRow("feature/forest", "2026-08-18", 42)))
}
