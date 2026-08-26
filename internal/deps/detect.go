// Package deps handles dependency installer detection for new worktrees.
// It maps well-known lockfiles to their corresponding install commands,
// matching well-known project conventions.
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
// Priority is first-match-wins.
var knownInstallers = []Installer{
	{Lockfile: "pnpm-lock.yaml", Binary: "pnpm", Args: []string{"install"}},
	{Lockfile: "yarn.lock", Binary: "yarn", Args: []string{"install"}},
	{Lockfile: "package-lock.json", Binary: "npm", Args: []string{"install"}},
	{Lockfile: "go.mod", Binary: "go", Args: []string{"mod", "download"}},
}

// unsupportedManifests are known dependency manifests TreeMan does not yet
// bootstrap automatically.
var unsupportedManifests = []string{"Cargo.toml", "Cargo.lock"}

// DetectInstaller returns the first Installer whose lockfile appears in files,
// or nil if no known lockfile is present.
//
// files should be the list of filenames (basename only) in the worktree root.
// The caller is responsible for reading the directory.
//
// Priority order: pnpm > yarn > npm > go
func DetectInstaller(files []string) *Installer {
	return detectInstaller(filenameSet(files))
}

func detectInstaller(files map[string]struct{}) *Installer {
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
	return isPythonProject(filenameSet(files))
}

func isPythonProject(files map[string]struct{}) bool {
	for _, pyFile := range pythonFiles {
		if _, ok := files[pyFile]; ok {
			return true
		}
	}
	return false
}

// DetectUnsupportedManifests returns known dependency manifests that TreeMan
// does not bootstrap automatically.
func DetectUnsupportedManifests(files []string) []string {
	return detectUnsupportedManifests(filenameSet(files))
}

func detectUnsupportedManifests(files map[string]struct{}) []string {
	var manifests []string
	for _, manifest := range unsupportedManifests {
		if _, ok := files[manifest]; ok {
			manifests = append(manifests, manifest)
		}
	}
	return manifests
}

func filenameSet(files []string) map[string]struct{} {
	set := make(map[string]struct{}, len(files))
	for _, f := range files {
		set[f] = struct{}{}
	}
	return set
}
