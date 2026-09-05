package git

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// trashDirName is a durable cleanup queue under the Git common directory.
const trashDirName = "treeman/trash"

const (
	cleanupOriginalPathFile = ".treeman-original-path"
	cleanupReadyFile        = ".treeman-cleanup-ready"
	stagedWorktreeName      = "worktree"
)

// detachRemoval is swapped in tests that need cleanup to finish synchronously.
var detachRemoval = detachRemoveAll

// RemovalScope classifies a failed removal by what it left behind. A caller
// removing worktrees in a batch needs that answer to decide whether the next
// candidate is still worth attempting, and it cannot be inferred from which
// durable transitions completed: a capture that could not be restored leaves
// the worktree registered too, and a repository whose state cannot be read
// removes nothing either.
type RemovalScope int

const (
	// RemovalScopeCandidate is a decision about this worktree alone. Nothing
	// durable changed and the other candidates are unaffected, so a batch may
	// skip it and keep going.
	RemovalScopeCandidate RemovalScope = iota
	// RemovalScopeRepository is a failure to read or prepare repository-wide
	// state. Nothing changed, but every remaining candidate would fail the same
	// way, so a batch stops rather than reporting one problem once per worktree.
	RemovalScopeRepository
	// RemovalScopeCaptureRetained means the worktree directory was moved into
	// the cleanup queue and could not be put back. The registration survives,
	// but the directory is no longer at the registered path -- most often
	// because something else came to occupy it, which is what stopped the
	// restoring rename. This is not an untouched worktree, and putting it back
	// is a judgement about whatever is in the way, so Capture names where the
	// directory is and recovery is manual.
	RemovalScopeCaptureRetained
	// RemovalScopeBranchRetained means the worktree was unregistered and its
	// branch outlived it.
	RemovalScopeBranchRetained
)

// RemovalError is the classification every RemoveWorktreeAndBranch failure
// carries. Capture names where a retained capture remains, and is empty for
// every other scope.
type RemovalError struct {
	Scope   RemovalScope
	Capture string
	Err     error
}

func (e *RemovalError) Error() string { return e.Err.Error() }

func (e *RemovalError) Unwrap() error { return e.Err }

func candidateRefusal(err error) error {
	return &RemovalError{Scope: RemovalScopeCandidate, Err: err}
}

func repositoryFailure(err error) error {
	return &RemovalError{Scope: RemovalScopeRepository, Err: err}
}

func branchRetained(err error) error {
	return &RemovalError{Scope: RemovalScopeBranchRetained, Err: err}
}

// RemoveWorktreeResult describes which durable state transitions completed. It
// reports progress, not classification: what a failure left behind is the
// RemovalError's answer to give, because a registration that survives says
// nothing about whether the directory is still standing at it.
// File cleanup may remain queued after both Git resources have been removed.
// CleanupJob names that queued directory so callers can ask whether it is still
// on disk when they report, rather than being handed an answer sampled when the
// removal returned.
type RemoveWorktreeResult struct {
	WorktreeUnregistered bool
	BranchDeleted        bool
	CleanupJob           string
	CleanupError         error
}

// CleanupPending reports whether a queued cleanup job still has files to
// unlink. Unlinking is detached and usually finishes in the time it takes to
// print a result, so this must be answered when the answer is used: sampling it
// the instant the background process starts reports every removal as
// outstanding, which tells the user nothing about the one that actually is.
func CleanupPending(job string) bool {
	return job != "" && pathExists(job)
}

type stagedWorktree struct {
	job  string
	path string
}

