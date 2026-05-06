// Package git provides a thin wrapper around git subprocess invocations.
// All git operations used by treeman commands are centralised here so they
// can be replaced by a mock in tests.
package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// WorktreeEntry represents a single entry from `git worktree list`.
type WorktreeEntry struct {
	Path   string
	Branch string // empty string for detached HEAD
}

// run executes git with the given arguments, returning trimmed stdout.
// stderr is discarded unless the command fails, in which case it is included
// in the returned error.
func run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// runInDir is like run but sets the working directory.
func runInDir(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// IsInsideRepo reports whether the current working directory is inside a git
// repository.
//
// Mirrors the `git rev-parse --git-dir` guard used throughout wt.sh.
func IsInsideRepo() bool {
	_, err := run("rev-parse", "--git-dir")
	return err == nil
}

// MainWorktreeRoot returns the absolute path of the main (first) worktree.
// This works correctly even when called from inside a linked worktree.
//
// Mirrors _wt_main_root in wt.sh:55.
func MainWorktreeRoot() (string, error) {
	out, err := run("worktree", "list", "--porcelain")
	if err != nil {
		return "", fmt.Errorf("could not list worktrees: %w", err)
	}

	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			return strings.TrimPrefix(line, "worktree "), nil
		}
	}
	return "", fmt.Errorf("could not determine main worktree root")
}

// DetectDefaultBranch returns "main" or "master" by inspecting the origin
// remote. It prefers the fast path (local origin/HEAD ref) and falls back to
// querying origin with ls-remote.
//
// Mirrors _wt_detect_default_branch in wt.sh:24.
func DetectDefaultBranch() (string, error) {
	// Fast path: read local symbolic-ref for origin/HEAD.
	originHead, err := run("symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if err == nil {
		branch := strings.TrimPrefix(originHead, "origin/")
		if branch == "main" {
			return "main", nil
		}
		if branch == "master" {
			return "master", nil
		}
	}

	// Slow path: ask origin directly.
	refs, err := run("ls-remote", "--heads", "origin", "main", "master")
	if err != nil {
		return "", fmt.Errorf("could not detect default branch: %w", err)
	}

	if strings.Contains(refs, "refs/heads/main") {
		return "main", nil
	}
	if strings.Contains(refs, "refs/heads/master") {
		return "master", nil
	}

	return "", fmt.Errorf("could not find 'main' or 'master' on origin")
}

// BranchExists reports whether a local branch with the given name exists.
//
// Mirrors `git show-ref --verify --quiet refs/heads/<branch>` in wt.sh.
func BranchExists(branch string) bool {
	_, err := run("show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// Fetch runs `git fetch origin <refspec>`.
func Fetch(refspec string) error {
	_, err := run("fetch", "origin", refspec)
	return err
}

// WorktreeAdd creates a new linked worktree.
// It is equivalent to:
//
//	HUSKY=0 git worktree add --no-track -b <branch> <path> <startPoint>
//
// HUSKY=0 is set in the process environment to suppress husky hooks.
func WorktreeAdd(path, branch, startPoint string) error {
	cmd := exec.Command("git", "worktree", "add", "--no-track", "-b", branch, path, startPoint)
	cmd.Env = append(cmd.Environ(), "HUSKY=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("git worktree add: %s", msg)
		}
		return fmt.Errorf("git worktree add: %w", err)
	}
	return nil
}

// WorktreeList returns the list of all worktrees, parsed from
// `git worktree list --porcelain`.
//
// Mirrors _wt_worktree_lines in wt.sh:208.
func WorktreeList() ([]WorktreeEntry, error) {
	out, err := run("worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("could not list worktrees: %w", err)
	}
	return parseWorktreePorcelain(out), nil
}

// parseWorktreePorcelain parses the output of `git worktree list --porcelain`
// into WorktreeEntry values.
func parseWorktreePorcelain(out string) []WorktreeEntry {
	var entries []WorktreeEntry
	var current WorktreeEntry

	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			current = WorktreeEntry{Path: strings.TrimPrefix(line, "worktree ")}
		case strings.HasPrefix(line, "branch refs/heads/"):
			current.Branch = strings.TrimPrefix(line, "branch refs/heads/")
		case line == "":
			if current.Path != "" {
				entries = append(entries, current)
				current = WorktreeEntry{}
			}
		}
	}
	// Flush last entry if there was no trailing blank line.
	if current.Path != "" {
		entries = append(entries, current)
	}

	return entries
}

// SetUpstreamInDir sets the upstream tracking branch for the given local branch
// to origin/<branch>. dir must be the worktree directory so git resolves
// the correct branch.
//
//	git branch --set-upstream-to=origin/<branch> <branch>
func SetUpstreamInDir(dir, branch string) error {
	_, err := runInDir(dir, "branch", "--set-upstream-to=origin/"+branch, branch)
	if err != nil {
		return fmt.Errorf("could not set upstream for %q: %w", branch, err)
	}
	return nil
}

// WorktreeRemove force-removes the linked worktree at path.
// Uses --force because the user has already confirmed deletion and
// untracked/modified files should not block removal.
//
// Mirrors `git worktree remove` in wt.sh:490.
func WorktreeRemove(path string) error {
	_, err := run("worktree", "remove", "--force", path)
	if err != nil {
		return fmt.Errorf("failed to remove worktree %q: %w", path, err)
	}
	return nil
}

// DeleteBranch force-deletes a local branch.
//
// Mirrors `git branch -D` in wt.sh:496.
func DeleteBranch(branch string) error {
	_, err := run("branch", "-D", branch)
	if err != nil {
		return fmt.Errorf("branch %q could not be deleted: %w", branch, err)
	}
	return nil
}

// OriginRemoteURL returns the URL of the origin remote.
// If the environment variable _TREEMAN_REMOTE_URL is set it is returned
// directly without querying git (mirrors wt.sh:77 caching behaviour and
// allows the smoke-test to inject a fake URL against a local bare repo).
//
// Mirrors _wt_origin_remote_url in wt.sh:77.
func OriginRemoteURL() (string, error) {
	if override := os.Getenv("_TREEMAN_REMOTE_URL"); override != "" {
		return override, nil
	}
	url, err := run("remote", "get-url", "origin")
	if err != nil {
		return "", fmt.Errorf("could not read origin remote URL")
	}
	return url, nil
}

// FindWorktreeForBranch returns the worktree path for a given branch, or an
// empty string if no worktree is checked out for that branch.
//
// Mirrors _wt_find_worktree_for_branch in wt.sh:422.
func FindWorktreeForBranch(branch string) (string, error) {
	entries, err := WorktreeList()
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.Branch == branch {
			return e.Path, nil
		}
	}
	return "", nil
}

// WorktreeListNormal returns the output of `git worktree list` (non-porcelain)
// for display purposes. Each line has the format:
//
//	/path/to/worktree  <sha>  [branch]
//
// Mirrors _wt_worktree_lines in wt.sh:208.
func WorktreeListNormal() (string, error) {
	out, err := run("worktree", "list")
	if err != nil {
		return "", fmt.Errorf("could not list worktrees: %w", err)
	}
	return out, nil
}

// runWithEnv executes git with extra environment variables.
func runWithEnv(env []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Env = append(cmd.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// CurrentDir returns the absolute path of the current working directory as
// known to git (git rev-parse --show-toplevel).
func CurrentDir() (string, error) {
	return run("rev-parse", "--show-toplevel")
}
