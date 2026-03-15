package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// StartProcess starts a process runtime for a worktree.
// It allocates ports, generates the env file, launches the command
// in the background, and saves the state.
func StartProcess(cfg *Config, worktreePath, repo, branch, branchSlug string) (*RuntimeState, error) {
	// Load port registry and allocate ports
	registry, err := LoadRegistry()
	if err != nil {
		return nil, fmt.Errorf("loading port registry: %w", err)
	}

	key := AllocateKey(repo, branchSlug)

	var ports map[string]int
	if len(cfg.Runtime.Ports) > 0 {
		ports, err = registry.AllocatePorts(key, cfg.Runtime.Ports)
		if err != nil {
			return nil, fmt.Errorf("allocating ports: %w", err)
		}

		if err := registry.Save(); err != nil {
			return nil, fmt.Errorf("saving port registry: %w", err)
		}
	}

	// Prepare state
	logFile := LogFilePath(repo, branchSlug)
	state := &RuntimeState{
		Repo:         repo,
		RepoRoot:     worktreePath,
		Branch:       branch,
		BranchSlug:   branchSlug,
		WorktreePath: worktreePath,
		RuntimeType:  "process",
		Status:       "running",
		Command:      cfg.Runtime.Command,
		Ports:        ports,
		EnvFile:      cfg.Runtime.EnvFile,
		LogFile:      logFile,
		StartedAt:    time.Now(),
	}

	// Generate env file
	if err := GenerateEnvFile(state); err != nil {
		return nil, fmt.Errorf("generating env file: %w", err)
	}

	// Ensure log directory exists
	if err := os.MkdirAll(filepath.Dir(logFile), 0755); err != nil {
		return nil, fmt.Errorf("creating log directory: %w", err)
	}

	// Open log file
	logF, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, fmt.Errorf("opening log file: %w", err)
	}

	// Build command
	parts := strings.Fields(cfg.Runtime.Command)
	if len(parts) == 0 {
		logF.Close()
		return nil, fmt.Errorf("empty command")
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = worktreePath
	cmd.Stdout = logF
	cmd.Stderr = logF

	// Build environment: inherit current env + add port variables
	env := os.Environ()
	for name, port := range ports {
		envName := strings.ToUpper(name) + "_PORT"
		env = append(env, fmt.Sprintf("%s=%d", envName, port))
		if name == "app" {
			env = append(env, fmt.Sprintf("PORT=%d", port))
		}
	}
	env = append(env, fmt.Sprintf("TREEMAN_BRANCH=%s", branch))
	env = append(env, fmt.Sprintf("TREEMAN_BRANCH_SLUG=%s", branchSlug))
	cmd.Env = env

	// Start in a new process group so signals don't propagate from treeman
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		logF.Close()
		return nil, fmt.Errorf("starting process: %w", err)
	}

	state.PID = cmd.Process.Pid

	// Save state
	if err := SaveState(state); err != nil {
		// Try to kill the process if we can't save state
		cmd.Process.Kill()
		logF.Close()
		return nil, fmt.Errorf("saving state: %w", err)
	}

	// Detach: we don't wait for the process. The log file handle will be
	// inherited by the child process.
	// Release the process so it doesn't become a zombie
	cmd.Process.Release()
	logF.Close()

	return state, nil
}

// StopProcess stops a running process runtime.
// Sends SIGTERM, waits up to 10 seconds, then sends SIGKILL.
func StopProcess(state *RuntimeState) error {
	if state.PID == 0 {
		return fmt.Errorf("no PID recorded")
	}

	proc, err := os.FindProcess(state.PID)
	if err != nil {
		state.Status = "stopped"
		state.StoppedAt = time.Now()
		SaveState(state)
		return nil
	}

	// Check if the process is still alive
	if !isProcessAlive(state.PID) {
		state.Status = "stopped"
		state.StoppedAt = time.Now()
		SaveState(state)
		return nil
	}

	// Send SIGTERM to the process group
	syscall.Kill(-state.PID, syscall.SIGTERM)

	// Wait for up to 10 seconds
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !isProcessAlive(state.PID) {
			state.Status = "stopped"
			state.StoppedAt = time.Now()
			SaveState(state)
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Force kill
	_ = proc.Kill()
	// Also kill the process group
	syscall.Kill(-state.PID, syscall.SIGKILL)

	state.Status = "stopped"
	state.StoppedAt = time.Now()
	SaveState(state)

	return nil
}

// isProcessAlive checks if a process with the given PID is still running.
func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 checks if the process exists without actually sending a signal
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

// CheckProcessStatus checks if a process runtime is still alive and
// updates the state accordingly. Returns the detected status.
func CheckProcessStatus(state *RuntimeState) string {
	if state.PID == 0 {
		return "stopped"
	}

	if isProcessAlive(state.PID) {
		return "running"
	}

	// Process is dead but state says running → stale
	if state.Status == "running" {
		return "stale"
	}

	return "stopped"
}
