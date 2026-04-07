// Package terminal defines the Manager interface for terminal emulator
// integration. Concrete implementations live in sub-packages (e.g. ghostty).
package terminal

import "github.com/shoutcape/treeman/internal/config"

// Manager controls a terminal emulator: opening tabs, focusing existing
// sessions, and closing them.
type Manager interface {
	Open(wt WorktreeInfo) error
	Focus(wt WorktreeInfo) (bool, error)
	Close(wt WorktreeInfo) error
}

// WorktreeInfo carries the data a terminal integration needs to identify
// and set up a worktree session.
type WorktreeInfo struct {
	Path   string
	Branch string
	Slug   string
}

// Factory is a constructor that creates a Manager from a layout config.
type Factory func(layout *config.LayoutConfig) Manager

// registry maps terminal app names to their factory functions.
var registry = map[string]Factory{}

// Register associates a terminal app name with its factory function.
// Sub-packages call this in their init() to wire themselves in.
func Register(app string, f Factory) {
	registry[app] = f
}

// NewManager returns a Manager for the configured terminal app, or nil
// when terminal integration is not configured or the app is unknown.
func NewManager(cfg *config.TerminalConfig) Manager {
	if cfg == nil || cfg.App == "" {
		return nil
	}
	f, ok := registry[cfg.App]
	if !ok {
		return nil
	}
	return f(cfg.Layout)
}
