package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// trashDirName is where staged worktrees wait to be deleted, under the Git
// common directory so nothing in it is visible from the user's workspace.
const trashDirName = "treeman/trash"

// detachRemoval is swapped in tests that need the deletion to complete before
// they assert on the filesystem.
var detachRemoval = detachRemoveAll

// WorktreeRemove removes the linked worktree at path.
//
// Removing a populated worktree is almost entirely the cost of unlinking its
// files -- a dependency tree is tens or hundreds of thousands of them, and Git
// does that work synchronously before `git worktree remove` returns. So the
// directory is renamed out of the workspace first, which on one filesystem is
// a metadata operation, and the unlinking is handed to a detached process. The
// user's workspace is clear as soon as the rename lands.
//
// force is honoured by the fallback only. The staged path does not consult Git
// about the working tree at all, so the caller owns that decision and must
// have made it before calling: see EnsureHoldsWorktree, whose docs say why the
// ownership half of it cannot be delegated here.
func WorktreeRemove(mainRoot, path string, force bool) error {
	// Every Git call here runs from mainRoot. The worktree directory is about
	// to stop existing at its own path -- and may already be gone -- so it
	// cannot be the working directory Git is asked from, and neither can
	// whatever this process happens to be sitting in, which is frequently the
	// worktree being deleted.
	commonDir, err := CommonDir(mainRoot)
	if err != nil {
		return err
	}

	staged := stageWorktreeRemoval(commonDir, path)
	if staged == "" {
		return removeWorktreeDirect(mainRoot, path, force)
	}

	// The directory has moved, so Git resolves the entry by its recorded path
	// and unregisters it without running the validation it would apply to a
	// directory that is still there.
	if err := runWorktreeRemove(mainRoot, path, false); err != nil {
		// The registration outlived the rename. Put the directory back so the
		// repository is not left with an entry pointing at nothing and a
		// worktree nobody can find.
		if renameErr := os.Rename(staged, path); renameErr != nil {
			return fmt.Errorf("failed to remove worktree %q: %w (its directory is staged at %q and could not be restored: %v)", path, err, staged, renameErr)
		}
		return fmt.Errorf("failed to remove worktree %q: %w", path, err)
	}

	if err := detachRemoval(staged); err != nil {
		// Nothing references the staged copy any more, so finishing here costs
		// the wait but never correctness.
		if removeErr := os.RemoveAll(staged); removeErr != nil {
			return fmt.Errorf("removed worktree %q but could not delete its staged copy at %q: %w", path, staged, removeErr)
		}
	}
	return nil
}

// stageWorktreeRemoval renames the worktree directory into the repository's
// trash directory and returns the staged path. It returns an empty path when
// the rename could not be made -- a worktree on another filesystem gets EXDEV,
// and one whose directory is already gone has nothing to move -- and the
// caller falls back to a direct removal.
func stageWorktreeRemoval(commonDir, path string) string {
	trashDir := filepath.Join(commonDir, filepath.FromSlash(trashDirName))
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		return ""
	}

	staged := stagedPath(trashDir, path)
	if err := os.Rename(path, staged); err != nil {
		return ""
	}
	return staged
}

// stagedPath names a staging directory that cannot collide with a concurrent
// removal of a same-named worktree in another repository checkout.
func stagedPath(trashDir, worktreePath string) string {
	name := filepath.Base(worktreePath)
	if name == "" || name == string(filepath.Separator) || name == "." {
		name = "worktree"
	}
	return filepath.Join(trashDir, fmt.Sprintf("%s-%d-%d", name, time.Now().UnixNano(), os.Getpid()))
}

// detachRemoveAll starts an unlinking process that outlives this one.
//
// The path is always one this process just created by renaming into the trash
// directory, and it is re-checked against that directory here, because the
// argument to an unsupervised recursive delete is the one thing in this file
// that cannot be allowed to be wrong.
func detachRemoveAll(staged string) error {
	trashDir := filepath.Dir(staged)
	if filepath.Base(filepath.Dir(trashDir)) != "treeman" || filepath.Base(trashDir) != "trash" {
		return fmt.Errorf("refusing to detach removal of %q: not a staged worktree", staged)
	}

	cmd := exec.Command("rm", "-rf", "--", staged)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start background removal of %q: %w", staged, err)
	}
	// Reap the child if it finishes while this process is still alive, so a
	// short-lived removal does not linger as a zombie.
	go func() { _ = cmd.Wait() }()
	return nil
}

func removeWorktreeDirect(dir, path string, force bool) error {
	if err := runWorktreeRemove(dir, path, force); err != nil {
		return fmt.Errorf("failed to remove worktree %q: %w", path, err)
	}
	return nil
}

func runWorktreeRemove(dir, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	_, err := runInDir(dir, args...)
	return err
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
// Callers run this before inspecting the working tree, not after. A status
// read against a foreign occupant reports that repository's changes as this
// worktree's, and offers --force as the remedy -- which is the one flag that
// would carry the removal through.
func EnsureHoldsWorktree(mainRoot, path string) error {
	commonDir, err := CommonDir(mainRoot)
	if err != nil {
		return err
	}

	// A directory whose Git directory cannot be resolved at all is the
	// strongest form of "not this worktree", so the failure is a refusal.
	gitDir, err := runInDir(path, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return fmt.Errorf("cannot remove worktree %q: it no longer looks like a Git worktree: %w", path, err)
	}
	gitDir = resolvePath(gitDir)

	// The main worktree has no registration to point back at: its Git
	// directory is the common directory, and that equality is the whole test.
	if gitDir == resolvePath(commonDir) {
		return nil
	}

	registrations := filepath.Join(resolvePath(commonDir), "worktrees")
	if filepath.Dir(gitDir) != registrations {
		return fmt.Errorf("cannot remove worktree %q: the directory belongs to a different repository", path)
	}

	recorded, err := os.ReadFile(filepath.Join(gitDir, "gitdir"))
	if err != nil {
		return fmt.Errorf("cannot remove worktree %q: its registration is unreadable: %w", path, err)
	}
	// The file records the worktree's `.git` entry, so the worktree is its
	// parent directory.
	recordedWorktree := filepath.Dir(resolvePath(strings.TrimSpace(string(recorded))))
	if recordedWorktree != resolvePath(path) {
		return fmt.Errorf("cannot remove worktree %q: the registration there records %q instead", path, recordedWorktree)
	}
	return nil
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

// resolvePath canonicalises a path so comparisons survive symlinked worktree
// or repository locations.
func resolvePath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}
