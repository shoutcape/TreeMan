package deps

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// InstallResult describes what happened during dependency installation.
type InstallResult struct {
	// Skipped is true when no installer was found and no Python project
	// was detected.
	Skipped bool
	// Python is true when a Python project was detected (no auto-install).
	Python bool
	// Installer is the Installer that was run (nil if none).
	Installer *Installer
}

// Install detects the package manager for the project at dir and runs the
// appropriate install command. It is a non-fatal operation — if the binary is
// not found it prints a warning and returns with Skipped=true.
//
// Mirrors the execution part of _wt_install_deps in wt.sh:546.
func Install(dir string) (InstallResult, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return InstallResult{}, fmt.Errorf("deps: reading directory %q: %w", dir, err)
	}

	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, e.Name())
		}
	}

	installer := DetectInstaller(files)

	if installer == nil {
		if IsPythonProject(files) {
			return InstallResult{Python: true}, nil
		}
		return InstallResult{Skipped: true}, nil
	}

	// Check that the binary is available.
	if _, err := exec.LookPath(installer.Binary); err != nil {
		// Not installed — warn but don't fail (mirrors wt.sh:575 behaviour).
		return InstallResult{Skipped: true}, fmt.Errorf(
			"%s found but %s is not installed, skipping",
			installer.Lockfile, installer.Binary,
		)
	}

	args := append([]string{installer.Binary}, installer.Args...)
	cmd := exec.Command(filepath.Clean(args[0]), args[1:]...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return InstallResult{Installer: installer}, fmt.Errorf(
			"%s %s failed: %w", installer.Binary, installer.Args, err,
		)
	}

	return InstallResult{Installer: installer}, nil
}
