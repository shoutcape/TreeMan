package cmd

import (
	"fmt"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/worktree"
)

// creationPaths is everything a creation flow needs to know about where the
// new worktree goes, resolved once from one configuration snapshot.
//
// Path selection and post-create setup both depend on the project's
// configuration -- the worktree directory and the .gitignore entry that
// describes it -- so they read it from the same value rather than loading the
// file twice and possibly disagreeing about it.
type creationPaths struct {
	// mainRoot is the main worktree root every relative path resolves from.
	mainRoot string
	// parentDir is the resolved absolute directory new worktrees are made in.
	parentDir string
	// path is the destination selected before the operation started. The
	// worktree is created at whatever plan selects under the lock, which is
	// this path unless another process took it in the meantime.
	path string
	// protected names the repository paths a worktree may not be placed on.
	protected worktree.Protected
	// config is the project configuration the whole flow works from.
	config config.Config
}

// prepareCreationPaths resolves where a worktree for branch would go and
// refuses the operation now if it cannot go there.
//
// Creation flows call this before fetching or touching any ref. A destination
// that is unusable, or a configuration that cannot be read, must not cost the
// user a network round trip and must never leave a branch behind.
func prepareCreationPaths(mainRoot, branch string) (creationPaths, error) {
	return prepareCreationPathsIn(mainRoot, branch, "")
}

// prepareCreationPathsIn uses parentDir when the caller owns the filesystem
// boundary, as benchmark sandboxes do. Project configuration still supplies
// every non-location setup option.
func prepareCreationPathsIn(mainRoot, branch, parentDir string) (creationPaths, error) {
	loaded := config.Load(mainRoot)
	if loaded.Warning != "" {
		// The file may place worktrees somewhere other than the default.
		// Creating one in the default location instead would quietly ignore
		// what the project asked for, so an unusable config stops the flow.
		return creationPaths{}, fmt.Errorf("cannot create a worktree: %s", loaded.Warning)
	}

	configuredParent, err := worktree.ResolveDir(mainRoot, loaded.Config.WorktreeDirSetting())
	if err != nil {
		return creationPaths{}, err
	}
	if parentDir == "" {
		parentDir = configuredParent
	}

	commonDir, err := git.CommonDir("")
	if err != nil {
		return creationPaths{}, err
	}

	paths := creationPaths{
		mainRoot:  mainRoot,
		parentDir: parentDir,
		protected: worktree.Protected{MainRoot: mainRoot, CommonDir: commonDir},
		config:    loaded.Config,
	}

	existing, err := git.WorktreeList()
	if err != nil {
		return creationPaths{}, err
	}
	paths.path, err = worktree.ResolvePathForBranch(parentDir, branch, existing)
	if err != nil {
		return creationPaths{}, err
	}
	if err := worktree.ValidateDestination(paths.protected, paths.path); err != nil {
		return creationPaths{}, err
	}
	return paths, nil
}

// plan returns the destination selection Git runs under its worktree mutation
// lock. It repeats the selection and the safety check against the worktrees
// recorded at that moment, because the destination chosen by
// prepareCreationPaths may have been taken since, and creates the parent
// directories only once the destination is known to be usable.
func (p creationPaths) plan(branch string) git.WorktreePlan {
	return func(existing []git.WorktreeEntry) (string, error) {
		path, err := worktree.ResolvePathForBranch(p.parentDir, branch, existing)
		if err != nil {
			return "", err
		}
		if err := worktree.ValidateDestination(p.protected, path); err != nil {
			return "", err
		}
		if err := worktree.EnsureParentDir(path); err != nil {
			return "", err
		}
		return path, nil
	}
}
