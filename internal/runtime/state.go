package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
