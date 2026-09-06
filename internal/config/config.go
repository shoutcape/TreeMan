// Package config handles loading .treeman.toml project configuration.
//
// The config file is optional. When absent, all features requiring config
// (such as per-branch database management) are silently disabled.
// A config file that exists but cannot be read, parsed, or validated is
// reported as a warning rather than an error, so read-only commands can
// describe the problem. Creation flows refuse to run on a warning: the file
// may place worktrees somewhere other than the default, and creating one in
// the default location instead would silently ignore what the user asked for.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// ConfigFileName is the name of the config file searched for.
const ConfigFileName = ".treeman.toml"

// Config holds the full project-level configuration from .treeman.toml.
type Config struct {
	// Database configures per-branch database management.
	// Nil when the [database] section is absent (feature disabled).
	Database *DatabaseConfig `toml:"database"`

	// Hooks configures lifecycle hooks (commands to run at various stages).
	// Nil when the [hooks] section is absent (no custom hooks).
	Hooks *HooksConfig `toml:"hooks"`

	// UpdateGitignore controls whether TreeMan appends the worktree directory
	// to the root .gitignore when creating a worktree. Defaults to false so
	// that projects managing their own .gitignore are not modified without
	// consent.
	UpdateGitignore bool `toml:"update_gitignore"`

	// WorktreeDir is the parent directory new worktrees are created in.
	// Empty means the default, ".worktrees" inside the main worktree.
	// The value is resolved by the worktree package, which owns the
	// placeholder, tilde, and relative-path rules.
	WorktreeDir string `toml:"worktree_dir"`
}

// HooksConfig configures lifecycle hook commands.
type HooksConfig struct {
	// PostCreate is a list of shell commands to run after a worktree is
	// created. Commands run sequentially in the new worktree directory.
	// Failures are treated as warnings (best-effort).
	PostCreate []string `toml:"post_create"`
}

// DatabaseConfig configures per-branch database management.
type DatabaseConfig struct {
	// EnvKey is the environment variable name that holds the database URI
	// (e.g. "DATABASE_URI", "DATABASE_URL"). Required when [database] is present.
	EnvKey string `toml:"env_key"`
	// Container optionally names the running Docker container that hosts
	// PostgreSQL. Without it, TreeMan requires one unambiguous local port match.
	Container string `toml:"container"`
}

// DatabaseEnvKey returns the configured env variable name for the database URI.
// Returns "" if database management is not configured.
func (c Config) DatabaseEnvKey() string {
	if c.Database == nil {
		return ""
	}
	return c.Database.EnvKey
}

// DatabaseContainer returns the optional configured PostgreSQL container.
func (c Config) DatabaseContainer() string {
	if c.Database == nil {
		return ""
	}
	return c.Database.Container
}

// PostCreateHooks returns the list of post-create hook commands.
// Returns nil if no hooks are configured.
func (c Config) PostCreateHooks() []string {
	if c.Hooks == nil {
		return nil
	}
	return c.Hooks.PostCreate
}

// ShouldUpdateGitignore reports whether TreeMan should append the worktree
// directory to the root .gitignore when creating a worktree.
func (c Config) ShouldUpdateGitignore() bool {
	return c.UpdateGitignore
}

// WorktreeDirSetting returns the configured worktree parent directory exactly
// as written. Empty means the caller should apply the default.
func (c Config) WorktreeDirSetting() string {
	return c.WorktreeDir
}

// LoadResult holds the outcome of loading a config file.
type LoadResult struct {
	// Config is the parsed configuration. Zero value when no config was found
	// or a parse error occurred.
	Config Config
	// Path is the absolute path to the config file that was loaded.
	// Empty when no config was found.
	Path string
	// Warning is set when configuration could not be discovered, read, parsed,
	// or validated. Read-only commands display it; creation commands stop.
	Warning string
}

// Load searches for .treeman.toml starting from dir and walking up to the
// filesystem root. It returns the first config found or a zero LoadResult
// if none exists.
//
// Configuration errors are returned as warnings in LoadResult.Warning rather
// than as errors. Creation commands promote the warning to an error because
// falling back could place a worktree somewhere the user did not request.
func Load(dir string) LoadResult {
	path, err := findConfig(dir)
	if err != nil {
		return LoadResult{
			Warning: fmt.Sprintf("could not search for %s from %s: %v", ConfigFileName, dir, err),
		}
	}
	if path == "" {
		return LoadResult{}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return LoadResult{
			Path:    path,
			Warning: fmt.Sprintf("could not read %s: %v", path, err),
		}
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return LoadResult{
			Path:    path,
			Warning: fmt.Sprintf("could not parse %s: %v", path, err),
		}
	}

	// Validate: if [database] is present, env_key is required.
	if cfg.Database != nil && cfg.Database.EnvKey == "" {
		return LoadResult{
			Path:    path,
			Warning: fmt.Sprintf("%s: [database] section requires env_key", path),
		}
	}
	if cfg.Database != nil && cfg.Database.Container != "" && strings.TrimSpace(cfg.Database.Container) == "" {
		return LoadResult{
			Path:    path,
			Warning: fmt.Sprintf("%s: [database].container cannot be whitespace", path),
		}
	}

	return LoadResult{
		Config: cfg,
		Path:   path,
	}
}

// findConfig walks from dir upward looking for ConfigFileName.
// Returns the absolute path of the first match, or "" if not found.
//
// Only a genuine "not there" answer continues the walk. A directory that
// cannot be searched is reported instead, because treating an unreadable
// ancestor as an absent config would hide a file the user did write and let
// creation fall back to defaults the user configured away from.
func findConfig(dir string) (string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	for {
		candidate := filepath.Join(absDir, ConfigFileName)
		switch _, err := os.Stat(candidate); {
		case err == nil:
			return candidate, nil
		case !errors.Is(err, fs.ErrNotExist):
			return "", err
		}

		parent := filepath.Dir(absDir)
		if parent == absDir {
			// Reached filesystem root.
			return "", nil
		}
		absDir = parent
	}
}
