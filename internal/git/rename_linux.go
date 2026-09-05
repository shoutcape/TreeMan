package git

import (
	"errors"

	"golang.org/x/sys/unix"
)

// renameNoReplace moves oldPath to newPath, failing rather than replacing an
// object that already occupies newPath. Restoring a staged worktree must never
// overwrite whatever has come to sit at its original path.
//
// RENAME_NOREPLACE makes the check and the move one operation, so nothing can
// appear at newPath between them. Not every filesystem implements the flag --
// NFS, 9p, and several FUSE filesystems reject it, and kernels before 3.15
// lack the call entirely -- and there the rename is refused rather than
// performed. Failing closed on that would be the worse answer by far: this is
// the path that puts a captured worktree back, so refusing it would strand a
// user's directory in the cleanup queue over an ordinary refusal, on
// filesystems where nothing is wrong. Falling back to the portable check
// gives up only what every non-Linux build already gives up.
func renameNoReplace(oldPath, newPath string) error {
	err := unix.Renameat2(unix.AT_FDCWD, oldPath, unix.AT_FDCWD, newPath, unix.RENAME_NOREPLACE)
	if err == nil {
		return nil
	}
	if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EOPNOTSUPP) {
		return renameCheckedNoReplace(oldPath, newPath)
	}
	return err
}
