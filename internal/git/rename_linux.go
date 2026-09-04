package git

import "golang.org/x/sys/unix"

// renameNoReplace moves oldPath to newPath, failing rather than replacing an
// object that already occupies newPath. Restoring a staged worktree must never
// overwrite whatever has come to sit at its original path.
//
// RENAME_NOREPLACE makes the check and the move one operation, so nothing can
// appear at newPath between them.
func renameNoReplace(oldPath, newPath string) error {
	return unix.Renameat2(unix.AT_FDCWD, oldPath, unix.AT_FDCWD, newPath, unix.RENAME_NOREPLACE)
}
