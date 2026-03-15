package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveAndLoadState(t *testing.T) {
	dir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	defer os.Setenv("HOME", origHome)

	state := &RuntimeState{
		Repo:         "myapp",
		Branch:       "feature/test",
		BranchSlug:   "feature-test",
		WorktreePath: "/tmp/myapp.feature-test",
		RuntimeType:  "process",
		Status:       "running",
		PID:          12345,
		Command:      "pnpm dev",
		Ports:        map[string]int{"app": 3001},
		LogFile:      "/tmp/logs/myapp/feature-test.log",
		StartedAt:    time.Now().Truncate(time.Second),
	}

	if err := SaveState(state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	loaded, err := LoadState("myapp", "feature-test")
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	if loaded == nil {
		t.Fatal("LoadState returned nil")
	}

	if loaded.Repo != "myapp" {
		t.Errorf("Repo = %q, want %q", loaded.Repo, "myapp")
	}
	if loaded.Branch != "feature/test" {
		t.Errorf("Branch = %q, want %q", loaded.Branch, "feature/test")
	}
	if loaded.PID != 12345 {
		t.Errorf("PID = %d, want %d", loaded.PID, 12345)
	}
	if loaded.Ports["app"] != 3001 {
		t.Errorf("Ports[app] = %d, want %d", loaded.Ports["app"], 3001)
	}
	if loaded.Status != "running" {
		t.Errorf("Status = %q, want %q", loaded.Status, "running")
	}
}

func TestLoadStateMissing(t *testing.T) {
	dir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	defer os.Setenv("HOME", origHome)

	state, err := LoadState("nonexistent", "branch")
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state != nil {
		t.Error("expected nil for missing state")
	}
}

func TestListStates(t *testing.T) {
	dir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	defer os.Setenv("HOME", origHome)

	// Save two states
	state1 := &RuntimeState{Repo: "myapp", Branch: "main", BranchSlug: "main", Status: "running"}
	state2 := &RuntimeState{Repo: "myapp", Branch: "feature/a", BranchSlug: "feature-a", Status: "stopped"}

	SaveState(state1)
	SaveState(state2)

	states, err := ListStates("myapp")
	if err != nil {
		t.Fatalf("ListStates: %v", err)
	}

	if len(states) != 2 {
		t.Errorf("len(states) = %d, want 2", len(states))
	}
}

func TestRemoveState(t *testing.T) {
	dir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	defer os.Setenv("HOME", origHome)

	state := &RuntimeState{Repo: "myapp", Branch: "main", BranchSlug: "main", Status: "stopped"}
	SaveState(state)

	if err := RemoveState("myapp", "main"); err != nil {
		t.Fatalf("RemoveState: %v", err)
	}

	loaded, _ := LoadState("myapp", "main")
	if loaded != nil {
		t.Error("expected nil after RemoveState")
	}
}

func TestCleanupRuntimeArtifacts(t *testing.T) {
	dir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	defer os.Setenv("HOME", origHome)

	worktree := filepath.Join(dir, "worktree")
	if err := os.MkdirAll(filepath.Join(worktree, "config"), 0755); err != nil {
		t.Fatal(err)
	}

	state := &RuntimeState{
		Repo:         "myapp",
		Branch:       "feature/test",
		BranchSlug:   "feature-test",
		WorktreePath: worktree,
		EnvFile:      "config/.env.treeman",
		LogFile:      LogFilePath("myapp", "feature-test"),
		Ports:        map[string]int{"app": 3001},
	}

	if err := SaveState(state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	registry := &PortRegistry{Allocations: map[string]map[string]int{
		AllocateKey("myapp", "feature-test"): {"app": 3001},
	}}
	if err := registry.Save(); err != nil {
		t.Fatalf("registry.Save: %v", err)
	}

	envPath, err := EnvFilePath(state)
	if err != nil {
		t.Fatalf("EnvFilePath: %v", err)
	}
	if err := os.WriteFile(envPath, []byte("PORT=3001\n"), 0644); err != nil {
		t.Fatalf("writing env file: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(state.LogFile), 0755); err != nil {
		t.Fatalf("creating log dir: %v", err)
	}
	if err := os.WriteFile(state.LogFile, []byte("log\n"), 0644); err != nil {
		t.Fatalf("writing log file: %v", err)
	}

	if err := CleanupRuntimeArtifacts(state); err != nil {
		t.Fatalf("CleanupRuntimeArtifacts: %v", err)
	}

	if _, err := os.Stat(envPath); !os.IsNotExist(err) {
		t.Fatalf("expected env file to be removed, got err=%v", err)
	}
	if _, err := os.Stat(state.LogFile); !os.IsNotExist(err) {
		t.Fatalf("expected log file to be removed, got err=%v", err)
	}

	loaded, err := LoadState("myapp", "feature-test")
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded != nil {
		t.Fatal("expected state to be removed")
	}

	loadedRegistry, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if loadedRegistry.GetPorts(AllocateKey("myapp", "feature-test")) != nil {
		t.Fatal("expected ports to be released")
	}
}

func TestCleanupRuntimeArtifactsRejectsEscapingEnvPath(t *testing.T) {
	dir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	defer os.Setenv("HOME", origHome)

	worktree := filepath.Join(dir, "worktree")
	if err := os.MkdirAll(worktree, 0755); err != nil {
		t.Fatal(err)
	}

	outsideDir := filepath.Join(dir, "outside")
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outsideDir, ".env.treeman")
	if err := os.WriteFile(outsideFile, []byte("PORT=3001\n"), 0644); err != nil {
		t.Fatal(err)
	}

	state := &RuntimeState{
		Repo:         "myapp",
		Branch:       "feature/test",
		BranchSlug:   "feature-test",
		WorktreePath: worktree,
		EnvFile:      "../outside/.env.treeman",
	}

	err := CleanupRuntimeArtifacts(state)
	if err == nil || !strings.Contains(err.Error(), "must stay inside the worktree") {
		t.Fatalf("expected escaping env path error, got %v", err)
	}

	if _, err := os.Stat(outsideFile); err != nil {
		t.Fatalf("expected outside env file to remain, got %v", err)
	}
}
