package ui

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/shoutcape/treeman/internal/terminal"
)

// Palette maps a theme's colors to TreeMan's semantic output roles.
// Background is retained as a reference but never painted by TreeMan.
type Palette struct {
	Background string
	Foreground string
	Title      string
	Branch     string
	Path       string
	PR         string
	Success    string
	Warning    string
	Failure    string
	Info       string
	Muted      string
}

var themes = map[string]Palette{
	"forest":           {"", "#D8D5C4", "#6FB8D2", "#B2B644", "#C4915E", "#F2EA72", "#7BD88F", "#F2A65A", "#F05E5E", "#6FB8D2", "#FFFFFF"},
	"dracula":          {"#282A36", "#F8F8F2", "#8BE9FD", "#50FA7B", "#BD93F9", "#F1FA8C", "#50FA7B", "#FFB86C", "#FF5555", "#8BE9FD", "#6272A4"},
	"catppuccin-mocha": {"#1E1E2E", "#CDD6F4", "#89DCEB", "#A6E3A1", "#CBA6F7", "#F9E2AF", "#A6E3A1", "#FAB387", "#F38BA8", "#89B4FA", "#6C7086"},
	"nord":             {"#2E3440", "#D8DEE9", "#88C0D0", "#A3BE8C", "#81A1C1", "#EBCB8B", "#A3BE8C", "#D08770", "#BF616A", "#88C0D0", "#4C566A"},
	"gruvbox":          {"#282828", "#EBDBB2", "#83A598", "#B8BB26", "#D79921", "#FABD2F", "#B8BB26", "#FE8019", "#FB4934", "#83A598", "#928374"},
	"tokyo-night":      {"#1A1B26", "#C0CAF5", "#7DCFFF", "#9ECE6A", "#BB9AF7", "#E0AF68", "#9ECE6A", "#FF9E64", "#F7768E", "#7AA2F7", "#565F89"},
	"one-dark":         {"#282C34", "#ABB2BF", "#56B6C2", "#98C379", "#C678DD", "#E5C07B", "#98C379", "#D19A66", "#E06C75", "#61AFEF", "#5C6370"},
	"solarized-dark":   {"#002B36", "#839496", "#2AA198", "#859900", "#268BD2", "#B58900", "#859900", "#CB4B16", "#DC322F", "#268BD2", "#586E75"},
	"solarized-light":  {"#FDF6E3", "#657B83", "#2AA198", "#859900", "#268BD2", "#B58900", "#859900", "#CB4B16", "#DC322F", "#268BD2", "#93A1A1"},
}

var aliases = map[string]string{"catppuccin": "catppuccin-mocha"}

// Tone describes the semantic emphasis of a human-facing status.
type Tone int

const (
	ToneMuted Tone = iota
	ToneSuccess
	ToneWarning
	ToneFailure
	ToneInfo
)

var currentTheme = "forest"

type themeStyles struct {
	title, header, muted, branch, path, link, pr, info, success, warning, failure lipgloss.Style
}

// ThemeNames returns the supported canonical theme names.
func ThemeNames() []string {
	names := make([]string, 0, len(themes))
	for name := range themes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// CurrentTheme returns the active canonical theme name.
func CurrentTheme() string { return currentTheme }

// FZFColors returns an fzf color definition using the active semantic palette.
func FZFColors() string {
	return fzfColors(false)
}

// TransparentFZFColors returns themed fzf chrome with terminal-default panes.
func TransparentFZFColors() string {
	return fzfColors(true)
}

func fzfColors(transparent bool) string {
	p, ok := themePalette(currentTheme)
	if !ok {
		p = themes["forest"]
	}
	background := p.Background
	if transparent || background == "" {
		background = "-1"
	}
	return strings.Join([]string{
		"fg:" + p.Foreground,
		"fg+:" + p.Foreground,
		"bg:" + background,
		"bg+:" + background,
		"preview-bg:" + background,
		"hl:" + p.Branch,
		"hl+:" + p.Branch,
		"border:" + p.Title,
		"label:" + p.Title,
		"prompt:" + p.Info,
		"pointer:" + p.Success,
		"marker:" + p.Warning,
		"spinner:" + p.Info,
		"header:" + p.Muted,
		"info:" + p.Muted,
	}, ",")
}

// HasTheme reports whether name is a supported theme or alias.
func HasTheme(name string) bool {
	_, ok := themePalette(name)
	return ok
}

// SetTheme applies a named palette. It returns false and applies Forest for an
// unknown or empty name, ensuring presentation configuration never blocks work.
func SetTheme(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if alias, ok := aliases[name]; ok {
		name = alias
	}
	_, ok := themes[name]
	if !ok {
		name = "forest"
	}
	currentTheme = name
	return ok
}

func newThemeStylesFor(r *lipgloss.Renderer, p Palette) themeStyles {
	return themeStyles{
		title:   r.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Title)),
		header:  r.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Foreground)),
		muted:   r.NewStyle().Foreground(lipgloss.Color(p.Muted)),
		branch:  r.NewStyle().Foreground(lipgloss.Color(p.Branch)),
		path:    r.NewStyle().Foreground(lipgloss.Color(p.Path)),
		link:    r.NewStyle().Foreground(lipgloss.Color(p.Title)).Underline(true),
		pr:      r.NewStyle().Foreground(lipgloss.Color(p.PR)),
		info:    r.NewStyle().Foreground(lipgloss.Color(p.Info)),
		success: r.NewStyle().Foreground(lipgloss.Color(p.Success)),
		warning: r.NewStyle().Foreground(lipgloss.Color(p.Warning)),
		failure: r.NewStyle().Foreground(lipgloss.Color(p.Failure)),
	}
}

