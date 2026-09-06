// Package envfile handles copying .env* files between worktrees.
package envfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/shoutcape/treeman/internal/fsutil"
)

// Copying refuses anything that is not a regular file. A symlink would send
// the write somewhere the caller never named, and a copy cannot mean anything
// for a directory, a socket, or a device.
var (
	ErrSourceNotRegular      = errors.New("source is not a regular file")
	ErrDestinationNotRegular = errors.New("destination is not a regular file")
)

// CopyOptions selects which files to write and whether to replace existing files.
type CopyOptions struct {
	// Refresh replaces an existing regular destination file with the source.
	// Without it the destination keeps its contents, because a worktree .env
	// holds edits the copy source cannot supply again.
	Refresh bool
	// Skip names files that are never written, even if the destination is
	// absent. A blocked file does not hold back copying the others.
	Skip []string
}

// CopyFailure names one file that could not be copied, and why.
type CopyFailure struct {
	Name string
	Err  error
}

func (failure CopyFailure) Error() string {
	return fmt.Sprintf("%s: %v", failure.Name, failure.Err)
}

func (failure CopyFailure) Unwrap() error { return failure.Err }

// CopyResult holds the outcome of a copy, one bucket per outcome, named by
// basename. A single run can populate all four.
type CopyResult struct {
	Copied    []string
	Preserved []string
	Skipped   []string
	Failed    []CopyFailure
}

// Copy finds all .env* files in src and copies them to dest, replacing
// destination files that already exist. Worktree creation uses this: the
// destination is new, so there is nothing there to preserve.
func Copy(src, dest string) (CopyResult, error) {
	return CopyWith(src, dest, CopyOptions{Refresh: true})
}

// CopyWith copies every .env* file in src to dest under opts.
//
// A returned error means no file was examined, because the source directory
// could not be read. Anything that goes wrong with an individual file becomes
// a CopyFailure in the result instead, so one unreadable file cannot hide the
// files that copied.
func CopyWith(src, dest string, opts CopyOptions) (CopyResult, error) {
	files, err := Files(src)
	if err != nil {
		return CopyResult{}, err
	}

	var result CopyResult
	for _, name := range files {
		if slices.Contains(opts.Skip, name) {
			result.Skipped = append(result.Skipped, name)
			continue
		}
		outcome, err := copyFile(filepath.Join(src, name), filepath.Join(dest, name), opts.Refresh)
		switch {
		case err != nil:
			result.Failed = append(result.Failed, CopyFailure{Name: name, Err: err})
		case outcome == outcomePreserved:
			result.Preserved = append(result.Preserved, name)
		default:
			result.Copied = append(result.Copied, name)
		}
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

type copyOutcome int

const (
	outcomeCopied copyOutcome = iota
	outcomePreserved
)

// copyFile applies the policy to one file. It never removes a destination and
// never writes to one that is not a regular file.
func copyFile(src, dest string, refresh bool) (copyOutcome, error) {
	// Lstat, not Stat: a symlink named .env must be rejected, not followed.
	sourceInfo, err := os.Lstat(src)
	if err != nil {
		return 0, err
	}
	if !sourceInfo.Mode().IsRegular() {
		return 0, ErrSourceNotRegular
	}

	destinationInfo, err := os.Lstat(dest)
	switch {
	case errors.Is(err, os.ErrNotExist):
		data, err := os.ReadFile(src)
		if err != nil {
			return 0, err
		}
		return createFile(dest, data, sourceInfo.Mode().Perm())
	case err != nil:
		return 0, err
	case !destinationInfo.Mode().IsRegular():
		return 0, ErrDestinationNotRegular
	case !refresh:
		return outcomePreserved, nil
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return 0, err
	}
	// The destination's mode, not the source's: a worktree .env tightened to
	// 0600 stays that way after adopting the main worktree's contents.
	if err := fsutil.AtomicWriteFile(dest, data, destinationInfo.Mode().Perm()); err != nil {
		return 0, err
	}
	return outcomeCopied, nil
}

// createFile writes a destination that did not exist. Exclusive creation is
// what makes preservation a guarantee rather than a check: a file that arrives
// between the stat above and this open is reported, not overwritten.
func createFile(dest string, data []byte, mode os.FileMode) (copyOutcome, error) {
	file, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if errors.Is(err, os.ErrExist) {
		return outcomePreserved, nil
	}
	if err != nil {
		return 0, err
	}

	if err := writeNewFile(file, data, mode); err != nil {
		file.Close()
		// A half-written .env is worse than a missing one: remove it so the
		// next run copies the file instead of preserving the fragment.
		os.Remove(dest)
		return 0, err
	}
	if err := file.Close(); err != nil {
		os.Remove(dest)
		return 0, err
	}
	return outcomeCopied, nil
}

func writeNewFile(file *os.File, data []byte, mode os.FileMode) error {
	// The open mode is masked by the umask; chmod states it exactly, so a
	// copied file carries the source's permissions on any machine.
	if err := file.Chmod(mode); err != nil {
		return err
	}
	_, err := file.Write(data)
	return err
}
