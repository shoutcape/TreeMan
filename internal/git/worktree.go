package git

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Worktree represents a single git worktree entry.
type Worktree struct {
	Path   string // absolute path to worktree directory
	Branch string // branch name (without refs/heads/)
	Bare   bool   // true if this is a bare worktree
}

// WorktreeList returns all worktrees for the current repository.
// Parses the porcelain output of git worktree list for reliability.
func WorktreeList() ([]Worktree, error) {
	output, err := runSilent("worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	return parseWorktreePorcelain(output), nil
}

// parseWorktreePorcelain parses the porcelain output of git worktree list.
//
// Format:
//
//	worktree /path/to/main
//	HEAD abc123
//	branch refs/heads/main
//	<blank line>
//	worktree /path/to/feature
//	HEAD def456
//	branch refs/heads/feature/foo
//	<blank line>
func parseWorktreePorcelain(output string) []Worktree {
	var worktrees []Worktree
	var current Worktree

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "worktree "):
			current = Worktree{Path: strings.TrimPrefix(line, "worktree ")}
		case strings.HasPrefix(line, "branch "):
			ref := strings.TrimPrefix(line, "branch ")
			current.Branch = strings.TrimPrefix(ref, "refs/heads/")
		case line == "bare":
			current.Bare = true
		case line == "":
			if current.Path != "" {
				worktrees = append(worktrees, current)
				current = Worktree{}
			}
		}
	}
	// Handle last entry if no trailing newline
	if current.Path != "" {
		worktrees = append(worktrees, current)
	}

	return worktrees
}

// MainRoot returns the absolute path to the main (first) worktree.
func MainRoot() (string, error) {
	worktrees, err := WorktreeList()
	if err != nil {
		return "", err
	}
	if len(worktrees) == 0 {
		return "", fmt.Errorf("no worktrees found")
	}
	return worktrees[0].Path, nil
}

// BranchSlug converts a branch name to a directory-safe slug.
// Slashes are replaced with dashes: "feature/foo" → "feature-foo"
func BranchSlug(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}

// WorktreePathForBranch builds a sibling worktree path from the main root
// and branch name: <parent>/<repo>.<branch-slug>
func WorktreePathForBranch(mainRoot, branch string) string {
	repoName := filepath.Base(mainRoot)
	slug := BranchSlug(branch)
	return filepath.Join(filepath.Dir(mainRoot), repoName+"."+slug)
}

// WorktreeAdd creates a new worktree with a new branch.
// Equivalent to: HUSKY=0 git worktree add --no-track -b <branch> <path> <startPoint>
func WorktreeAdd(path, branch, startPoint string) error {
	cmd := newGitCmd("worktree", "add", "--no-track", "-b", branch, path, startPoint)
	cmd.Env = append(os.Environ(), "HUSKY=0")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("creating worktree: %w", err)
	}
	return nil
}

// WorktreeRemove removes a worktree directory. Does not force removal
// of dirty worktrees.
func WorktreeRemove(path string) error {
	_, err := run("worktree", "remove", path)
	return err
}

// FindWorktreeForBranch returns the worktree path for a given branch name,
// or empty string if no worktree exists for that branch.
func FindWorktreeForBranch(branch string) string {
	worktrees, err := WorktreeList()
	if err != nil {
		return ""
	}
	for _, wt := range worktrees {
		if wt.Branch == branch {
			return wt.Path
		}
	}
	return ""
}

// CopyEnvFiles copies .env* files from the source directory to the
// destination directory. Only copies regular files at the top level.
func CopyEnvFiles(src, dest string) {
	matches, err := filepath.Glob(filepath.Join(src, ".env*"))
	if err != nil {
		return
	}

	copied := 0
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil || info.IsDir() {
			continue
		}

		srcFile, err := os.Open(match)
		if err != nil {
			continue
		}

		destPath := filepath.Join(dest, filepath.Base(match))
		destFile, err := os.Create(destPath)
		if err != nil {
			srcFile.Close()
			continue
		}

		_, err = io.Copy(destFile, srcFile)
		srcFile.Close()
		destFile.Close()

		if err == nil {
			fmt.Fprintf(os.Stderr, "  Copied %s\n", filepath.Base(match))
			copied++
		}
	}

	if copied > 0 {
		fmt.Fprintf(os.Stderr, "Copied %d env file(s) from main worktree.\n", copied)
	}
}

// InstallDeps detects the project's package manager and runs the install command.
// Returns nil if no lockfile is found or if installation succeeds.
func InstallDeps(dir string) error {
	type dep struct {
		lockfile string
		binary   string
		args     []string
	}

	deps := []dep{
		{"pnpm-lock.yaml", "pnpm", []string{"install"}},
		{"yarn.lock", "yarn", []string{"install"}},
		{"package-lock.json", "npm", []string{"install"}},
		{"go.mod", "go", []string{"mod", "download"}},
	}

	for _, d := range deps {
		if _, err := os.Stat(filepath.Join(dir, d.lockfile)); err == nil {
			path, err := findExecutable(d.binary)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: %s found but %s is not installed, skipping.\n", d.lockfile, d.binary)
				return nil
			}

			fmt.Fprintf(os.Stderr, "Detected %s — running %s %s...\n", d.lockfile, d.binary, strings.Join(d.args, " "))
			cmd := newCmd(path, d.args...)
			cmd.Dir = dir
			cmd.Stdout = os.Stderr
			cmd.Stderr = os.Stderr
			return cmd.Run()
		}
	}

	// Check for Python projects
	for _, pyFile := range []string{"requirements.txt", "pyproject.toml"} {
		if _, err := os.Stat(filepath.Join(dir, pyFile)); err == nil {
			fmt.Fprintf(os.Stderr, "Detected Python project — skipping auto-install (activate your venv manually).\n")
			return nil
		}
	}

	fmt.Fprintf(os.Stderr, "No known dependency file detected, skipping install.\n")
	return nil
}