// RemoveWorktreeAndBranch validates and removes one linked worktree and its
// branch while holding TreeMan's repository mutation lock. The directory is
// renamed before identity and cleanliness are checked, so the checks apply to
// the exact object that can later be unlinked.
func RemoveWorktreeAndBranch(mainRoot, path, branch, expectedSHA string, force bool) (RemoveWorktreeResult, error) {
	var result RemoveWorktreeResult
	err := withWorktreeMutationLock(mainRoot, func() error {
		commonDir, err := CommonDir(mainRoot)
		if err != nil {
			return repositoryFailure(err)
		}
		trashRoot := filepath.Join(commonDir, filepath.FromSlash(trashDirName))
		if err := os.MkdirAll(trashRoot, 0o700); err != nil {
			return repositoryFailure(fmt.Errorf("could not create worktree cleanup queue: %w", err))
		}

		entries, err := worktreeListInDir(mainRoot)
		if err != nil {
			return repositoryFailure(err)
		}
		jobs, err := inspectCleanupQueue(trashRoot)
		if err != nil {
			return repositoryFailure(err)
		}
		result.CleanupError = retryTrashCleanup(trashRoot, jobs)

		entry, branchSHA, err := ValidateWorktreeRemoval(mainRoot, entries, path, branch, expectedSHA)
		if err != nil {
			return candidateRefusal(err)
		}

		// A capture left behind by an earlier run is this path's problem alone:
		// the removal it belonged to already reported the recovery, and every
		// other candidate is still removable.
		for _, job := range jobs {
			if job.state == cleanupUnresolved && sameRemovalPath(job.originalPath, entry.Path) {
				return candidateRefusal(fmt.Errorf("cannot remove worktree %q: unresolved captured data remains at %q; recover it before retrying removal", entry.Path, filepath.Join(job.path, stagedWorktreeName)))
			}
		}

		expectedGitDir, err := linkedRegistrationDir(commonDir, entry.Path)
		if err != nil {
			return err
		}
		staged, missing, err := stageWorktreeRemoval(trashRoot, entry.Path)
		if err != nil {
			return err
		}
		if !missing {
			if err := validateStagedWorktree(mainRoot, entry.Path, staged.path, expectedGitDir, force); err != nil {
				return restoreStagedWorktree(entry.Path, staged, err)
			}
		}

		if err := runWorktreeRemove(mainRoot, entry.Path); err != nil {
			removeErr := fmt.Errorf("failed to remove worktree %q: %w", entry.Path, err)
			if staged != nil {
				return restoreStagedWorktree(entry.Path, staged, removeErr)
			}
			return candidateRefusal(removeErr)
		}
		result.WorktreeUnregistered = true

		if staged != nil {
			// The readiness marker is what puts the job in the queue: without
			// it the capture stays unresolved and no later removal retries it,
			// so it must not be reported as cleanup that is merely pending.
			if err := os.WriteFile(filepath.Join(staged.job, cleanupReadyFile), nil, 0o600); err != nil {
				result.CleanupError = errors.Join(result.CleanupError, fmt.Errorf("could not mark staged worktree %q ready for cleanup: %w", staged.path, err))
			} else {
				result.CleanupJob = staged.job
				if err := detachRemoval(trashRoot, staged.job); err != nil {
					result.CleanupError = errors.Join(result.CleanupError, err)
				}
			}
		}

		// Past this point the registration is gone, so every remaining failure
		// leaves the branch behind rather than refusing the removal.
		entries, err = worktreeListInDir(mainRoot)
		if err != nil {
			return branchRetained(err)
		}
		for _, other := range entries {
			if other.Branch == branch {
				return branchRetained(fmt.Errorf("branch %q is still checked out at worktree %q", branch, other.Path))
			}
		}
		if _, err := runInDir(mainRoot, "update-ref", "-d", "refs/heads/"+branch, branchSHA); err != nil {
			return branchRetained(fmt.Errorf("branch %q could not be deleted at expected SHA %s: %w", branch, branchSHA, err))
		}
		result.BranchDeleted = true
		return nil
	})
	return result, classifiedRemovalFailure(err)
}

// classifiedRemovalFailure keeps the scope contract total at the boundary.
// Every failure inside the locked operation classifies itself where it happens,
// so the only thing that reaches here unclassified is acquiring the mutation
// lock: that fails before this worktree is reached at all, and the next
// candidate would meet it too.
func classifiedRemovalFailure(err error) error {
	if err == nil {
		return nil
	}
	var failure *RemovalError
	if errors.As(err, &failure) {
		return err
	}
	return repositoryFailure(err)
}

