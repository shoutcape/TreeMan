// Package git provides a thin wrapper around git subprocess invocations.
// All git operations used by treeman commands are centralised here so they
// can be replaced by a mock in tests.
package git

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
)

// WorktreeEntry represents a single entry from `git worktree list`.
type WorktreeEntry struct {
	Path   string
	Branch string // empty string for detached HEAD
	// Locked records `git worktree lock`. A locked worktree is one the user
	// asked not to be removed -- the flag exists for a worktree on removable
	// media or a network mount, whose directory is legitimately absent
	// sometimes -- so it is never removed and never inferred to be stale.
	Locked bool
	// LockReason is the optional message passed to `git worktree lock`.
	LockReason string
	// Prunable records Git's own judgement that the registration no longer
	// describes a usable worktree.
	Prunable bool
}

// CreatedWorktree identifies a worktree and branch created by one operation.
// SHA is captured at creation so cleanup can avoid deleting later work.
type CreatedWorktree struct {
	Path   string
	Branch string
	SHA    string
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
	stdout, err := runInDirRaw(dir, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(stdout)), nil
}

func runInDirRaw(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
		}
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return stdout.Bytes(), nil
}

// IgnoredPaths returns absolute paths ignored by Git in dir. Directory entries
// represent an ignored subtree when Git can collapse it to one path.
func IgnoredPaths(dir string) (map[string]struct{}, error) {
	output, err := runInDirRaw(dir, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z")
	if err != nil {
		return nil, fmt.Errorf("could not list ignored paths: %w", err)
	}

	paths := make(map[string]struct{})
	for _, path := range bytes.Split(output, []byte{0}) {
		if len(path) == 0 {
			continue
		}
		paths[filepath.Clean(filepath.Join(dir, filepath.FromSlash(string(path))))] = struct{}{}
	}
	return paths, nil
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

// DetectDefaultBranch returns the branch named by origin/HEAD. It prefers the
// fast path (local origin/HEAD ref) and falls back to querying origin for main
// or master when that ref is unavailable.
func DetectDefaultBranch() (string, error) {
	// Fast path: read local symbolic-ref for origin/HEAD.
	if branch, ok := LocalDefaultBranch(); ok {
		return branch, nil
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

// LocalDefaultBranch reads the default branch from origin/HEAD without
// touching the network. Git writes that ref on clone, and since 2.49 every
// fetch creates it too, so this answers in almost every repository. Callers
// that must not stall -- anything decorating a prompt -- use this rather than
// DetectDefaultBranch, whose fallback asks the remote.
func LocalDefaultBranch() (string, bool) {
	originHead, err := run("symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		return "", false
	}
	branch := strings.TrimPrefix(originHead, "origin/")
	return branch, branch != ""
}

// BranchExists reports whether a local branch with the given name exists.
func BranchExists(branch string) bool {
	_, err := run("show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// BranchMissing reports whether the exact local branch ref is absent. Unlike
// BranchExists, operational failures are returned so callers can fail closed.
func BranchMissing(dir, branch string) (bool, error) {
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return true, nil
		}
		return false, fmt.Errorf("could not check branch %q: %w", branch, err)
	}
	return false, nil
}

// HasCommits reports whether HEAD in dir resolves to a commit. An initialised
// repository that has none can still be cloned, but there is nothing in it to
// check out or branch from.
func HasCommits(dir string) (bool, error) {
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("could not resolve HEAD in %q: %w", dir, err)
	}
	return true, nil
}

// BranchSHA returns the commit SHA at the tip of the given local branch.
func BranchSHA(branch string) (string, error) {
	sha, err := run("rev-parse", "--verify", "--quiet", "refs/heads/"+branch+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("could not resolve branch %q: %w", branch, err)
	}
	return sha, nil
}

// BranchSHAs returns local tips for the requested branches in one Git process.
// Branches absent from the result no longer resolve to local commits.
func BranchSHAs(branches []string) (map[string]string, error) {
	if len(branches) == 0 {
		return map[string]string{}, nil
	}
	args := []string{"for-each-ref", "--format=%(refname:short)%09%(objectname)"}
	for _, branch := range branches {
		args = append(args, "refs/heads/"+branch)
	}
	out, err := run(args...)
	if err != nil {
		return nil, fmt.Errorf("could not resolve local branch tips: %w", err)
	}
	tips := make(map[string]string, len(branches))
	for _, line := range strings.Split(out, "\n") {
		branch, sha, found := strings.Cut(line, "\t")
		if found && branch != "" && sha != "" {
			tips[branch] = sha
		}
	}
	return tips, nil
}

// RemoteTrackingBranchSHA returns the locally stored origin/<branch> commit
// SHA. exists is false when no tracking ref has been fetched yet.
func RemoteTrackingBranchSHA(branch string) (sha string, exists bool, err error) {
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch+"^{commit}")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "", false, nil
		}
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", false, fmt.Errorf("could not resolve origin/%q: %s", branch, msg)
		}
		return "", false, fmt.Errorf("could not resolve origin/%q: %w", branch, err)
	}
	return strings.TrimSpace(stdout.String()), true, nil
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

