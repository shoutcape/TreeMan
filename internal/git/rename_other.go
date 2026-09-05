//go:build !linux

package git

// renameNoReplace moves oldPath to newPath, failing rather than replacing an
// object that already occupies newPath. Restoring a staged worktree must never
// overwrite whatever has come to sit at its original path.
//
// RENAME_NOREPLACE has no portable equivalent, so the check and the move are
// two steps here, with the exposure renameCheckedNoReplace describes.
func renameNoReplace(oldPath, newPath string) error {
	return renameCheckedNoReplace(oldPath, newPath)
}
