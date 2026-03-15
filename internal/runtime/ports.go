package runtime

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
)

// PortRegistry manages port allocations across all worktrees.
type PortRegistry struct {
	Allocations map[string]map[string]int `json:"allocations"` // key → port_name → port
	mu          sync.Mutex
}

// registryPath returns the path to the ports registry file.
func registryPath() string {
	return filepath.Join(tremanDir(), "ports.json")
}

// LoadRegistry reads the port registry from disk.
// Returns an empty registry if the file doesn't exist.
func LoadRegistry() (*PortRegistry, error) {
	reg := &PortRegistry{
		Allocations: make(map[string]map[string]int),
	}

	data, err := os.ReadFile(registryPath())
	if err != nil {
		if os.IsNotExist(err) {
			return reg, nil
		}
		return nil, fmt.Errorf("reading port registry: %w", err)
	}

	if err := json.Unmarshal(data, reg); err != nil {
		return nil, fmt.Errorf("parsing port registry: %w", err)
	}

	if reg.Allocations == nil {
		reg.Allocations = make(map[string]map[string]int)
	}

	return reg, nil
}

// Save writes the port registry to disk.
func (r *PortRegistry) Save() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	dir := filepath.Dir(registryPath())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating registry directory: %w", err)
	}

	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling port registry: %w", err)
	}

	return os.WriteFile(registryPath(), data, 0644)
}

// AllocateKey returns the registry key for a worktree.
// Format: <repo-basename>/<branch-slug>
func AllocateKey(repo, branchSlug string) string {
	return repo + "/" + branchSlug
}

// AllocatePorts assigns free ports for the requested logical ports.
// If the key already has allocations, reuses them (idempotent).
// Otherwise allocates new free ports starting from each base port.
func (r *PortRegistry) AllocatePorts(key string, requested map[string]int) (map[string]int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Reuse existing allocations if present
	if existing, ok := r.Allocations[key]; ok && len(existing) > 0 {
		// Verify all requested ports are covered
		allCovered := true
		for name := range requested {
			if _, ok := existing[name]; !ok {
				allCovered = false
				break
			}
		}
		if allCovered {
			return existing, nil
		}
	}

	// Build set of all currently allocated ports
	usedPorts := make(map[int]bool)
	for _, ports := range r.Allocations {
		for _, port := range ports {
			usedPorts[port] = true
		}
	}

	// Allocate new ports
	allocated := make(map[string]int)
	for name, base := range requested {
		port, err := findFreePort(base, usedPorts)
		if err != nil {
			return nil, fmt.Errorf("allocating port for %s (base %d): %w", name, base, err)
		}
		allocated[name] = port
		usedPorts[port] = true
	}

	r.Allocations[key] = allocated
	return allocated, nil
}

// ReleasePorts removes the port allocations for a key.
func (r *PortRegistry) ReleasePorts(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.Allocations, key)
}

// GetPorts returns the allocated ports for a key, or nil if not allocated.
func (r *PortRegistry) GetPorts(key string) map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Allocations[key]
}

// findFreePort finds a free port starting from base, skipping ports in the
// usedPorts set and ports that are actually in use on the system.
func findFreePort(base int, usedPorts map[int]bool) (int, error) {
	for port := base; port < base+1000; port++ {
		if usedPorts[port] {
			continue
		}
		if isPortFree(port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no free port found in range %d-%d", base, base+999)
}

// isPortFree checks if a TCP port is available on localhost.
func isPortFree(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}