// ValidateWorktreeRemoval checks registration and branch identity for preflight
// and execution. Its result is a snapshot, not authorization: execution must
// supply freshly read entries and call it under the repository mutation lock.
// Directory identity and cleanliness are checked separately after staging.
func ValidateWorktreeRemoval(mainRoot string, entries []WorktreeEntry, path, branch, expectedSHA string) (WorktreeEntry, string, error) {
	entry, err := registeredWorktreeAt(entries, path)
	if err != nil {
		return WorktreeEntry{}, "", err
	}
	if sameRemovalPath(entry.Path, mainRoot) {
		return WorktreeEntry{}, "", fmt.Errorf("cannot delete the main worktree")
	}
	if entry.Branch != branch {
		return WorktreeEntry{}, "", fmt.Errorf("worktree %q is checked out on branch %q, not %q", entry.Path, entry.Branch, branch)
	}
	// A lock protects even an absent directory; force never waives it.
	if entry.Locked {
		reason := ""
		if entry.LockReason != "" {
			reason = ": " + entry.LockReason
		}
		return WorktreeEntry{}, "", fmt.Errorf("worktree %q is locked%s; run `git -C %q worktree unlock %q` first", entry.Path, reason, mainRoot, entry.Path)
	}
	for _, other := range entries {
		if other.Branch == branch && !sameRemovalPath(other.Path, entry.Path) {
			return WorktreeEntry{}, "", fmt.Errorf("branch %q is also checked out at worktree %q", branch, other.Path)
		}
	}
	branchSHA, err := runInDir(mainRoot, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch+"^{commit}")
	if err != nil {
		return WorktreeEntry{}, "", fmt.Errorf("cannot remove worktree %q because branch %q could not be resolved: %w", entry.Path, branch, err)
	}
	if expectedSHA != "" && branchSHA != expectedSHA {
		return WorktreeEntry{}, "", fmt.Errorf("cannot remove worktree %q: branch %q moved after merge verification (expected %s, found %s)", entry.Path, branch, expectedSHA, branchSHA)
	}
	return entry, branchSHA, nil
}

func registeredWorktreeAt(entries []WorktreeEntry, path string) (WorktreeEntry, error) {
	for _, entry := range entries {
		if sameRemovalPath(entry.Path, path) {
			return entry, nil
		}
	}
	return WorktreeEntry{}, fmt.Errorf("path %q is not a linked worktree", path)
}

// linkedRegistrationDir finds the authoritative registration that records the
// requested path. It deliberately does not inspect the object currently at the
// path; that object is checked against this registration only after staging.
func linkedRegistrationDir(commonDir, worktreePath string) (string, error) {
	registrations := filepath.Join(commonDir, "worktrees")
	entries, err := os.ReadDir(registrations)
	if err != nil {
		// Every removal reads the same directory, so losing it is the
		// repository's failure rather than this worktree's.
		return "", repositoryFailure(fmt.Errorf("cannot remove worktree %q: could not read linked-worktree registrations: %w", worktreePath, err))
	}
	var readErrors []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		gitDir := filepath.Join(registrations, entry.Name())
		recorded, err := os.ReadFile(filepath.Join(gitDir, "gitdir"))
		if err != nil {
			readErrors = append(readErrors, err)
			continue
		}
		recordedGitFile := strings.TrimSpace(string(recorded))
		if !filepath.IsAbs(recordedGitFile) {
			recordedGitFile = filepath.Join(gitDir, recordedGitFile)
		}
		if sameRemovalPath(filepath.Dir(recordedGitFile), worktreePath) {
			return canonicalRemovalPath(gitDir), nil
		}
	}
	if len(readErrors) > 0 {
		return "", candidateRefusal(fmt.Errorf("cannot remove worktree %q: its registration is unreadable: %w", worktreePath, errors.Join(readErrors...)))
	}
	return "", candidateRefusal(fmt.Errorf("cannot remove worktree %q: its linked-worktree registration could not be found", worktreePath))
}

// stageWorktreeRemoval moves exactly one filesystem object into a new cleanup
// job. Only ENOENT means there was no object to protect; files and every other
// filesystem error remain deletion refusals.
func stageWorktreeRemoval(trashRoot, path string) (*stagedWorktree, bool, error) {
	// An unusable queue is the repository's failure: it stops the next removal
	// exactly as it stopped this one.
	job, err := os.MkdirTemp(trashRoot, "worktree-")
	if err != nil {
		return nil, false, repositoryFailure(fmt.Errorf("could not create worktree cleanup job: %w", err))
	}
	if err := os.WriteFile(filepath.Join(job, cleanupOriginalPathFile), []byte(path), 0o600); err != nil {
		_ = os.RemoveAll(job)
		return nil, false, repositoryFailure(fmt.Errorf("could not record staged worktree path: %w", err))
	}

	staged := &stagedWorktree{job: job, path: filepath.Join(job, stagedWorktreeName)}
	if err := os.Rename(path, staged.path); err != nil {
		_ = os.RemoveAll(job)
		if os.IsNotExist(err) {
			_, statErr := os.Lstat(path)
			if os.IsNotExist(statErr) {
				return nil, true, nil
			}
			if statErr != nil {
				return nil, false, candidateRefusal(fmt.Errorf("could not determine whether worktree %q exists: %w", path, statErr))
			}
		}
		return nil, false, candidateRefusal(fmt.Errorf("could not stage worktree %q for removal: %w", path, err))
	}
	return staged, false, nil
}

