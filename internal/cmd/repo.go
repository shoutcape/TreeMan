package cmd

import (
	"errors"
	"os"

	"github.com/shoutcape/treeman/internal/git"
)

// errNotInRepository is what a command reports when it runs outside a Git
// repository.
var errNotInRepository = errors.New("not inside a git repository")

// openCurrentRepo opens the repository the command was run in. Commands act on
// the directory they start in; this is the one place that reads it, and the
// Repo it returns is passed down from there.
func openCurrentRepo() (git.Repo, error) {
	// A current directory that cannot be read, such as one deleted under
	// the shell, is not inside any repository Git could open either.
	dir, err := os.Getwd()
	if err != nil {
		return git.Repo{}, errNotInRepository
	}
	repo, err := git.Open(dir)
	if errors.Is(err, git.ErrNotRepository) {
		return git.Repo{}, errNotInRepository
	}
	return repo, err
}
