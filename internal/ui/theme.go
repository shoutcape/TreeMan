package ui

import "github.com/charmbracelet/lipgloss"

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
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#6FB8D2"))
	headerStyle  = lipgloss.NewStyle().Bold(true).Faint(true)
	mutedStyle   = lipgloss.NewStyle().Faint(true)
	branchStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#B2B644"))
	pathStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#C4915E"))
	prStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#F2EA72"))
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7BD88F"))
	warningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F2A65A"))
	failureStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F05E5E"))
)

func RenderTitle(value string) string  { return titleStyle.Render(value) }
func RenderHeader(value string) string { return headerStyle.Render(value) }
func RenderMuted(value string) string  { return mutedStyle.Render(value) }
func RenderBranch(value string) string { return branchStyle.Render(value) }
func RenderPath(value string) string   { return pathStyle.Render(value) }
func RenderPR(value string) string     { return prStyle.Render(value) }

func RenderTone(tone Tone, value string) string {
	switch tone {
	case ToneSuccess:
		return successStyle.Render(value)
	case ToneWarning:
		return warningStyle.Render(value)
	case ToneFailure:
		return failureStyle.Render(value)
	case ToneInfo, ToneMuted:
		return mutedStyle.Render(value)
	default:
		return value
	}
}

// RenderStatus keeps symbols and color together so status does not rely on color.
func RenderStatus(tone Tone, symbol, message string) string {
	return RenderTone(tone, symbol) + " " + message
}