func validateStagedWorktree(mainRoot, originalPath, stagedPath, expectedGitDir string, force bool) error {
	if err := validateWorktreeIdentity(originalPath, stagedPath, expectedGitDir); err != nil {
		return err
	}
	// --force waives the answer, so the question is not worth asking: listing
	// every untracked file in a dependency tree is the cost this whole path
	// exists to avoid, and it would be paid holding the mutation lock.
	if force {
		return nil
	}
	status, err := runInDir(mainRoot, "--git-dir="+expectedGitDir, "--work-tree="+stagedPath, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return fmt.Errorf("could not inspect staged worktree %q: %w", stagedPath, err)
	}
	if status != "" {
		return fmt.Errorf("worktree %q has uncommitted or untracked changes; use --force to delete it", stagedPath)
	}
	return nil
}

// Inspect the captured object, but resolve its relative references from where
// Git registered it, before staging changed its location.
func validateWorktreeIdentity(originalPath, stagedPath, expectedGitDir string) error {
	info, err := os.Lstat(stagedPath)
	if err != nil {
		return fmt.Errorf("cannot remove worktree %q: could not inspect the staged object: %w", stagedPath, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("cannot remove worktree %q: the registered path contains a non-directory object", stagedPath)
	}

	gitFile := filepath.Join(stagedPath, ".git")
	gitFileInfo, err := os.Lstat(gitFile)
	if err != nil {
		return fmt.Errorf("cannot remove worktree %q: it no longer looks like a Git worktree: %w", stagedPath, err)
	}
	if !gitFileInfo.Mode().IsRegular() {
		return fmt.Errorf("cannot remove worktree %q: its .git entry is not the expected linked-worktree file", stagedPath)
	}
	recorded, err := os.ReadFile(gitFile)
	if err != nil {
		return fmt.Errorf("cannot remove worktree %q: its .git entry is unreadable: %w", stagedPath, err)
	}
	gitDir, ok := strings.CutPrefix(strings.TrimSpace(string(recorded)), "gitdir:")
	if !ok || strings.TrimSpace(gitDir) == "" {
		return fmt.Errorf("cannot remove worktree %q: its .git entry is invalid", stagedPath)
	}
	gitDir = strings.TrimSpace(gitDir)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(originalPath, gitDir)
	}
	if !sameRemovalPath(gitDir, expectedGitDir) {
		return fmt.Errorf("cannot remove worktree %q: the staged directory belongs to a different registration", stagedPath)
	}

	return nil
}

// restoreStagedWorktree puts the capture back and reports what the failure
// left behind. A restored worktree is untouched and refuses only itself; a
// capture that could not be restored is a registration whose directory now
// sits elsewhere, which no caller may treat as a worktree it merely declined
// to remove.
func restoreStagedWorktree(originalPath string, staged *stagedWorktree, cause error) error {
	if err := renameNoReplace(staged.path, originalPath); err != nil {
		return &RemovalError{
			Scope:   RemovalScopeCaptureRetained,
			Capture: staged.path,
			Err:     fmt.Errorf("%w (the captured worktree could not be restored and remains at %q: %v)", cause, staged.path, err),
		}
	}
	if err := os.RemoveAll(staged.job); err != nil {
		return candidateRefusal(errors.Join(cause, fmt.Errorf("could not remove restored worktree staging metadata at %q: %w", staged.job, err)))
	}
	return candidateRefusal(cause)
}

func runWorktreeRemove(dir, path string) error {
	_, err := runInDir(dir, "worktree", "remove", path)
	return err
}

type cleanupState int

const (
	cleanupUnresolved cleanupState = iota
	cleanupEligible
	cleanupActive
)

type cleanupJob struct {
	path         string
	originalPath string
	state        cleanupState
}

