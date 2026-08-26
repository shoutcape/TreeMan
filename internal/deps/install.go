package deps

import (
	"fmt"
	"io"
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
	// UnsupportedManifests are detected dependency manifests that TreeMan did
	// not bootstrap.
	UnsupportedManifests []string
}

// Install detects the package manager for the project at dir and runs the
// appropriate install command. It is a non-fatal operation — if the binary is
// not found it prints a warning and returns with Skipped=true.
func Install(dir string, outputs ...io.Writer) (InstallResult, error) {
	output := io.Writer(os.Stderr)
	if len(outputs) > 0 && outputs[0] != nil {
		output = outputs[0]
	}
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

	filenames := filenameSet(files)
	installer := detectInstaller(filenames)
	unsupportedManifests := detectUnsupportedManifests(filenames)

	if installer == nil {
		if isPythonProject(filenames) {
			return InstallResult{Python: true, UnsupportedManifests: unsupportedManifests}, nil
		}
		return InstallResult{Skipped: true, UnsupportedManifests: unsupportedManifests}, nil
	}

	// Check that the binary is available.
	if _, err := exec.LookPath(installer.Binary); err != nil {
		// Not installed - warn but do not fail.
		return InstallResult{Skipped: true, UnsupportedManifests: unsupportedManifests}, fmt.Errorf(
			"%s found but %s is not installed, skipping",
			installer.Lockfile, installer.Binary,
		)
	}

	args := append([]string{installer.Binary}, installer.Args...)
	cmd := exec.Command(filepath.Clean(args[0]), args[1:]...)
	cmd.Dir = dir
	cmd.Stdout = output
	cmd.Stderr = output

	if err := cmd.Run(); err != nil {
		return InstallResult{Installer: installer, UnsupportedManifests: unsupportedManifests}, fmt.Errorf(
			"%s %s failed: %w", installer.Binary, installer.Args, err,
		)
	}

	return InstallResult{Installer: installer, UnsupportedManifests: unsupportedManifests}, nil
}