var fetchRefSequence atomic.Uint64

// FetchCommit fetches a ref through a unique temporary ref and returns its
// immutable commit SHA without relying on process-global FETCH_HEAD state.
func FetchCommit(refspec string) (string, error) {
	temporaryRef := fmt.Sprintf("refs/treeman/fetch/%d-%d", os.Getpid(), fetchRefSequence.Add(1))
	if _, err := run("fetch", "--no-write-fetch-head", "origin", "+"+refspec+":"+temporaryRef); err != nil {
		return "", err
	}
	sha, resolveErr := run("rev-parse", "--verify", temporaryRef+"^{commit}")
	_, cleanupErr := run("update-ref", "-d", temporaryRef)
	if resolveErr != nil {
		resolveErr = fmt.Errorf("could not resolve fetched ref %q: %w", refspec, resolveErr)
	}
	if cleanupErr != nil {
		cleanupErr = fmt.Errorf("could not remove temporary fetch ref %q: %w", temporaryRef, cleanupErr)
	}
	return sha, errors.Join(resolveErr, cleanupErr)
}

// Clone creates a no-checkout clone for isolated command execution.
func Clone(remoteURL, path string) error {
	cmd := exec.Command("git", "clone", "--no-checkout", "--origin", "origin", remoteURL, path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("git clone: %s", msg)
		}
		return fmt.Errorf("git clone: %w", err)
	}
	return nil
}

// CheckOutHead populates the working tree of dir from its current HEAD without
// moving any ref. A --no-checkout clone has no files on disk until this runs.
func CheckOutHead(dir string) error {
	if _, err := runInDir(dir, "reset", "--hard", "HEAD"); err != nil {
		return fmt.Errorf("could not check out HEAD in %q: %w", dir, err)
	}
	return nil
}

// DiscardWorktreeChanges returns a worktree to its committed state: tracked
// files are restored and untracked files are removed. Ignored files are
// deliberately kept, because a worktree's dependency directories and
// environment files are part of the setup a caller asked for, not part of the
// drift this clears. The removed untracked paths are returned, so a caller
// that populated the worktree itself can see how much of that output the
// project does not ignore.
func DiscardWorktreeChanges(path string) ([]string, error) {
	removed, err := UntrackedPaths(path)
	if err != nil {
		return nil, err
	}
	if _, err := runInDir(path, "reset", "--hard", "HEAD"); err != nil {
		return nil, fmt.Errorf("could not restore tracked files in %q: %w", path, err)
	}
	if _, err := runInDir(path, "clean", "--force", "-d"); err != nil {
		return nil, fmt.Errorf("could not remove untracked files in %q: %w", path, err)
	}
	return removed, nil
}

// UntrackedPaths returns the paths in dir that Git neither tracks nor ignores,
// relative to dir. A wholly untracked directory collapses to one path, the
// same way the clean that removes it treats the subtree.
func UntrackedPaths(dir string) ([]string, error) {
	output, err := runInDirRaw(dir, "ls-files", "--others", "--exclude-standard", "--directory", "-z")
	if err != nil {
		return nil, fmt.Errorf("could not list untracked paths in %q: %w", dir, err)
	}
	var paths []string
	for _, path := range bytes.Split(output, []byte{0}) {
		if len(path) == 0 {
			continue
		}
		paths = append(paths, filepath.FromSlash(string(path)))
	}
	return paths, nil
}

// FetchRemoteBranch fetches an origin branch into its remote-tracking ref.
func FetchRemoteBranch(branch string) error {
	refspec := "+refs/heads/" + branch + ":refs/remotes/origin/" + branch
	_, err := run("fetch", "origin", refspec)
	return err
}

// WorktreeAdd creates a new branch and linked worktree.
func WorktreeAdd(path, branch, startPoint string) error {
	_, err := CreateWorktree(path, branch, startPoint)
	return err
}

