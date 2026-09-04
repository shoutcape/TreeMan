//go:build !linux

package git

import (
	"fmt"
	"os"
)

// renameNoReplace moves oldPath to newPath, failing rather than replacing an
// object that already occupies newPath.
//
// RENAME_NOREPLACE has no portable equivalent, so here the check and the move
// are two steps rather than one. What that costs is narrow and worth naming:
// TreeMan's own concurrency is already covered, because every caller holds the
// repository mutation lock, so no second TreeMan process can create newPath in
// the window. What the Linux implementation additionally rules out, and this
// one cannot, is a foreign process creating newPath between the check and the
// rename -- in which case this replaces it.
func renameNoReplace(oldPath, newPath string) error {
	if _, err := os.Lstat(newPath); err == nil {
		return fmt.Errorf("an object already exists at %q", newPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("could not determine whether %q is free: %w", newPath, err)
	}
	return os.Rename(oldPath, newPath)
}
