package runtime

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/shoutcape/TreeMan/internal/git"
)

// WorktreeContext holds the resolved context for the current worktree.
type WorktreeContext struct {
	WorktreePath string // absolute path to current worktree
	MainRoot     string // absolute path to main worktree
	Repo         string // repo basename
	Branch       string // current branch name
	BranchSlug   string // branch slug (slashes → dashes)
}

// ResolveContext determines which worktree we're in and extracts the
// repo name, branch, and slug. Used by runtime commands to know which
// runtime state to load/save.
func ResolveContext() (*WorktreeContext, error) {
	if !git.IsInsideRepo() {
		return nil, fmt.Errorf("not inside a git repository")
	}

	mainRoot, err := git.MainRoot()
	if err != nil {
		return nil, fmt.Errorf("finding main worktree: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getting current directory: %w", err)
	}

	// Find which worktree we're in
	worktrees, err := git.WorktreeList()
	if err != nil {
		return nil, err
	}

	var currentWT *git.Worktree
	for i := range worktrees {
		if cwd == worktrees[i].Path || isSubdir(cwd, worktrees[i].Path) {
			currentWT = &worktrees[i]
			break
		}
	}

	if currentWT == nil {
		return nil, fmt.Errorf("current directory is not inside a known worktree")
	}

	repo := filepath.Base(mainRoot)

	return &WorktreeContext{
		WorktreePath: currentWT.Path,
		MainRoot:     mainRoot,
		Repo:         repo,
		Branch:       currentWT.Branch,
		BranchSlug:   git.BranchSlug(currentWT.Branch),
	}, nil
}

// FindConfig looks for .treeman.yml in the worktree root.
func FindConfig(worktreePath string) (string, error) {
	cfgPath := filepath.Join(worktreePath, ".treeman.yml")
	if _, err := os.Stat(cfgPath); err != nil {
		return "", fmt.Errorf(".treeman.yml not found in %s\nRun 'treeman init' to create one", worktreePath)
	}
	return cfgPath, nil
}

// isSubdir reports whether child is a subdirectory of parent.
func isSubdir(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != ".." && rel != "." && len(rel) > 0 && rel[0] != '.'
}
