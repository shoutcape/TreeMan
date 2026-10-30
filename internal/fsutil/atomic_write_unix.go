//go:build linux || darwin

package fsutil

import (
	"os"
	"path/filepath"
)

// AtomicWriteFile writes data to a same-directory temporary file, then replaces
// path only after the contents and file metadata have been synced.
// A directory-sync error is returned after replacement; it does not restore
// the previous file.
func AtomicWriteFile(path string, data []byte, mode os.FileMode) error {
	return atomicWriteFile(path, data, mode, SyncDirectory)
}

// AtomicReplaceFile is AtomicWriteFile with a check that runs after the new
// contents are durable and immediately before the rename. Returning an error
// from beforeRename leaves path untouched. Doing the slow work first keeps the
// window between the caller's last check and the replacement as small as
// possible; it cannot be closed against writers that take no lock.
func AtomicReplaceFile(path string, data []byte, mode os.FileMode, beforeRename func() error) error {
	return atomicReplaceFile(path, data, mode, beforeRename, SyncDirectory)
}

func atomicWriteFile(path string, data []byte, mode os.FileMode, syncDirectory func(string) error) error {
	return atomicReplaceFile(path, data, mode, nil, syncDirectory)
}

func atomicReplaceFile(path string, data []byte, mode os.FileMode, beforeRename func() error, syncDirectory func(string) error) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if beforeRename != nil {
		if err := beforeRename(); err != nil {
			return err
		}
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

// SyncDirectory flushes directory metadata to durable storage.
func SyncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
