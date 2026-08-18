package ui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

// Tone describes the semantic emphasis of a human-facing status.
type Tone int

const (
	ToneMuted Tone = iota
	ToneSuccess
	ToneWarning
	ToneFailure
	ToneInfo
)

var (
	renderer     = lipgloss.NewRenderer(os.Stderr)
	titleStyle   = renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("#6FB8D2"))
	headerStyle  = renderer.NewStyle().Bold(true).Faint(true)
	mutedStyle   = renderer.NewStyle().Faint(true)
	branchStyle  = renderer.NewStyle().Foreground(lipgloss.Color("#B2B644"))
	pathStyle    = renderer.NewStyle().Foreground(lipgloss.Color("#C4915E"))
	linkStyle    = renderer.NewStyle().Foreground(lipgloss.Color("#6FB8D2")).Underline(true)
	prStyle      = renderer.NewStyle().Foreground(lipgloss.Color("#F2EA72"))
	infoStyle    = renderer.NewStyle().Foreground(lipgloss.Color("#6FB8D2"))
	successStyle = renderer.NewStyle().Foreground(lipgloss.Color("#7BD88F"))
	warningStyle = renderer.NewStyle().Foreground(lipgloss.Color("#F2A65A"))
	failureStyle = renderer.NewStyle().Foreground(lipgloss.Color("#F05E5E"))
)

func RenderTitle(value string) string  { return titleStyle.Render(value) }
func RenderHeader(value string) string { return headerStyle.Render(value) }
func RenderMuted(value string) string  { return mutedStyle.Render(value) }
func RenderBranch(value string) string { return branchStyle.Render(value) }
func RenderPath(value string) string   { return pathStyle.Render(value) }
func RenderPR(value string) string     { return prStyle.Render(value) }

// RenderLink renders a concise clickable label in terminals that support OSC 8 links.
func RenderLink(label, url string) string {
	return ansi.SetHyperlink(url) + linkStyle.Render(label) + ansi.ResetHyperlink()
}

func RenderTone(tone Tone, value string) string {
	switch tone {
	case ToneSuccess:
		return successStyle.Render(value)
	case ToneWarning:
		return warningStyle.Render(value)
	case ToneFailure:
		return failureStyle.Render(value)
	case ToneInfo:
		return infoStyle.Render(value)
	case ToneMuted:
		return mutedStyle.Render(value)
	default:
		return value
	}
}

// RenderStatus keeps symbols and color together so status does not rely on color.
func RenderStatus(tone Tone, symbol, message string) string {
	return RenderTone(tone, symbol) + " " + FitToTerminal(message, 2)
}

// FitToTerminal truncates human-facing text to leave room for its fixed prefix.
// It preserves the complete value when stderr is not an interactive terminal.
func FitToTerminal(value string, reserved int) string {
	width, _, err := term.GetSize(os.Stderr.Fd())
	if err != nil || width <= 0 {
		return value
	}
	available := width - reserved
	if available <= 3 {
		return ansi.Truncate(value, max(1, available), "")
	}
	return ansi.Truncate(value, available, "...")
}
