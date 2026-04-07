// Package config handles loading .treeman.toml project configuration.
//
// The config file is optional. When absent, all features requiring config
// (such as per-branch database management) are silently disabled.
// Parse errors are surfaced as warnings rather than hard failures so that
// a malformed config never prevents worktree creation.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// ConfigFileName is the name of the config file searched for.
const ConfigFileName = ".treeman.toml"

// GlobalConfigFileName is the name of the global config file.
const GlobalConfigFileName = "config.toml"

// Config holds the full project-level configuration from .treeman.toml.
type Config struct {
	// Database configures per-branch database management.
	// Nil when the [database] section is absent (feature disabled).
	Database *DatabaseConfig `toml:"database"`

	// Hooks configures lifecycle hooks (commands to run at various stages).
	// Nil when the [hooks] section is absent (no custom hooks).
	Hooks *HooksConfig `toml:"hooks"`

	// Terminal configures terminal emulator integration.
	// Nil when the [terminal] section is absent (feature disabled).
	Terminal *TerminalConfig `toml:"terminal"`
}

// HooksConfig configures lifecycle hook commands.
type HooksConfig struct {
	// PostCreate is a list of shell commands to run after a worktree is
	// created. Commands run sequentially in the new worktree directory.
	// Failures are treated as warnings (best-effort).
	PostCreate []string `toml:"post_create"`
}

// TerminalConfig configures terminal emulator integration.
type TerminalConfig struct {
	App    string        `toml:"app"`
	Layout *LayoutConfig `toml:"layout"`
}

// LayoutConfig defines the pane layout for terminal integration.
type LayoutConfig struct {
	Splits []SplitConfig `toml:"splits"`
}

// SplitConfig defines a single terminal pane split.
type SplitConfig struct {
	Direction string `toml:"direction"`
	Command   string `toml:"command"`
}

// DatabaseConfig configures per-branch database management.
type DatabaseConfig struct {
	// EnvKey is the environment variable name that holds the database URI
	// (e.g. "DATABASE_URI", "DATABASE_URL"). Required when [database] is present.
	EnvKey string `toml:"env_key"`
}

// DatabaseEnvKey returns the configured env variable name for the database URI.
// Returns "" if database management is not configured.
func (c Config) DatabaseEnvKey() string {
	if c.Database == nil {
		return ""
	}
	return c.Database.EnvKey
}

// PostCreateHooks returns the list of post-create hook commands.
// Returns nil if no hooks are configured.
func (c Config) PostCreateHooks() []string {
	if c.Hooks == nil {
		return nil
	}
	return c.Hooks.PostCreate
}

// LoadResult holds the outcome of loading a config file.
type LoadResult struct {
	// Config is the parsed configuration. Zero value when no config was found
	// or a parse error occurred.
	Config Config
	// Path is the absolute path to the config file that was loaded.
	// Empty when no config was found.
	Path string
	// Warning is set when the config file was found but could not be parsed.
	// Callers should display this to the user but not treat it as a hard error.
	Warning string
}

// Load searches for .treeman.toml starting from dir and walking up to the
// filesystem root. It returns the first config found or a zero LoadResult
// if none exists.
//
// Parse errors are returned as warnings in LoadResult.Warning rather than
// as errors, so a malformed config never blocks worktree operations.
func Load(dir string) LoadResult {
	path := findConfig(dir)
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

	return LoadResult{
		Config: cfg,
		Path:   path,
	}
}

// LoadGlobal loads the global config from configDir/config.toml.
// If configDir is empty, it falls back to $XDG_CONFIG_HOME/treeman or
// $HOME/.config/treeman. Returns a zero LoadResult if no global config
// exists.
func LoadGlobal(configDir string) LoadResult {
	if configDir == "" {
		configDir = defaultGlobalConfigDir()
		if configDir == "" {
			return LoadResult{}
		}
	}
	path := filepath.Join(configDir, GlobalConfigFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return LoadResult{}
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return LoadResult{
			Path:    path,
			Warning: fmt.Sprintf("could not parse %s: %v", path, err),
		}
	}
	return LoadResult{Config: cfg, Path: path}
}

// defaultGlobalConfigDir returns the directory for the global config file,
// using $XDG_CONFIG_HOME/treeman or falling back to $HOME/.config/treeman.
func defaultGlobalConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "treeman")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "treeman")
}

// MergeTerminalConfig merges global and project terminal configs.
// Project-level fields override global when set, but unset project fields
// fall back to global values. This allows the global config to set
// app = "ghostty" while the project config only specifies a layout.
func MergeTerminalConfig(global, project *TerminalConfig) *TerminalConfig {
	if global == nil && project == nil {
		return nil
	}
	if global == nil {
		return project
	}
	if project == nil {
		return global
	}

	merged := &TerminalConfig{
		App:    global.App,
		Layout: global.Layout,
	}
	if project.App != "" {
		merged.App = project.App
	}
	if project.Layout != nil {
		merged.Layout = project.Layout
	}
	return merged
}

// findConfig walks from dir upward looking for ConfigFileName.
// Returns the absolute path of the first match, or "" if not found.
func findConfig(dir string) string {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}

	for {
		candidate := filepath.Join(absDir, ConfigFileName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}

		parent := filepath.Dir(absDir)
		if parent == absDir {
			// Reached filesystem root.
			return ""
		}
		absDir = parent
	}
}
