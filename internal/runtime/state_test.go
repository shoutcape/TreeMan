package runtime

import (
	"os"
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
