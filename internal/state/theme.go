// Package state manages TreeMan's user-specific persistent state.
package state

import (
	"os"
	"path/filepath"
	"strings"
)

const themeFileName = "theme"

// Theme returns the saved theme, or "" when no theme has been selected.
func Theme() string {
	path, err := themePath()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// SaveTheme persists a selected theme outside project configuration.
func SaveTheme(theme string) error {
	path, err := themePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(theme+"\n"), 0o600)
}

func themePath() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "treeman", themeFileName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "treeman", themeFileName), nil
}
