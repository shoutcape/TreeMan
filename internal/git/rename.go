package git

import (
	"fmt"
	"os"
)

// renameCheckedNoReplace moves oldPath to newPath, refusing rather than
// replacing an object that already occupies newPath.
//
// The check and the move are two steps rather than one, so what it costs is
// narrow and worth naming: TreeMan's own concurrency is already covered,
// because every caller holds the repository mutation lock, so no second
// TreeMan process can create newPath in the window. What an atomic
// no-replace rename additionally rules out, and this cannot, is a foreign
// process creating newPath between the check and the rename -- in which case
// this replaces it.
func renameCheckedNoReplace(oldPath, newPath string) error {
	if _, err := os.Lstat(newPath); err == nil {
		return fmt.Errorf("an object already exists at %q", newPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("could not determine whether %q is free: %w", newPath, err)
	}
	return os.Rename(oldPath, newPath)
}
