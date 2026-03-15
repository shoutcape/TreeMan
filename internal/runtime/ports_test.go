package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPortRegistryAllocate(t *testing.T) {
	reg := &PortRegistry{
		Allocations: make(map[string]map[string]int),
	}

	requested := map[string]int{
		"app": 3000,
		"api": 4000,
	}

	ports, err := reg.AllocatePorts("repo/main", requested)
	if err != nil {
		t.Fatalf("AllocatePorts: %v", err)
	}

	if ports["app"] < 3000 || ports["app"] >= 4000 {
		t.Errorf("app port %d out of expected range 3000-3999", ports["app"])
	}
	if ports["api"] < 4000 || ports["api"] >= 5000 {
		t.Errorf("api port %d out of expected range 4000-4999", ports["api"])
	}
}

func TestPortRegistryIdempotent(t *testing.T) {
	reg := &PortRegistry{
		Allocations: make(map[string]map[string]int),
	}

	requested := map[string]int{
		"app": 3000,
	}

	ports1, err := reg.AllocatePorts("repo/main", requested)
	if err != nil {
		t.Fatalf("first AllocatePorts: %v", err)
	}

	ports2, err := reg.AllocatePorts("repo/main", requested)
	if err != nil {
		t.Fatalf("second AllocatePorts: %v", err)
	}

	if ports1["app"] != ports2["app"] {
		t.Errorf("idempotent allocation failed: %d != %d", ports1["app"], ports2["app"])
	}
}

func TestPortRegistryNoCollisions(t *testing.T) {
	reg := &PortRegistry{
		Allocations: make(map[string]map[string]int),
	}

	requested := map[string]int{
		"app": 3000,
	}

	ports1, err := reg.AllocatePorts("repo/main", requested)
	if err != nil {
		t.Fatalf("first AllocatePorts: %v", err)
	}

	ports2, err := reg.AllocatePorts("repo/feature-a", requested)
	if err != nil {
		t.Fatalf("second AllocatePorts: %v", err)
	}

	if ports1["app"] == ports2["app"] {
		t.Errorf("port collision: both got %d", ports1["app"])
	}
}

func TestPortRegistryRelease(t *testing.T) {
	reg := &PortRegistry{
		Allocations: make(map[string]map[string]int),
	}

	requested := map[string]int{"app": 3000}
	reg.AllocatePorts("repo/main", requested)

	reg.ReleasePorts("repo/main")

	if ports := reg.GetPorts("repo/main"); ports != nil {
		t.Error("expected nil after release")
	}
}

func TestPortRegistrySaveLoad(t *testing.T) {
	// Override treeman dir for testing
	dir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	defer os.Setenv("HOME", origHome)

	// Ensure .treeman dir exists
	os.MkdirAll(filepath.Join(dir, ".treeman"), 0755)

	reg := &PortRegistry{
		Allocations: map[string]map[string]int{
			"repo/main": {"app": 3000, "api": 4000},
		},
	}

	if err := reg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}

	if loaded.Allocations["repo/main"]["app"] != 3000 {
		t.Errorf("loaded app port = %d, want 3000", loaded.Allocations["repo/main"]["app"])
	}
	if loaded.Allocations["repo/main"]["api"] != 4000 {
		t.Errorf("loaded api port = %d, want 4000", loaded.Allocations["repo/main"]["api"])
	}
}
