package fsutil

import (
	"errors"
	"io/fs"
	"path/filepath"
)

// CanonicalPath makes path absolute, resolves symlinks through its deepest
// existing ancestor, and reattaches any missing tail.
func CanonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return canonicalExistingPrefix(filepath.Clean(absolute))
}

func canonicalExistingPrefix(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}

	parent := filepath.Dir(path)
	if parent == path {
		return path, nil
	}
	resolvedParent, err := canonicalExistingPrefix(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolvedParent, filepath.Base(path)), nil
}
