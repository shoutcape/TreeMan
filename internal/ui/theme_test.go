package ui

import (
	"bytes"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/shoutcape/treeman/internal/terminal"
	"github.com/stretchr/testify/assert"
)

func TestThemeRenderersPreserveText(t *testing.T) {
	renderer := NewRenderer(&bytes.Buffer{}, terminal.Capabilities{Color: true})
	tests := []struct {
		name     string
		rendered string
		want     string
	}{
		{name: "branch", rendered: renderer.Branch("feature/forest"), want: "feature/forest"},
		{name: "path", rendered: renderer.Path("/repo/.worktrees/forest"), want: "/repo/.worktrees/forest"},
		{name: "pr", rendered: renderer.PR("#42"), want: "#42"},
		{name: "success", rendered: renderer.Tone(ToneSuccess, "CLEAN"), want: "CLEAN"},
		{name: "warning", rendered: renderer.Tone(ToneWarning, "DIRTY"), want: "DIRTY"},
		{name: "failure", rendered: renderer.Tone(ToneFailure, "FAILED"), want: "FAILED"},
		{name: "status", rendered: renderer.Status(ToneSuccess, "✓", "Worktree created"), want: "✓ Worktree created"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, StripANSI(test.rendered))
		})
	}
}

func TestRendererDoesNotEmitControlSequencesWithoutRichCapabilities(t *testing.T) {
	var output bytes.Buffer
	renderer := NewRenderer(&output, terminal.Capabilities{})

	rendered := renderer.Title("TreeMan") + " " + renderer.Link("docs", "https://example.com") + " " + renderer.Status(ToneSuccess, "✓", "ready")

	assert.NotContains(t, rendered, "\x1b")
	assert.Equal(t, "TreeMan docs ✓ ready", rendered)
}

func TestRendererHonorsNoColorAndDumbTerminalPolicy(t *testing.T) {
	for _, test := range []struct {
		name string
		env  string
		key  string
	}{
		{name: "no color", key: "NO_COLOR", env: "1"},
		{name: "dumb terminal", key: "TERM", env: "dumb"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", "")
			t.Setenv("TERM", "xterm-256color")
			t.Setenv(test.key, test.env)

			var output bytes.Buffer
			capabilities := terminal.Detect(&bytes.Buffer{}, &output)
			rendered := NewRenderer(&output, capabilities).Link("docs", "https://example.com")

			assert.False(t, capabilities.Color)
			assert.False(t, capabilities.Hyperlinks)
			assert.Equal(t, "docs", rendered)
			assert.NotContains(t, rendered, "\x1b")
		})
	}
}

func TestPickerRowsStripToLookupText(t *testing.T) {
	renderer := NewRenderer(&bytes.Buffer{}, terminal.Capabilities{})
	assert.Equal(t, "worktrees/feature-forest                  feature/forest", StripANSI(renderer.WorktreeRow("/repo/worktrees/feature-forest", "feature/forest")))
	assert.Equal(t, "#42       feature/forest                    Improve picker styling", StripANSI(renderer.PRRow(42, "feature/forest", "Improve picker styling")))
	assert.Equal(t, "feature/forest                                      2026-08-18      #42", StripANSI(renderer.BranchRow("feature/forest", "2026-08-18", 42)))
}

func TestTruncateTerminalText(t *testing.T) {
	assert.Equal(t, "feature...", ansi.Truncate("feature/very-long-branch", 10, "..."))
}

func TestRenderLinkStripsToLabel(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	link := NewRenderer(&bytes.Buffer{}, terminal.Detect(&bytes.Buffer{}, &bytes.Buffer{})).Link("PostgreSQL setup guide", "https://example.com")
	assert.NotContains(t, link, ansi.SetHyperlink("https://example.com"))
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
