package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/shoutcape/treeman/internal/fsutil"
	"github.com/shoutcape/treeman/internal/git"
)

// setupLockDirectory holds one lock file per linked worktree, beside the other
// TreeMan subsystems under the Git common directory.
const setupLockDirectory = "treeman/setup"

// withSetupLock serializes TreeMan's setup runs for one worktree. It is held
// across file copying, database work, installers, and hooks, so two runs
// cannot install into the same tree at once.
//
// The lock does not wait. A second run that queued behind an installer would
// look like a hang with nothing on screen to explain it, so it says what is
// happening and exits instead. Setup in a different worktree is unaffected:
// the lock is per worktree, not per repository.
func withSetupLock(worktreePath string, fn func() error) error {
	commonDir, err := git.CommonDir(worktreePath)
	if err != nil {
		return err
	}
	worktreeID, err := git.WorktreeID(worktreePath)
	if err != nil {
		return err
	}
	directory := filepath.Join(commonDir, setupLockDirectory)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("creating setup lock directory: %w", err)
	}

	lock, err := openSetupLock(filepath.Join(directory, worktreeID+".lock"))
	if err != nil {
		return err
	}
	defer lock.Close()

	acquired, err := fsutil.TryFileLock(lock, fn)
	if err != nil {
		return err
	}
	if !acquired {
		return fmt.Errorf("another treeman setup is already running for %q", worktreePath)
	}
	return nil
}

// openSetupLock opens the lock file without following a final symlink, and
// confirms through the descriptor that what it opened is a regular file.
func openSetupLock(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening setup lock %q: %w", path, err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("inspecting setup lock %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("setup lock path is not a regular file (symlinks are not allowed): %q", path)
	}
	return file, nil
}