// CreateWorktree creates a branch and linked worktree as one owned resource.
func CreateWorktree(path, branch, startPoint string) (CreatedWorktree, error) {
	return createWorktree("", path, branch, startPoint)
}

// WorktreeList returns the list of all worktrees, parsed from
// `git worktree list --porcelain`.
func WorktreeList() ([]WorktreeEntry, error) {
	return worktreeListInDir("")
}

func worktreeListInDir(dir string) ([]WorktreeEntry, error) {
	out, err := runInDir(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("could not list worktrees: %w", err)
	}
	return parseWorktreePorcelain(out), nil
}

// RemoteHeads queries origin for the given branch names in a single ls-remote
// call and returns their branch name -> commit SHA snapshot. Branches absent
// from the map do not exist on origin at the time of the query.
func RemoteHeads(branches []string) (map[string]string, error) {
	if len(branches) == 0 {
		return map[string]string{}, nil
	}
	args := []string{"ls-remote", "--heads", "origin"}
	for _, b := range branches {
		args = append(args, "refs/heads/"+b)
	}
	out, err := run(args...)
	if err != nil {
		return nil, fmt.Errorf("could not query remote branches: %w", err)
	}
	heads := make(map[string]string, len(branches))
	for _, line := range strings.Split(out, "\n") {
		// Each line is "<sha>\trefs/heads/<branch>"
		if idx := strings.Index(line, "\trefs/heads/"); idx >= 0 {
			sha := line[:idx]
			branch := line[idx+len("\trefs/heads/"):]
			if sha != "" && branch != "" {
				heads[branch] = sha
			}
		}
	}
	return heads, nil
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
		case line == "locked" || strings.HasPrefix(line, "locked "):
			current.Locked = true
			current.LockReason = strings.TrimSpace(strings.TrimPrefix(line, "locked"))
		case line == "prunable" || strings.HasPrefix(line, "prunable "):
			current.Prunable = true
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

type WorktreeState int

const (
	WorktreeStateClean WorktreeState = iota
	WorktreeStateDirty
	WorktreeStateStale
)

// InspectedWorktree combines a Git worktree record with its filesystem state.
type InspectedWorktree struct {
	Entry WorktreeEntry
	State WorktreeState
}

// InspectWorktree reports whether a worktree is clean, dirty, or stale.
// A path that disappears while Git checks its status is classified as stale.
func InspectWorktree(path string) (WorktreeState, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return WorktreeStateStale, nil
		}
		return WorktreeStateClean, fmt.Errorf("could not inspect worktree %q: %w", path, err)
	}
	if !info.IsDir() {
		return WorktreeStateStale, nil
	}

	out, err := runInDir(path, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		if info, statErr := os.Stat(path); (statErr != nil && os.IsNotExist(statErr)) || (statErr == nil && !info.IsDir()) {
			return WorktreeStateStale, nil
		}
		return WorktreeStateClean, fmt.Errorf("could not inspect worktree %q: %w", path, err)
	}
	if out != "" {
		return WorktreeStateDirty, nil
	}
	return WorktreeStateClean, nil
}

// InspectWorktrees reports filesystem state for worktree records in order.
func InspectWorktrees(entries []WorktreeEntry) ([]InspectedWorktree, error) {
	inspected := make([]InspectedWorktree, len(entries))
	for index, entry := range entries {
		state, err := InspectWorktree(entry.Path)
		if err != nil {
			return nil, err
		}
		inspected[index] = InspectedWorktree{Entry: entry, State: state}
	}
	return inspected, nil
}

// AnyCommitIsAncestor reports whether any ancestor is reachable from
// descendant in the current repository. It streams the descendant history and
// stops at the first matching commit.
func AnyCommitIsAncestor(ancestors []string, descendant string) (bool, error) {
	if len(ancestors) == 0 {
		return false, nil
	}
	wanted := make(map[string]struct{}, len(ancestors))
	for _, ancestor := range ancestors {
		wanted[ancestor] = struct{}{}
	}

	cmd := exec.Command("git", "rev-list", descendant)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return false, fmt.Errorf("could not read history for %q: %w", descendant, err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("could not read history for %q: %w", descendant, err)
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		if _, ok := wanted[scanner.Text()]; ok {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		_ = cmd.Wait()
		return false, fmt.Errorf("could not read history for %q: %w", descendant, err)
	}
	if err := cmd.Wait(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return false, fmt.Errorf("could not read history for %q: %s", descendant, msg)
		}
		return false, fmt.Errorf("could not read history for %q: %w", descendant, err)
	}
	return false, nil
}

