package ui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
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

func TestTruncateTerminalText(t *testing.T) {
	assert.Equal(t, "feature...", ansi.Truncate("feature/very-long-branch", 10, "..."))
}

func TestRenderLinkStripsToLabel(t *testing.T) {
	link := RenderLink("PostgreSQL setup guide", "https://example.com")
	assert.Contains(t, link, ansi.SetHyperlink("https://example.com"))
	assert.Equal(t, "PostgreSQL setup guide", StripANSI(link))
}

func TestThemePreviewDoesNotChangeActiveTheme(t *testing.T) {
	SetTheme("forest")
	t.Cleanup(func() { SetTheme("forest") })

	preview, ok := ThemePreview("nord")
	assert.True(t, ok)
	assert.Contains(t, preview, "\x1b[")
	assert.Contains(t, StripANSI(preview), "TreeMan - nord")
	assert.Equal(t, "forest", CurrentTheme())
}

func TestThemeNamesAndAliases(t *testing.T) {
	assert.Contains(t, ThemeNames(), "catppuccin-mocha")
	assert.True(t, HasTheme("catppuccin"))
	assert.False(t, HasTheme("unknown"))
	assert.True(t, SetTheme("catppuccin"))
	assert.Equal(t, "catppuccin-mocha", CurrentTheme())
	SetTheme("forest")
}

func TestNordUsesOfficialMutedColor(t *testing.T) {
	assert.Equal(t, "#4C566A", themes["nord"].Muted)
}

func TestFZFColorsUseThemeBackground(t *testing.T) {
	SetTheme("dracula")
	t.Cleanup(func() { SetTheme("forest") })

	colors := FZFColors()
	assert.Contains(t, colors, "bg:#282A36")
	assert.Contains(t, colors, "bg+:#282A36")
	assert.Contains(t, colors, "preview-bg:#282A36")
	assert.Contains(t, colors, "border:#8BE9FD")
	assert.Contains(t, colors, "pointer:#50FA7B")
}

func TestTransparentFZFColorsUseTerminalBackground(t *testing.T) {
	SetTheme("dracula")
	t.Cleanup(func() { SetTheme("forest") })

	colors := TransparentFZFColors()
	assert.Contains(t, colors, "bg:-1")
	assert.Contains(t, colors, "bg+:-1")
	assert.Contains(t, colors, "preview-bg:-1")
}
