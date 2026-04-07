package ghostty

import (
	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/terminal"
)

// Manager implements terminal.Manager for the Ghostty terminal emulator
// using AppleScript automation.
type Manager struct {
	layout *config.LayoutConfig
}

func init() {
	terminal.Register("ghostty", func(layout *config.LayoutConfig) terminal.Manager {
		return New(layout)
	})
}

// New creates a Ghostty Manager with the given layout configuration.
func New(layout *config.LayoutConfig) *Manager {
	return &Manager{layout: layout}
}

func (m *Manager) Open(wt terminal.WorktreeInfo) error {
	script := BuildOpenScript(wt, m.layout)
	_, err := runAppleScript(script)
	return err
}

func (m *Manager) Focus(wt terminal.WorktreeInfo) (bool, error) {
	script := BuildFocusScript(wt)
	out, err := runAppleScript(script)
	if err != nil {
		return false, err
	}
	return out == "found", nil
}

func (m *Manager) Close(wt terminal.WorktreeInfo) error {
	script := BuildCloseScript(wt)
	_, err := runAppleScript(script)
	return err
}

// Compile-time check that Manager satisfies terminal.Manager.
var _ terminal.Manager = (*Manager)(nil)
