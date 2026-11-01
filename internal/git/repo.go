package git

import (
	"errors"
	"fmt"
	"path/filepath"
)

// ErrNotRepository reports that Open was given a directory outside any Git
// repository.
var ErrNotRepository = errors.New("not inside a git repository")

// Repo is a Git repository opened from a directory. Its methods run Git from
// that directory, so a caller names the repository once instead of relying on
// the process's current directory.
//
// The zero Repo names no repository; its methods fail rather than fall back to
// the current directory.
type Repo struct {
	dir       string
	mainRoot  string
	commonDir string
}

// Open opens the repository that contains dir. It fails with ErrNotRepository
// when dir is not inside one, and resolves the main worktree root and the
// common directory before returning, so a Repo that opened is usable.
func Open(dir string) (Repo, error) {
	if dir == "" {
		return Repo{}, fmt.Errorf("could not open repository: no directory given")
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return Repo{}, fmt.Errorf("could not resolve %q: %w", dir, err)
	}
	if _, err := runInDir(absolute, "rev-parse", "--git-dir"); err != nil {
		return Repo{}, fmt.Errorf("%w: %s", ErrNotRepository, absolute)
	}
	commonDir, err := CommonDir(absolute)
	if err != nil {
		return Repo{}, err
	}
	mainRoot, err := mainWorktreeRoot(absolute)
	if err != nil {
		return Repo{}, err
	}
	return Repo{dir: absolute, mainRoot: mainRoot, commonDir: commonDir}, nil
}

// Dir returns the absolute directory the repository was opened from.
func (r Repo) Dir() string { return r.dir }

// MainRoot returns the main worktree's root. For a bare repository it is the
// repository directory itself.
func (r Repo) MainRoot() string { return r.mainRoot }

// CommonDir returns the absolute Git common directory shared by all worktrees.
func (r Repo) CommonDir() string { return r.commonDir }
