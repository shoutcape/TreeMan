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
func IsInsideRepo() bool {
	_, err := run("rev-parse", "--git-dir")
	return err == nil
}

// MainWorktreeRoot returns the absolute path of the main (first) worktree.
// This works correctly even when called from inside a linked worktree.
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

// CurrentWorktreeRoot returns the root of the worktree that contains the
// current directory.
func CurrentWorktreeRoot() (string, error) {
	root, err := run("rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("could not determine current worktree root: %w", err)
	}
	return root, nil
}

// DetectDefaultBranch returns "main" or "master" by inspecting the origin
// remote. It prefers the fast path (local origin/HEAD ref) and falls back to
// querying origin with ls-remote.
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
func BranchExists(branch string) bool {
	_, err := run("show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// BranchSHA returns the commit SHA at the tip of the given local branch.
func BranchSHA(branch string) (string, error) {
	sha, err := run("rev-parse", "--verify", "--quiet", "refs/heads/"+branch+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("could not resolve branch %q: %w", branch, err)
	}
	return sha, nil
}

// RemoteBranchExists reports whether origin has a branch with the exact name.
func RemoteBranchExists(branch string) bool {
	_, err := run("ls-remote", "--exit-code", "--heads", "origin", branch)
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
func WorktreeList() ([]WorktreeEntry, error) {
	out, err := run("worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("could not list worktrees: %w", err)
	}
	return parseWorktreePorcelain(out), nil
}

// RemoteBranchesExist queries origin for the given branch names in a single
// ls-remote call and returns a map of branch name -> exists on origin.
// Branches absent from the map can be treated as not existing on the remote.
func RemoteBranchesExist(branches []string) (map[string]bool, error) {
	if len(branches) == 0 {
		return map[string]bool{}, nil
	}
	args := []string{"ls-remote", "--heads", "origin"}
	for _, b := range branches {
		args = append(args, b)
	}
	out, err := run(args...)
	if err != nil {
		return nil, fmt.Errorf("could not query remote branches: %w", err)
	}
	exists := make(map[string]bool, len(branches))
	for _, line := range strings.Split(out, "\n") {
		// Each line is "<sha>\trefs/heads/<branch>"
		if idx := strings.Index(line, "\trefs/heads/"); idx >= 0 {
			branch := line[idx+len("\trefs/heads/"):]
			exists[branch] = true
		}
	}
	return exists, nil
}

// MergedBranches returns the observed SHA for local branches that are
// ancestors of target.
func MergedBranches(target string) (map[string]string, error) {
	out, err := run("branch", "--merged", target, "--format=%(refname:short)%09%(objectname)")
	if err != nil {
		return nil, fmt.Errorf("could not list branches merged into %q: %w", target, err)
	}

	branches := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		branch, sha, found := strings.Cut(line, "\t")
		if found && branch != "" && sha != "" {
			branches[branch] = sha
		}
	}
	return branches, nil
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

// WorktreeDirty reports whether a worktree has tracked, staged, or untracked
// changes.
func WorktreeDirty(path string) (bool, error) {
	out, err := runInDir(path, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return false, fmt.Errorf("could not inspect worktree %q: %w", path, err)
	}
	return out != "", nil
}

// BranchCanDelete reports whether Git's safe branch deletion would accept the
// branch. Git compares against its upstream when one exists, otherwise HEAD.
func BranchCanDelete(dir, branch string) (bool, error) {
	target := "HEAD"
	if upstream, err := runInDir(dir, "rev-parse", "--abbrev-ref", branch+"@{upstream}"); err == nil {
		target = upstream
	}
	return BranchMergedInto(dir, branch, target)
}

// BranchMergedInto reports whether branch is an ancestor of target.
func BranchMergedInto(dir, branch, target string) (bool, error) {
	cmd := exec.Command("git", "merge-base", "--is-ancestor", branch, target)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return false, fmt.Errorf("could not check whether branch %q is merged: %s", branch, msg)
		}
		return false, fmt.Errorf("could not check whether branch %q is merged: %w", branch, err)
	}
	return true, nil
}

// WorktreeRemove removes the linked worktree at path. force permits removal
// when Git would otherwise protect local changes.
func WorktreeRemove(path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	_, err := run(args...)
	if err != nil {
		return fmt.Errorf("failed to remove worktree %q: %w", path, err)
	}
	return nil
}

// DeleteBranch deletes a local branch from dir. force permits removal of
// unmerged branches.
func DeleteBranch(dir, branch string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := runInDir(dir, "branch", flag, branch)
	if err != nil {
		return fmt.Errorf("branch %q could not be deleted: %w", branch, err)
	}
	return nil
}

// DeleteBranchAtSHA atomically deletes branch only when it still points at
// expectedSHA. This prevents a verified cleanup from deleting later work.
func DeleteBranchAtSHA(dir, branch, expectedSHA string) error {
	_, err := runInDir(dir, "update-ref", "-d", "refs/heads/"+branch, expectedSHA)
	if err != nil {
		return fmt.Errorf("branch %q could not be deleted at verified SHA %s: %w", branch, expectedSHA, err)
	}
	return nil
}

// OriginRemoteURL returns the URL of the origin remote.
// If the environment variable _TREEMAN_REMOTE_URL is set it is returned
// directly without querying git. This lets the smoke test inject a fake URL
// against a local bare repository.
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

// WorktreeAddExisting creates a linked worktree for a branch that already
// exists on the remote. Unlike WorktreeAdd which creates a new branch (-b),
// this checks out an existing remote branch and sets up tracking.
//
//	HUSKY=0 git worktree add --no-track -b <branch> <path> origin/<branch>
func WorktreeAddExisting(path, branch string) error {
	cmd := exec.Command("git", "worktree", "add", "--no-track", "-b", branch, path, "origin/"+branch)
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