// Queue inspection owns recovery classification. Registration loss is never
// evidence that a captured directory passed validation.
func inspectCleanupQueue(trashRoot string) ([]cleanupJob, error) {
	entries, err := os.ReadDir(trashRoot)
	if err != nil {
		return nil, fmt.Errorf("could not read worktree cleanup queue: %w", err)
	}
	var jobs []cleanupJob
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		job := cleanupJob{path: filepath.Join(trashRoot, entry.Name()), state: cleanupEligible}
		if _, err := os.Stat(filepath.Join(job.path, cleanupReadyFile)); os.IsNotExist(err) {
			original, err := os.ReadFile(filepath.Join(job.path, cleanupOriginalPathFile))
			if err == nil {
				job.originalPath = strings.TrimSpace(string(original))
				if job.originalPath == "" {
					return nil, fmt.Errorf("empty cleanup metadata for %q", job.path)
				}
				// Metadata alone protects nothing. Staging records the original
				// path before the rename, so a process that stopped in that
				// window -- or a restoration whose metadata outlived the
				// directory it put back -- leaves a job with nothing captured
				// in it. Calling that unresolved would refuse every later
				// removal of the path forever, and name a recovery location
				// that does not exist.
				if _, statErr := os.Lstat(filepath.Join(job.path, stagedWorktreeName)); statErr == nil {
					job.state = cleanupUnresolved
				} else if !os.IsNotExist(statErr) {
					return nil, fmt.Errorf("could not inspect captured worktree for %q: %w", job.path, statErr)
				}
			} else if !os.IsNotExist(err) {
				return nil, fmt.Errorf("could not read cleanup metadata for %q: %w", job.path, err)
			}
		} else if err != nil {
			return nil, fmt.Errorf("could not inspect cleanup state for %q: %w", job.path, err)
		}
		// Metadata-free directories are legacy cleanup jobs. Unresolved jobs
		// stay protected even if a cleanup lock happens to exist.
		if job.state == cleanupEligible {
			active, err := cleanupJobActive(trashRoot, job.path)
			if err != nil {
				return nil, err
			}
			if active {
				job.state = cleanupActive
			}
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func retryTrashCleanup(trashRoot string, jobs []cleanupJob) error {
	var errs []error
	for _, queued := range jobs {
		if queued.state != cleanupEligible {
			continue
		}
		job := queued.path
		if failure, err := os.ReadFile(cleanupErrorPath(trashRoot, job)); err == nil {
			message := strings.TrimSpace(string(failure))
			if message == "" {
				message = "background removal exited unsuccessfully"
			}
			errs = append(errs, fmt.Errorf("previous file cleanup for %q failed: %s", job, message))
		} else if !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("could not read cleanup diagnostic for %q: %w", job, err))
		}
		if err := detachRemoval(trashRoot, job); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// detachRemoveAll starts an unlinking process that outlives this one. The job
// must be a canonical direct child of this repository's actual trash root.
// stderr remains as a durable diagnostic if rm exits unsuccessfully.
func detachRemoveAll(trashRoot, job string) error {
	rootInfo, err := os.Stat(trashRoot)
	if err != nil {
		return fmt.Errorf("could not inspect trash root %q: %w", trashRoot, err)
	}
	if err := ensureCleanupJobContained(trashRoot, job); err != nil {
		return err
	}

	rootFD, err := unix.Open(trashRoot, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("could not open trash root %q: %w", trashRoot, err)
	}
	rootFile := os.NewFile(uintptr(rootFD), trashRoot)
	openedRootInfo, err := rootFile.Stat()
	if err != nil {
		_ = rootFile.Close()
		return fmt.Errorf("could not inspect opened trash root %q: %w", trashRoot, err)
	}
	if !os.SameFile(rootInfo, openedRootInfo) {
		_ = rootFile.Close()
		return fmt.Errorf("refusing to detach removal of %q: trash root changed during validation", job)
	}

	jobName := filepath.Base(job)
	jobFD, err := unix.Openat(rootFD, jobName, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		_ = rootFile.Close()
		return fmt.Errorf("could not open cleanup job %q: %w", job, err)
	}
	jobFile := os.NewFile(uintptr(jobFD), job)

	lockName := cleanupLockName(job)
	lockFD, err := unix.Openat(rootFD, lockName, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC, 0o600)
	if err != nil {
		_ = jobFile.Close()
		_ = rootFile.Close()
		return fmt.Errorf("could not open cleanup lock for %q: %w", job, err)
	}
	lock := os.NewFile(uintptr(lockFD), filepath.Join(trashRoot, lockName))
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		_ = jobFile.Close()
		_ = rootFile.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil
		}
		return fmt.Errorf("could not lock cleanup job %q: %w", job, err)
	}

	errorName := cleanupErrorName(job)
	errorFD, err := unix.Openat(rootFD, errorName, unix.O_CREAT|unix.O_WRONLY|unix.O_TRUNC|unix.O_CLOEXEC, 0o600)
	if err != nil {
		_ = lock.Close()
		_ = jobFile.Close()
		_ = rootFile.Close()
		return fmt.Errorf("could not open cleanup diagnostic for %q: %w", job, err)
	}
	errorFile := os.NewFile(uintptr(errorFD), filepath.Join(trashRoot, errorName))

	cmd := cleanupCommand(trashRoot, jobName, errorName, lockName, lock, rootFile, jobFile)
	cmd.Stderr = errorFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		message := fmt.Sprintf("could not start background removal: %v", err)
		_, _ = errorFile.WriteString(message)
		_ = errorFile.Close()
		_ = lock.Close()
		_ = jobFile.Close()
		_ = rootFile.Close()
		return fmt.Errorf("could not start background removal of %q: %w", job, err)
	}
	_ = jobFile.Close()
	_ = errorFile.Close()
	_ = lock.Close()
	_ = rootFile.Close()
	go func() { _ = cmd.Wait() }()
	return nil
}

