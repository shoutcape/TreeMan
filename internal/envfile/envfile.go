// Package envfile handles copying .env* files between worktrees.
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
func Copy(src, dest string) (CopyResult, error) {
	files, err := Files(src)
	if err != nil {
		return CopyResult{}, err
	}

	var result CopyResult
	for _, name := range files {
		srcPath := filepath.Join(src, name)
		destPath := filepath.Join(dest, name)

		if err := copyFile(srcPath, destPath); err != nil {
			return result, fmt.Errorf("envfile: copying %s: %w", name, err)
		}
		result.Copied = append(result.Copied, name)
	}
	return result, nil
}

// Files returns the names of all .env* files directly in dir.
func Files(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("envfile: reading source directory: %w", err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), ".env") {
			files = append(files, entry.Name())
		}
	}
	return files, nil
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
