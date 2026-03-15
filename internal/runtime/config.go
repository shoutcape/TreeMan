// Package runtime manages per-worktree runtime isolation.
package runtime

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the contents of a .treeman.yml file.
type Config struct {
	Runtime RuntimeConfig `yaml:"runtime"`
}

// RuntimeConfig defines how a worktree's runtime should be started.
type RuntimeConfig struct {
	Type        string         `yaml:"type"`                   // "process" or "docker-compose"
	Command     string         `yaml:"command,omitempty"`      // command to run (type: process)
	ComposeFile string         `yaml:"compose_file,omitempty"` // compose file path (type: docker-compose)
	EnvFile     string         `yaml:"env_file,omitempty"`     // generated env file path (default: .env.treeman)
	Ports       map[string]int `yaml:"ports,omitempty"`        // logical port name → base port number
}

// LoadConfig reads and parses a .treeman.yml file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Apply defaults
	if cfg.Runtime.EnvFile == "" {
		cfg.Runtime.EnvFile = ".env.treeman"
	}

	return &cfg, nil
}

// Validate checks that the config is valid.
func (c *Config) Validate() error {
	switch c.Runtime.Type {
	case "process":
		if c.Runtime.Command == "" {
			return fmt.Errorf("runtime type 'process' requires a 'command' field")
		}
	case "docker-compose":
		if c.Runtime.ComposeFile == "" {
			return fmt.Errorf("runtime type 'docker-compose' requires a 'compose_file' field")
		}
	case "":
		return fmt.Errorf("runtime 'type' is required")
	default:
		return fmt.Errorf("unsupported runtime type: %q", c.Runtime.Type)
	}
	return nil
}
