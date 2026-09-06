package cmd

import (
	"fmt"
	"io"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/hooks"
	"github.com/shoutcape/treeman/internal/ui"
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
	projectPaths
	// path is the destination selected before the operation started. The
	// worktree is created at whatever plan selects under the lock, which is
	// this path unless another process took it in the meantime.
	path string
	// hooks is the consent resolved for this creation's post-create hooks.
	hooks   hookApproval
	options creationSetupOptions
}

// projectPaths is what one project configuration says about a repository,
// resolved once. Creating a worktree and repairing an existing one both read
// it, and both read it from the main worktree: a branch may have edited its
// own copy of the configuration, and setup must not follow that copy.
type projectPaths struct {
	// mainRoot is the main worktree root every relative path resolves from.
	mainRoot string
	// parentDir is the resolved absolute directory new worktrees are made in.
	parentDir string
	// configPath is the absolute configuration file used for this snapshot.
	configPath string
	// protected names the repository paths a worktree may not be placed on.
	protected worktree.Protected
	// config is the project configuration the whole flow works from.
	config config.Config
}

// loadProjectPaths reads the project configuration and everything derived from
// it. An unusable configuration file is an error rather than a warning: it may
// place worktrees somewhere other than the default, and carrying on with the
// default would quietly ignore what the project asked for.
func loadProjectPaths(mainRoot string) (projectPaths, error) {
	loaded := config.Load(mainRoot)
	if loaded.Warning != "" {
		return projectPaths{}, fmt.Errorf("cannot read project configuration: %s", loaded.Warning)
	}
	parentDir, err := worktree.ResolveDir(mainRoot, loaded.Config.WorktreeDirSetting())
	if err != nil {
		return projectPaths{}, err
	}
	commonDir, err := git.CommonDir("")
	if err != nil {
		return projectPaths{}, err
	}
	return projectPaths{
		mainRoot:   mainRoot,
		parentDir:  parentDir,
		configPath: loaded.Path,
		protected:  worktree.Protected{MainRoot: mainRoot, CommonDir: commonDir},
		config:     loaded.Config,
	}, nil
}

// prepareCreationPathsIn resolves where a worktree for branch would go and
// refuses the operation now if it cannot go there. An empty parentDir takes
// the destination from project configuration; a caller that owns the
// filesystem boundary, as benchmark sandboxes do, supplies its own. Project
// configuration still supplies every non-location setup option.
//
// Creation flows reach this through prepareApprovedCreationPaths, before
// fetching or touching any ref. A destination that is unusable, or a
// configuration that cannot be read, must not cost the user a network round
// trip and must never leave a branch behind.
func prepareCreationPathsIn(mainRoot, branch, parentDir string) (creationPaths, error) {
	project, err := loadProjectPaths(mainRoot)
	if err != nil {
		return creationPaths{}, fmt.Errorf("cannot create a worktree: %w", err)
	}
	if parentDir != "" {
		project.parentDir = parentDir
	}
	paths := creationPaths{projectPaths: project}

	existing, err := git.WorktreeList()
	if err != nil {
		return creationPaths{}, err
	}
	paths.path, err = worktree.ResolvePathForBranch(paths.parentDir, branch, existing)
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
// prepareCreationPathsIn may have been taken since, and creates the parent
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

// hookApproval holds the exact commands authorized for this creation.
type hookApproval struct {
	commands []string
}

func (a hookApproval) run(w io.Writer, render ui.Renderer, worktreePath string) setupStatus {
	if len(a.commands) == 0 {
		return skippedStatus("skipped (no post-create hooks configured)")
	}
	fmt.Fprintln(w, render.Status(ui.ToneInfo, "→", fmt.Sprintf("Running %d post-create hook(s)...", len(a.commands))))
	results := hooks.RunPostCreate(worktreePath, a.commands, w)
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintln(w, render.Status(ui.ToneWarning, "!", fmt.Sprintf("hook %q failed: %v", r.Command, r.Err)))
		} else {
			fmt.Fprintln(w, render.Status(ui.ToneSuccess, "✓", "Ran: "+r.Command))
		}
	}
	return summarizeHooks(results)
}