// Renderer applies one immutable palette to one output stream.
type Renderer struct {
	capabilities terminal.Capabilities
	styles       themeStyles
}

// NewRenderer creates a renderer for writer using the supplied stream policy.
func NewRenderer(writer io.Writer, capabilities terminal.Capabilities) Renderer {
	renderer := lipgloss.NewRenderer(writer)
	if !capabilities.Color {
		renderer.SetColorProfile(termenv.Ascii)
	}
	p, ok := themePalette(CurrentTheme())
	if !ok {
		p = themes["forest"]
	}
	return Renderer{capabilities: capabilities, styles: newThemeStylesFor(renderer, p)}
}

func (r Renderer) Title(value string) string  { return r.styles.title.Render(value) }
func (r Renderer) Header(value string) string { return r.styles.header.Render(value) }
func (r Renderer) Muted(value string) string  { return r.styles.muted.Render(value) }
func (r Renderer) Branch(value string) string { return r.styles.branch.Render(value) }
func (r Renderer) Path(value string) string   { return r.styles.path.Render(value) }
func (r Renderer) PR(value string) string     { return r.styles.pr.Render(value) }

// Link renders a concise clickable label only when the destination supports it.
func (r Renderer) Link(label, url string) string {
	if !r.capabilities.Hyperlinks {
		return label
	}
	return ansi.SetHyperlink(url) + r.styles.link.Render(label) + ansi.ResetHyperlink()
}

func (r Renderer) Tone(tone Tone, value string) string {
	switch tone {
	case ToneSuccess:
		return r.styles.success.Render(value)
	case ToneWarning:
		return r.styles.warning.Render(value)
	case ToneFailure:
		return r.styles.failure.Render(value)
	case ToneInfo:
		return r.styles.info.Render(value)
	case ToneMuted:
		return r.styles.muted.Render(value)
	default:
		return value
	}
}

// Status keeps symbols and color together so status does not rely on color.
func (r Renderer) Status(tone Tone, symbol, message string) string {
	return r.Tone(tone, symbol) + " " + r.Fit(message, 2)
}

// Fit truncates human-facing text only when the destination has a known width.
func (r Renderer) Fit(value string, reserved int) string {
	if r.capabilities.Width <= 0 {
		return value
	}
	available := r.capabilities.Width - reserved
	if available <= 3 {
		return ansi.Truncate(value, max(1, available), "")
	}
	return ansi.Truncate(value, available, "...")
}

// ThemePickerRow renders a compact, non-mutating sample for fzf. It forces
// color because fzf is only launched for interactive rich-terminal sessions.
func ThemePickerRow(name string, current bool) string {
	p, ok := themePalette(name)
	if !ok {
		return name
	}
	previewRenderer := lipgloss.NewRenderer(os.Stdout)
	previewRenderer.SetColorProfile(termenv.TrueColor)
	s := newThemeStylesFor(previewRenderer, p)
	marker := " "
	if current {
		marker = "*"
	}
	return fmt.Sprintf("%s %-18s %s  %s  %s  %s", marker, name,
		s.success.Render("✓ CLEAN"), s.branch.Render("feature/ui"), s.path.Render("~/.worktrees/ui"), s.pr.Render("#42"))
}

// ThemePreview renders a full non-mutating theme sample for fzf's preview pane.
// It intentionally forces color for that isolated fzf preview protocol.
func ThemePreview(name string) (string, bool) {
	p, ok := themePalette(name)
	if !ok {
		return "", false
	}
	previewRenderer := lipgloss.NewRenderer(os.Stdout)
	previewRenderer.SetColorProfile(termenv.TrueColor)
	s := newThemeStylesFor(previewRenderer, p)
	return strings.Join([]string{
		s.title.Render("TreeMan - " + displayThemeName(name)),
		"",
		s.header.Render("WORKTREES"),
		"",
		"  " + s.success.Render("✓ CLEAN") + "   " + s.branch.Render("main") + "                 " + s.path.Render("~/Projects/TreeMan"),
		"  " + s.warning.Render("! DIRTY") + "   " + s.branch.Render("feature/themes") + "       " + s.path.Render("~/.worktrees/themes"),
		"  " + s.success.Render("✓ MERGED") + "  " + s.branch.Render("fix/config") + "            " + s.path.Render("~/.worktrees/config"),
		"",
		s.header.Render("DIAGNOSTICS"),
		"",
		"  " + s.success.Render("✓ Repository") + "       " + s.muted.Render("Git repository detected"),
		"  " + s.muted.Render("○ Database") + "         " + s.muted.Render("Not configured"),
		"  " + s.warning.Render("! Docker") + "           " + s.failure.Render("Daemon unavailable"),
		"",
		"  " + s.info.Render("Branch") + "       " + s.branch.Render("feature/themes"),
		"  " + s.info.Render("Path") + "         " + s.path.Render("~/.worktrees/themes"),
		"  " + s.info.Render("Pull Request") + " " + s.pr.Render("#42") + "  " + s.muted.Render("Ready for review"),
	}, "\n"), true
}

func themePalette(name string) (Palette, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if alias, ok := aliases[name]; ok {
		name = alias
	}
	p, ok := themes[name]
	return p, ok
}

func displayThemeName(name string) string {
	return strings.Join(strings.FieldsFunc(name, func(r rune) bool { return r == '-' }), " ")
}
