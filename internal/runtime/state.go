package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RuntimeState holds the persisted runtime metadata for a single worktree.
type RuntimeState struct {
	Repo               string         `json:"repo"`
	RepoRoot           string         `json:"repo_root"`
	Branch             string         `json:"branch"`
	BranchSlug         string         `json:"branch_slug"`
	WorktreePath       string         `json:"worktree_path"`
	RuntimeType        string         `json:"runtime_type"`
	Status             string         `json:"status"` // "running", "stopped", "stale"
	PID                int            `json:"pid,omitempty"`
	Command            string         `json:"command,omitempty"`
	Ports              map[string]int `json:"ports,omitempty"`
	EnvFile            string         `json:"env_file,omitempty"`
	LogFile            string         `json:"log_file,omitempty"`
	ComposeProjectName string         `json:"compose_project_name,omitempty"`
	ComposeFile        string         `json:"compose_file,omitempty"`
	StartedAt          time.Time      `json:"started_at,omitempty"`
	StoppedAt          time.Time      `json:"stopped_at,omitempty"`
}

// stateDir returns the state directory for a given repo.
// ~/.treeman/state/<repo-basename>/
func stateDir(repo string) string {
	return filepath.Join(tremanDir(), "state", repo)
}

// stateFilePath returns the path to the state file for a repo/branch.
// ~/.treeman/state/<repo-basename>/<branch-slug>.json
func stateFilePath(repo, branchSlug string) string {
	return filepath.Join(stateDir(repo), branchSlug+".json")
}

// logDir returns the log directory for a given repo.
// ~/.treeman/logs/<repo-basename>/
func logDir(repo string) string {
	return filepath.Join(tremanDir(), "logs", repo)
}

// LogFilePath returns the path to the log file for a repo/branch.
// ~/.treeman/logs/<repo-basename>/<branch-slug>.log
func LogFilePath(repo, branchSlug string) string {
	return filepath.Join(logDir(repo), branchSlug+".log")
}

// EnvFilePath returns the absolute path to the generated env file.
func EnvFilePath(state *RuntimeState) (string, error) {
	if state == nil {
		return "", fmt.Errorf("runtime state is required")
	}
	if state.WorktreePath == "" {
		return "", fmt.Errorf("worktree path is required")
	}
	if state.EnvFile == "" {
		return "", fmt.Errorf("env file path is required")
	}

	clean := filepath.Clean(state.EnvFile)
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("env file path must be relative to the worktree")
	}
	if clean == "." {
		return "", fmt.Errorf("env file path must point to a file")
	}

	fullPath := filepath.Join(state.WorktreePath, clean)
	rel, err := filepath.Rel(state.WorktreePath, fullPath)
	if err != nil {
		return "", fmt.Errorf("resolving env file path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("env file path must stay inside the worktree")
	}

	return fullPath, nil
}

// tremanDir returns the TreeMan home directory: ~/.treeman
func tremanDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), ".treeman")
	}
	return filepath.Join(home, ".treeman")
}

// SaveState writes the runtime state to disk.
func SaveState(state *RuntimeState) error {
	dir := stateDir(state.Repo)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	path := stateFilePath(state.Repo, state.BranchSlug)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing state file: %w", err)
	}

	return nil
}

// LoadState reads the runtime state for a repo/branch from disk.
// Returns nil, nil if no state file exists.
func LoadState(repo, branchSlug string) (*RuntimeState, error) {
	path := stateFilePath(repo, branchSlug)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading state file: %w", err)
	}

	var state RuntimeState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing state file: %w", err)
	}

	return &state, nil
}

// ListStates returns all runtime states for a given repo.
func ListStates(repo string) ([]*RuntimeState, error) {
	dir := stateDir(repo)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading state directory: %w", err)
	}

	var states []*RuntimeState
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}

		var state RuntimeState
		if err := json.Unmarshal(data, &state); err != nil {
			continue
		}

		states = append(states, &state)
	}

	return states, nil
}

// RemoveState deletes the state file for a repo/branch.
func RemoveState(repo, branchSlug string) error {
	path := stateFilePath(repo, branchSlug)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing state file: %w", err)
	}
	return nil
}

// CleanupRuntimeArtifacts removes generated files and persistent runtime state.
func CleanupRuntimeArtifacts(state *RuntimeState) error {
	if state == nil {
		return nil
	}

	var firstErr error
	recordErr := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if state.EnvFile != "" {
		envPath, err := EnvFilePath(state)
		if err != nil {
			recordErr(fmt.Errorf("resolving env file path: %w", err))
		} else {
			err = os.Remove(envPath)
			if err != nil && !os.IsNotExist(err) {
				recordErr(fmt.Errorf("removing env file: %w", err))
			}
		}
	}

	if state.LogFile != "" {
		err := os.Remove(state.LogFile)
		if err != nil && !os.IsNotExist(err) {
			recordErr(fmt.Errorf("removing log file: %w", err))
		}
	}

	registry, err := LoadRegistry()
	if err != nil {
		recordErr(fmt.Errorf("loading port registry: %w", err))
	} else {
		registry.ReleasePorts(AllocateKey(state.Repo, state.BranchSlug))
		recordErr(registry.Save())
	}

	recordErr(RemoveState(state.Repo, state.BranchSlug))
	return firstErr
}
