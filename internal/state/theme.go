// Package state manages TreeMan's user-specific persistent state.
package state

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shoutcape/treeman/internal/fsutil"
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
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, themeFileName), nil
}

// stateDir is the one directory TreeMan keeps user state in. Every state file
// in this package resolves through it, so they agree on where state lives.
//
// The result is absolute and canonical: state that is only reachable relative
// to a working directory is not one location, and a symlinked prefix would let
// the same configuration name different files at different times.
func stateDir() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home directory: %w", err)
		}
		base = filepath.Join(home, ".local", "state")
	}
	if !filepath.IsAbs(base) {
		return "", fmt.Errorf("state home must be absolute (check XDG_STATE_HOME or HOME): %q", base)
	}
	base, err := fsutil.CanonicalPath(base)
	if err != nil {
		return "", fmt.Errorf("canonicalize state home: %w", err)
	}
	return filepath.Join(base, "treeman"), nil
}