// UnmergedCommitCount counts the commits on branch that target cannot reach.
// It is reported at the confirmation prompt, never used as a gate: deleting a
// branch drops its reflog along with the worktree's, so unmerged commits
// become unreachable, and the moment to say so is while the user is deciding.
func UnmergedCommitCount(dir, branch, target string) (int, error) {
	out, err := runInDir(dir, "rev-list", "--count", target+".."+branch)
	if err != nil {
		return 0, fmt.Errorf("could not count commits on %q not in %q: %w", branch, target, err)
	}
	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("could not parse commit count for %q: %w", branch, err)
	}
	return count, nil
}

// DeleteBranchAtSHA atomically deletes branch only when it still points at
// expectedSHA. This prevents deletion from discarding later work.
func DeleteBranchAtSHA(dir, branch, expectedSHA string) error {
	return withWorktreeMutationLock(dir, func() error {
		entries, err := worktreeListInDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.Branch == branch {
				return fmt.Errorf("branch %q is still checked out at worktree %q", branch, entry.Path)
			}
		}
		_, err = runInDir(dir, "update-ref", "-d", "refs/heads/"+branch, expectedSHA)
		if err != nil {
			return fmt.Errorf("branch %q could not be deleted at expected SHA %s: %w", branch, expectedSHA, err)
		}
		return nil
	})
}

// RemoveCreatedWorktree removes a worktree and its unchanged branch while
// holding the repository mutation lock for the complete operation. It is
// idempotent: a worktree or branch that is already gone leaves nothing to do
// rather than failing.
func RemoveCreatedWorktree(dir string, created CreatedWorktree) error {
	return withWorktreeMutationLock(dir, func() error {
		var errs []error
		entries, err := worktreeListInDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.Path != created.Path {
				continue
			}
			if entry.Branch != created.Branch {
				errs = append(errs, fmt.Errorf("worktree %q now checks out branch %q, expected %q", created.Path, entry.Branch, created.Branch))
				break
			}
			if _, err := runInDir(dir, "worktree", "remove", "--force", created.Path); err != nil {
				errs = append(errs, fmt.Errorf("failed to remove worktree %q: %w", created.Path, err))
			}
			break
		}

		entries, err = worktreeListInDir(dir)
		if err != nil {
			errs = append(errs, err)
			return errors.Join(errs...)
		}
		for _, entry := range entries {
			if entry.Branch == created.Branch {
				errs = append(errs, fmt.Errorf("branch %q is still checked out at worktree %q", created.Branch, entry.Path))
				return errors.Join(errs...)
			}
		}
		// A caller may be cleaning up after an operation that already removed
		// the branch. There is nothing left to compare-and-delete, so the
		// absent ref is the state being asked for, not a failure.
		missing, err := BranchMissing(dir, created.Branch)
		if err != nil {
			return errors.Join(append(errs, err)...)
		}
		if missing {
			return errors.Join(errs...)
		}
		if _, err := runInDir(dir, "update-ref", "-d", "refs/heads/"+created.Branch, created.SHA); err != nil {
			errs = append(errs, fmt.Errorf("branch %q could not be deleted at expected SHA %s: %w", created.Branch, created.SHA, err))
		}
		return errors.Join(errs...)
	})
}

