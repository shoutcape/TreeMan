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

func atomicWriteFile(path string, data []byte, mode os.FileMode, syncDirectory func(string) error) error {
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