func cleanupJobActive(trashRoot, job string) (bool, error) {
	lock, err := os.OpenFile(cleanupLockPath(trashRoot, job), os.O_RDWR, 0)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("could not inspect cleanup lock for %q: %w", job, err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return true, nil
		}
		return false, fmt.Errorf("could not inspect cleanup lock for %q: %w", job, err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		return false, fmt.Errorf("could not release cleanup lock for %q: %w", job, err)
	}
	return false, nil
}

func ensureCleanupJobContained(trashRoot, job string) error {
	root, err := canonicalExistingPath(trashRoot)
	if err != nil {
		return fmt.Errorf("refusing to detach removal of %q: invalid trash root: %w", job, err)
	}
	contained, err := canonicalExistingPath(job)
	if err != nil {
		return fmt.Errorf("refusing to detach removal of %q: not a staged worktree: %w", job, err)
	}
	relative, err := filepath.Rel(root, contained)
	if err != nil || relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.Dir(relative) != "." {
		return fmt.Errorf("refusing to detach removal of %q: not contained in trash root %q", job, trashRoot)
	}
	return nil
}

func cleanupLockPath(trashRoot, job string) string {
	return filepath.Join(trashRoot, cleanupLockName(job))
}

func cleanupErrorPath(trashRoot, job string) string {
	return filepath.Join(trashRoot, cleanupErrorName(job))
}

func cleanupLockName(job string) string {
	return "." + filepath.Base(job) + ".lock"
}

func cleanupErrorName(job string) string {
	return "." + filepath.Base(job) + ".error"
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil || !os.IsNotExist(err)
}

func sameRemovalPath(a, b string) bool {
	return canonicalRemovalPath(a) == canonicalRemovalPath(b)
}

func canonicalRemovalPath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(absolute)
}

func canonicalExistingPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

// EnsureHoldsWorktree verifies that the directory at path is still the linked
// worktree the repository has registered there.
//
// Git makes this check itself before `git worktree remove` and refuses without
// it, --force included. Renaming the directory instead means Git's validation
// never runs, so the guarantee has to be made here. What it protects against
// is narrow and unrecoverable: a registration whose directory was replaced --
// an unrelated clone that came to sit at the path, or a sibling worktree moved
// onto it -- would otherwise be renamed away and unlinked.
//
// The test is Git's own. The directory's Git directory must be this
// repository's registration for it, and that registration's `gitdir` file must
// name this directory back. Asking only which repository the occupant belongs
// to is the weaker half of the question: a sibling worktree's Git directory
// also lives under the common directory, so that alone would accept one that
// had been moved here.
//
// This is the plan-time half of the identity check: it names the problem
// before the user is asked to confirm. validateStagedWorktree makes the same
// guarantee about the captured directory, under the lock, after the rename.
func EnsureHoldsWorktree(mainRoot, path string) error {
	commonDir, err := CommonDir(mainRoot)
	if err != nil {
		return err
	}

	gitDir, err := linkedRegistrationDir(commonDir, path)
	if err != nil {
		return err
	}
	return validateWorktreeIdentity(path, path, gitDir)
}

// WorktreeDirectoryMissing reports whether the worktree directory is absent,
// without asking Git anything about it. The ownership and clean checks both
// need a directory to read, so this decides whether they apply at all.
func WorktreeDirectoryMissing(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	return !info.IsDir()
}
