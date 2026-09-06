// Package deps handles dependency installer detection for new worktrees.
// It maps well-known lockfiles to their corresponding install commands,
// matching well-known project conventions.
package deps

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shoutcape/treeman/internal/fsutil"
	"github.com/shoutcape/treeman/internal/git"
)

// Installer describes a detected package manager and the command to run.
type Installer struct {
	// Manifest is the root manifest or lockfile that triggers this installer.
	Manifest string
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

// knownInstallers is the ordered list of manifest-to-installer mappings.
// Priority is first-match-wins.
var knownInstallers = []Installer{
	{Manifest: "pnpm-lock.yaml", Binary: "pnpm", Args: []string{"install"}},
	{Manifest: "yarn.lock", Binary: "yarn", Args: []string{"install"}},
	{Manifest: "package-lock.json", Binary: "npm", Args: []string{"install"}},
	{Manifest: "go.mod", Binary: "go", Args: []string{"mod", "download"}},
	{Manifest: "Cargo.toml", Binary: "cargo", Args: []string{"fetch"}},
}

var ignoredModuleDirs = map[string]struct{}{
	".git":         {},
	".venv":        {},
	".worktrees":   {},
	"node_modules": {},
	"vendor":       {},
}

// DetectInstaller returns the first Installer whose manifest appears in files,
// or nil if no known manifest is present.
//
// files should be the list of filenames (basename only) in the worktree root.
// The caller is responsible for reading the directory.
//
// Priority order: pnpm > yarn > npm > go > cargo
func DetectInstaller(files []string) *Installer {
	return installerFor(fileSet(files))
}

func declaredYarnInstaller(packageJSON []byte) *Installer {
	var manifest struct {
		PackageManager string `json:"packageManager"`
	}
	if err := json.Unmarshal(packageJSON, &manifest); err != nil {
		return nil
	}

	manager, _, _ := strings.Cut(manifest.PackageManager, "@")
	if manager != "yarn" {
		return nil
	}

	return &Installer{
		Manifest: "package.json",
		Binary:   "corepack",
		Args:     []string{"yarn", "install"},
	}
}

func installerFor(files map[string]struct{}) *Installer {
	for i := range knownInstallers {
		if _, ok := files[knownInstallers[i].Manifest]; ok {
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
		return installer.Manifest
	}
	return pythonManifestFor(files)
}

func isSupportedManifest(name string) bool {
	for _, installer := range knownInstallers {
		if name == installer.Manifest {
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

// DiscoverNestedModules finds supported package modules below dir, excluding
// paths ignored by Git and any excluded directory given. Paths are relative to
// dir so callers can report locations without exposing the full worktree path.
//
// excluded is for directories a caller knows are not part of the project, such
// as a configured worktree parent inside the repository. They are matched by
// resolved path rather than by name, so excluding "build/worktrees" does not
// also hide an unrelated "packages/worktrees" module.
func DiscoverNestedModules(dir string, excluded ...string) ([]Module, error) {
	ignoredPaths, err := git.IgnoredPaths(dir)
	if err != nil {
		return nil, err
	}
	excludedPaths, err := exclusionSet(dir, excluded)
	if err != nil {
		return nil, err
	}
	return discoverNestedModules(dir, ignoredPaths, excludedPaths)
}

// exclusionSet translates canonical exclusions into the walk's path namespace
// once. WalkDir does not follow directory symlinks, so direct lookup is enough
// while walking and external exclusions can be discarded.
func exclusionSet(root string, paths []string) (map[string]struct{}, error) {
	canonicalRoot, err := fsutil.CanonicalPath(root)
	if err != nil {
		return nil, fmt.Errorf("deps: resolving discovery root %q: %w", root, err)
	}
	set := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		resolved, err := fsutil.CanonicalPath(path)
		if err != nil {
			return nil, fmt.Errorf("deps: resolving excluded directory %q: %w", path, err)
		}
		relative, err := filepath.Rel(canonicalRoot, resolved)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		set[filepath.Clean(filepath.Join(root, relative))] = struct{}{}
	}
	return set, nil
}

func discoverNestedModules(dir string, ignoredPaths, excludedPaths map[string]struct{}) ([]Module, error) {
	manifestsByDir := make(map[string]map[string]struct{})
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != dir {
				if _, ignored := ignoredPaths[path]; ignored {
					return filepath.SkipDir
				}
				if _, ignored := ignoredModuleDirs[entry.Name()]; ignored {
					return filepath.SkipDir
				}
				if _, excluded := excludedPaths[path]; excluded {
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
