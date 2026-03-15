package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStartProcessReleasesPortsOnFailure(t *testing.T) {
	dir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	defer os.Setenv("HOME", origHome)

	worktree := filepath.Join(dir, "worktree")
	if err := os.MkdirAll(worktree, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		Runtime: RuntimeConfig{
			Type:    "process",
			Command: "definitely-not-a-real-command",
			Ports: map[string]int{
				"app": 3000,
			},
		},
	}

	if _, err := StartProcess(cfg, worktree, "myapp", "main", "main"); err == nil {
		t.Fatal("expected StartProcess to fail")
	}

	registry, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}

	if registry.GetPorts(AllocateKey("myapp", "main")) != nil {
		t.Fatal("expected ports to be released after failed start")
	}
}
