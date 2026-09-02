package deps

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Detection describes the dependency setup supported by a project.
type Detection struct {
	// Python is true when a Python project was detected (no auto-install).
	Python bool
	// Installer is the Installer to run (nil if none).
	Installer *Installer
}

// Detect reports the dependency setup supported by the project at dir.
// Only files directly in dir are considered; nested modules require an
// explicit trusted post-create hook.
func Detect(dir string) (Detection, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Detection{}, fmt.Errorf("deps: reading directory %q: %w", dir, err)
	}

	files := make([]string, 0, len(entries))
	hasPackageJSON := false
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, e.Name())
			hasPackageJSON = hasPackageJSON || e.Name() == "package.json"
		}
	}
	if hasPackageJSON {
		contents, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err == nil {
			if installer := declaredYarnInstaller(contents); installer != nil {
				return Detection{Installer: installer}, nil
			}
		}
	}

	installer := DetectInstaller(files)

	if installer == nil {
		if IsPythonProject(files) {
			return Detection{Python: true}, nil
		}
		return Detection{}, nil
	}

	return Detection{Installer: installer}, nil
}

// Run executes installer in dir, forwarding output to output. It is a
// non-fatal operation: callers should report errors without aborting setup.
func Run(dir string, installer *Installer, output io.Writer) error {
	// Check that the binary is available.
	if err := InstallerAvailable(installer); err != nil {
		// Not installed - warn but do not fail.
		return fmt.Errorf(
			"%s found but %s is not installed, skipping",
			installer.Manifest, installer.Binary,
		)
	}

	args := append([]string{installer.Binary}, installer.Args...)
	cmd := exec.Command(filepath.Clean(args[0]), args[1:]...)
	cmd.Dir = dir
	cmd.Stdout = output
	cmd.Stderr = output

	if err := cmd.Run(); err != nil {
		return fmt.Errorf(
			"%s %s failed: %w", installer.Binary, installer.Args, err,
		)
	}

	return nil
}

// InstallerAvailable reports whether the detected installer can be run.
func InstallerAvailable(installer *Installer) error {
	_, err := exec.LookPath(installer.Binary)
	return err
}
