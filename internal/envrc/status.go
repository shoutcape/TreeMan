// Package envrc reports activation tools for .envrc-based environments.
package envrc

import (
	"os"
	"os/exec"
	"path/filepath"
)

const (
	Unavailable          = "unavailable"
	Available            = "available"
	Active               = "active"
	ActiveInCurrentShell = "active (this shell)"
)

// ToolStatus describes the availability or active state of an .envrc tool.
type ToolStatus struct {
	Name   string
	Status string
}

// Detect reports direnv and Nix status when path contains an .envrc file.
func Detect(path string) []ToolStatus {
	return detect(path, exec.LookPath, os.Getenv)
}

func detect(path string, lookPath func(string) (string, error), getenv func(string) string) []ToolStatus {
	envrcPath := filepath.Join(path, ".envrc")
	info, err := os.Stat(envrcPath)
	if err != nil || info.IsDir() {
		return nil
	}

	absEnvrcPath, err := filepath.Abs(envrcPath)
	if err == nil {
		envrcPath = absEnvrcPath
	}

	direnvStatus := toolStatus(lookPath, "direnv", filepath.Clean(getenv("DIRENV_FILE")) == filepath.Clean(envrcPath))
	nixStatus := toolStatus(lookPath, "nix", getenv("IN_NIX_SHELL") != "")
	if nixStatus == Active {
		nixStatus = ActiveInCurrentShell
	}

	return []ToolStatus{
		{Name: "direnv", Status: direnvStatus},
		{Name: "Nix", Status: nixStatus},
	}
}

func toolStatus(lookPath func(string) (string, error), binary string, active bool) string {
	if _, err := lookPath(binary); err != nil {
		return Unavailable
	}
	if active {
		return Active
	}
	return Available
}
