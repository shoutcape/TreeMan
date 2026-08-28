// Package deps handles dependency installer detection for new worktrees.
// It maps well-known lockfiles to their corresponding install commands,
// matching well-known project conventions.
package deps

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// Installer describes a detected package manager and the command to run.
type Installer struct {
	// Lockfile is the filename that triggers this installer.
	Lockfile string
	// Binary is the executable to invoke (e.g. "npm", "go").
	Binary string
	// Args are the arguments passed to Binary (e.g. ["install"] or ["mod", "download"]).
	Args []string
}

// Module describes a supported package module found below the project root.
// Nested modules are reported but are never installed automatically.
type Module struct {
	Path     string
	Manifest string
}

// pythonFiles are filenames that indicate a Python project.
// Python projects are detected but not auto-installed; the user must
// activate their virtualenv manually.
var pythonFiles = []string{"requirements.txt", "pyproject.toml"}

// knownInstallers is the ordered list of lockfile→installer mappings.
// Priority is first-match-wins.
var knownInstallers = []Installer{
	{Lockfile: "pnpm-lock.yaml", Binary: "pnpm", Args: []string{"install"}},
	{Lockfile: "yarn.lock", Binary: "yarn", Args: []string{"install"}},
	{Lockfile: "package-lock.json", Binary: "npm", Args: []string{"install"}},
	{Lockfile: "go.mod", Binary: "go", Args: []string{"mod", "download"}},
	{Lockfile: "Cargo.toml", Binary: "cargo", Args: []string{"fetch"}},
}

var ignoredModuleDirs = map[string]struct{}{
	".git":         {},
	".venv":        {},
	".worktrees":   {},
	"node_modules": {},
	"vendor":       {},
}

// DetectInstaller returns the first Installer whose lockfile appears in files,
// or nil if no known lockfile is present.
//
// files should be the list of filenames (basename only) in the worktree root.
// The caller is responsible for reading the directory.
//
// Priority order: pnpm > yarn > npm > go > cargo
func DetectInstaller(files []string) *Installer {
	return installerFor(fileSet(files))
}

func installerFor(files map[string]struct{}) *Installer {
	for i := range knownInstallers {
		if _, ok := files[knownInstallers[i].Lockfile]; ok {
			result := knownInstallers[i] // copy
			return &result
		}
	}
	return nil
}

// IsPythonProject reports whether any of files indicates a Python project.
// This is checked only when DetectInstaller returns nil.
func IsPythonProject(files []string) bool {
	return pythonManifestFor(fileSet(files)) != ""
}

func fileSet(files []string) map[string]struct{} {
	set := make(map[string]struct{}, len(files))
	for _, file := range files {
		set[file] = struct{}{}
	}
	return set
}

func pythonManifestFor(files map[string]struct{}) string {
	for _, pyFile := range pythonFiles {
		if _, ok := files[pyFile]; ok {
			return pyFile
		}
	}
	return ""
}

func supportedManifestFor(files map[string]struct{}) string {
	if installer := installerFor(files); installer != nil {
		return installer.Lockfile
	}
	return pythonManifestFor(files)
}

func isSupportedManifest(name string) bool {
	for _, installer := range knownInstallers {
		if name == installer.Lockfile {
			return true
		}
	}
	for _, manifest := range pythonFiles {
		if name == manifest {
			return true
		}
	}
	return false
}

// DiscoverNestedModules finds supported package modules below dir. Paths are
// relative to dir so callers can report locations without exposing the full
// worktree path. ignoredPaths contains absolute paths ignored by Git.
func DiscoverNestedModules(dir string, ignoredPaths map[string]struct{}) ([]Module, error) {
	manifestsByDir := make(map[string]map[string]struct{})
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != dir {
				if _, ignored := ignoredPaths[path]; ignored || strings.HasPrefix(entry.Name(), ".") {
					return filepath.SkipDir
				}
				if _, ignored := ignoredModuleDirs[entry.Name()]; ignored {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if _, ignored := ignoredPaths[path]; ignored {
			return nil
		}

		parent := filepath.Dir(path)
		if parent == dir || !isSupportedManifest(entry.Name()) {
			return nil
		}
		if manifestsByDir[parent] == nil {
			manifestsByDir[parent] = make(map[string]struct{})
		}
		manifestsByDir[parent][entry.Name()] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("deps: discovering nested modules in %q: %w", dir, err)
	}

	paths := make([]string, 0, len(manifestsByDir))
	for path := range manifestsByDir {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	modules := make([]Module, 0)
	for _, path := range paths {
		manifest := supportedManifestFor(manifestsByDir[path])
		if manifest == "" {
			continue
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil, fmt.Errorf("deps: resolving nested module path %q: %w", path, err)
		}
		modules = append(modules, Module{Path: filepath.ToSlash(rel), Manifest: manifest})
	}
	return modules, nil
}
