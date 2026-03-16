// Package envfile handles copying .env* files between worktrees.
// It mirrors _wt_copy_env_files in wt.sh:592.
package envfile

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CopyResult holds the outcome of a Copy call.
type CopyResult struct {
	// Copied is the list of filenames (basename only) that were copied.
	Copied []string
}

// Copy finds all .env* files in src and copies them to dest.
// It silently skips if no .env* files exist.
// Returns the filenames that were copied and any error encountered.
//
// Mirrors _wt_copy_env_files in wt.sh:592.
func Copy(src, dest string) (CopyResult, error) {
	entries, err := os.ReadDir(src)
	if err != nil {
		return CopyResult{}, fmt.Errorf("envfile: reading source directory: %w", err)
	}

	var result CopyResult
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, ".env") {
			continue
		}

		srcPath := filepath.Join(src, name)
		destPath := filepath.Join(dest, name)

		if err := copyFile(srcPath, destPath); err != nil {
			return result, fmt.Errorf("envfile: copying %s: %w", name, err)
		}
		result.Copied = append(result.Copied, name)
	}
	return result, nil
}

// copyFile copies a single file from src to dst, preserving permissions.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
