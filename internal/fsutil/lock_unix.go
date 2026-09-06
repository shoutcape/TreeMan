//go:build linux || darwin

package fsutil

import (
	"fmt"
	"os"
	"syscall"
)

// WithFileLock holds an exclusive advisory lock on f for the duration of fn,
// and releases it however fn ends.
//
// The caller owns opening f, so a caller with its own open-time guarantees --
// that the path was not a symlink, that it is still the file it validated --
// keeps them instead of having them re-derived from a path here.
func WithFileLock(f *os.File, fn func() error) error {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock %q: %w", f.Name(), err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}