// withWorktreeMutationLock serializes TreeMan worktree additions and guarded
// branch deletions for one repository. Git's ref transaction makes the SHA
// comparison atomic; this lock keeps another TreeMan process from creating a
// checkout between the checked-out-branch check and that transaction. Direct
// Git worktree mutations do not participate in this advisory lock.
func withWorktreeMutationLock(dir string, operation func() error) error {
	commonDir, err := CommonDir(dir)
	if err != nil {
		return err
	}

	lock, err := os.OpenFile(filepath.Join(commonDir, "treeman-worktree.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("could not open worktree mutation lock: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("could not lock worktree mutations: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return operation()
}

// CommonDir returns the absolute Git common directory shared by all worktrees.
func CommonDir(dir string) (string, error) {
	commonDir, err := runInDir(dir, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("could not determine Git common directory: %w", err)
	}
	if filepath.IsAbs(commonDir) {
		return filepath.Clean(commonDir), nil
	}
	base := dir
	if base == "" {
		base, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("could not determine current directory: %w", err)
		}
	}
	absolute, err := filepath.Abs(filepath.Join(base, commonDir))
	if err != nil {
		return "", fmt.Errorf("could not normalize Git common directory: %w", err)
	}
	return filepath.Clean(absolute), nil
}

// WorktreeID returns the linked-worktree administration directory name.
// It is stable for the lifetime of that linked worktree and remains available
// before Git removes the worktree administration directory.
func WorktreeID(dir string) (string, error) {
	gitDir, err := runInDir(dir, "rev-parse", "--git-dir")
	if err != nil {
		return "", fmt.Errorf("could not determine Git directory: %w", err)
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(dir, gitDir)
	}
	gitDir, err = filepath.Abs(gitDir)
	if err != nil {
		return "", fmt.Errorf("could not normalize Git directory: %w", err)
	}
	gitDir = filepath.Clean(gitDir)
	commonDir, err := CommonDir(dir)
	if err != nil {
		return "", err
	}
	worktreesDir := filepath.Join(commonDir, "worktrees")
	if filepath.Clean(filepath.Dir(gitDir)) != filepath.Clean(worktreesDir) {
		return "", fmt.Errorf("%q is not a linked worktree", dir)
	}
	id := filepath.Base(gitDir)
	if id == "." || id == string(filepath.Separator) || id == "" {
		return "", fmt.Errorf("invalid linked worktree ID for %q", dir)
	}
	return id, nil
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

// WorktreeAddExisting creates a local branch and linked worktree from an
// existing remote branch.
func WorktreeAddExisting(path, branch string) error {
	_, err := CreateWorktreeFromRemote(path, branch)
	return err
}

// CreateWorktreeFromRemote creates a worktree from origin/branch.
func CreateWorktreeFromRemote(path, branch string) (CreatedWorktree, error) {
	return createWorktree("", path, branch, "origin/"+branch)
}

func worktreeAdd(dir, path, branch, startPoint string) error {
	_, err := createWorktree(dir, path, branch, startPoint)
	return err
}

func createWorktree(dir, path, branch, startPoint string) (CreatedWorktree, error) {
	var created CreatedWorktree
	err := withWorktreeMutationLock(dir, func() error {
		sha, err := runInDir(dir, "rev-parse", "--verify", startPoint+"^{commit}")
		if err != nil {
			return fmt.Errorf("could not resolve worktree start point %q: %w", startPoint, err)
		}
		ref := "refs/heads/" + branch
		if _, err := runInDir(dir, "update-ref", ref, sha, ""); err != nil {
			return fmt.Errorf("could not create branch %q: %w", branch, err)
		}

		cmd := exec.Command("git", "worktree", "add", path, branch)
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(), "HUSKY=0")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			rollbackErr := rollbackWorktreeCreation(dir, path, branch, sha)
			msg := strings.TrimSpace(stderr.String())
			if msg != "" {
				return errors.Join(fmt.Errorf("git worktree add: %s", msg), rollbackErr)
			}
			return errors.Join(fmt.Errorf("git worktree add: %w", err), rollbackErr)
		}
		created = CreatedWorktree{Path: path, Branch: branch, SHA: sha}
		return nil
	})
	return created, err
}

func rollbackWorktreeCreation(dir, path, branch, sha string) error {
	var errs []error
	entries, err := worktreeListInDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Path == path && entry.Branch == branch {
			if _, removeErr := runInDir(dir, "worktree", "remove", "--force", path); removeErr != nil {
				errs = append(errs, fmt.Errorf("rolling back worktree %q: %w", path, removeErr))
			}
			break
		}
	}

	entries, err = worktreeListInDir(dir)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, entry := range entries {
		if entry.Branch == branch {
			errs = append(errs, fmt.Errorf("cannot roll back branch %q: it is checked out at worktree %q", branch, entry.Path))
			return errors.Join(errs...)
		}
	}
	if _, err := runInDir(dir, "update-ref", "-d", "refs/heads/"+branch, sha); err != nil {
		errs = append(errs, fmt.Errorf("rolling back branch %q: %w", branch, err))
	}
	return errors.Join(errs...)
}

// LocalBranchNames returns the name of every local branch. Unlike BranchSHAs
// it needs no candidate list, so a caller that is still streaming remote
// branches can filter them without knowing the full set up front.
func LocalBranchNames() (map[string]struct{}, error) {
	out, err := run("for-each-ref", "--format=%(refname:short)", "refs/heads/")
	if err != nil {
		return nil, fmt.Errorf("could not list local branches: %w", err)
	}
	names := make(map[string]struct{})
	for _, line := range strings.Split(out, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names[name] = struct{}{}
		}
	}
	return names, nil
}
