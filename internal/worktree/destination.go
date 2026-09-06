package worktree

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/shoutcape/treeman/internal/fsutil"
)

// Protected describes the repository paths a worktree must never be created
// on top of. The caller supplies them because resolving them costs a Git
// process, and one lookup serves a whole command.
type Protected struct {
	// MainRoot is the main worktree root.
	MainRoot string
	// CommonDir is the Git common directory shared by every worktree. The
	// linked-worktree administration directories live inside it.
	CommonDir string
}

// ValidateDestination reports whether a new worktree may be created at dest.
//
// It refuses a destination that already exists in any form, that is or is
// inside a path Git owns, and that cannot be reached because an ancestor is
// missing in a way that creating directories will not fix. Comparisons run on
// canonical paths, so a symlinked parent cannot be used to reach a protected
// path under another name.
//
// A destination that passes may still be taken by the time it is used, so
// callers revalidate under the worktree mutation lock immediately before
// creating the worktree.
func ValidateDestination(protected Protected, dest string) error {
	cleaned := filepath.Clean(dest)

	if info, err := os.Lstat(cleaned); err == nil {
		return fmt.Errorf("cannot place a worktree at %q: %s already exists", cleaned, describeExisting(info))
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("cannot inspect worktree destination %q: %w", cleaned, err)
	}

	if err := validateAncestors(cleaned); err != nil {
		return err
	}

	canonical, err := fsutil.CanonicalPath(cleaned)
	if err != nil {
		return fmt.Errorf("cannot resolve worktree destination %q: %w", cleaned, err)
	}
	return validateNotProtected(protected, cleaned, canonical)
}

// validateAncestors walks up to the first existing ancestor and requires that
// it be a usable directory. MkdirAll cannot create a directory below a file, a
// broken symlink, or a symlink loop, and reporting that here names the path
// that has to change.
func validateAncestors(dest string) error {
	current := filepath.Dir(dest)
	for {
		info, err := os.Lstat(current)
		switch {
		case err == nil:
			return validateExistingAncestor(current, info)
		case !errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("cannot inspect %q on the way to worktree destination %q: %w", current, dest, err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return fmt.Errorf("cannot place a worktree at %q: no ancestor of it exists", dest)
		}
		current = parent
	}
}

func validateExistingAncestor(path string, info fs.FileInfo) error {
	if info.Mode()&fs.ModeSymlink != 0 {
		// A symlink that resolves to a directory is a legitimate parent; one
		// that dangles or loops is not, and Stat says which.
		target, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("cannot follow %q on the way to the worktree destination: %w", path, err)
		}
		if !target.IsDir() {
			return fmt.Errorf("cannot place a worktree below %q: it links to a file, not a directory", path)
		}
		return nil
	}
	if !info.IsDir() {
		return fmt.Errorf("cannot place a worktree below %q: it is a file, not a directory", path)
	}
	return nil
}

// validateNotProtected refuses destinations that would overwrite or hide the
// repository itself. Git's own directories are the dangerous half: a worktree
// created inside the common directory would put its checkout among the
// administration files of every worktree in the repository.
func validateNotProtected(protected Protected, dest, canonical string) error {
	for _, candidate := range protectedPaths(protected) {
		canonicalCandidate, err := fsutil.CanonicalPath(candidate.path)
		if err != nil {
			return fmt.Errorf("cannot resolve %s %q: %w", candidate.name, candidate.path, err)
		}
		if canonical == canonicalCandidate {
			return fmt.Errorf("cannot place a worktree at %q: it is %s", dest, candidate.name)
		}
		if candidate.protectDescendants && fsutil.Contains(canonicalCandidate, canonical) {
			return fmt.Errorf("cannot place a worktree at %q: it is inside %s %q", dest, candidate.name, candidate.path)
		}
	}
	return nil
}

type protectedPath struct {
	name               string
	path               string
	protectDescendants bool
}

func protectedPaths(protected Protected) []protectedPath {
	var paths []protectedPath
	if protected.MainRoot != "" {
		paths = append(paths,
			protectedPath{name: "the main worktree root", path: protected.MainRoot},
			protectedPath{name: "the repository's Git directory", path: filepath.Join(protected.MainRoot, ".git"), protectDescendants: true},
		)
	}
	if protected.CommonDir != "" {
		paths = append(paths, protectedPath{name: "the Git common directory", path: protected.CommonDir, protectDescendants: true})
	}
	return paths
}

// EnsureParentDir creates the directories leading to dest. It is called only
// after ValidateDestination, so the ancestors it creates are known to be
// creatable.
func EnsureParentDir(dest string) error {
	parent := filepath.Dir(filepath.Clean(dest))
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("could not create worktree parent directory %q: %w", parent, err)
	}
	return nil
}

func describeExisting(info fs.FileInfo) string {
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return "a symlink"
	case info.IsDir():
		return "a directory"
	default:
		return "a file"
	}
}
