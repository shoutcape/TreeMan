//go:build linux || darwin

package fsutil

import (
	"errors"
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

// TryFileLock takes an exclusive advisory lock on f without waiting, and
// releases it however fn ends. It reports false, with no error, when another
// process holds the lock and fn was not run.
//
// Callers that hold a lock across an installer or a hook use this rather than
// WithFileLock: a second run that waited would look like a hang, with nothing
// on screen to say what it was waiting for.
func TryFileLock(f *os.File, fn func() error) (bool, error) {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return false, nil
		}
		return false, fmt.Errorf("lock %q: %w", f.Name(), err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return true, fn()
}
