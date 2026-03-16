// Package deps handles dependency installer detection for new worktrees.
// It maps well-known lockfiles to their corresponding install commands,
// matching the behaviour of _wt_install_deps in wt.sh:546.
package deps

// Installer describes a detected package manager and the command to run.
type Installer struct {
	// Lockfile is the filename that triggers this installer.
	Lockfile string
	// Binary is the executable to invoke (e.g. "npm", "go").
	Binary string
	// Args are the arguments passed to Binary (e.g. ["install"] or ["mod", "download"]).
	Args []string
}

// pythonFiles are filenames that indicate a Python project.
// Python projects are detected but not auto-installed; the user must
// activate their virtualenv manually.
var pythonFiles = []string{"requirements.txt", "pyproject.toml"}

// knownInstallers is the ordered list of lockfile→installer mappings.
// Priority is first-match-wins, mirroring the deps array in wt.sh:552-557.
var knownInstallers = []Installer{
	{Lockfile: "pnpm-lock.yaml", Binary: "pnpm", Args: []string{"install"}},
	{Lockfile: "yarn.lock", Binary: "yarn", Args: []string{"install"}},
	{Lockfile: "package-lock.json", Binary: "npm", Args: []string{"install"}},
	{Lockfile: "go.mod", Binary: "go", Args: []string{"mod", "download"}},
}

// KnownInstallers returns a copy of the ordered installer list.
// The first matching entry should be used (highest priority first).
func KnownInstallers() []Installer {
	out := make([]Installer, len(knownInstallers))
	copy(out, knownInstallers)
	return out
}

// DetectInstaller returns the first Installer whose lockfile appears in files,
// or nil if no known lockfile is present.
//
// files should be the list of filenames (basename only) in the worktree root.
// The caller is responsible for reading the directory.
//
// Priority order: pnpm > yarn > npm > go
func DetectInstaller(files []string) *Installer {
	set := make(map[string]struct{}, len(files))
	for _, f := range files {
		set[f] = struct{}{}
	}

	for i := range knownInstallers {
		if _, ok := set[knownInstallers[i].Lockfile]; ok {
			result := knownInstallers[i] // copy
			return &result
		}
	}
	return nil
}

// IsPythonProject reports whether any of files indicates a Python project.
// This is checked only when DetectInstaller returns nil.
//
// Mirrors the python check in wt.sh:582-585.
func IsPythonProject(files []string) bool {
	set := make(map[string]struct{}, len(files))
	for _, f := range files {
		set[f] = struct{}{}
	}
	for _, pyFile := range pythonFiles {
		if _, ok := set[pyFile]; ok {
			return true
		}
	}
	return false
}
