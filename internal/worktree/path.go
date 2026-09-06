// Package worktree provides high-level worktree business logic.
// This file handles path naming conventions for worktrees.
package worktree

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/shoutcape/treeman/internal/fsutil"
	"github.com/shoutcape/treeman/internal/git"
)

// slugSuffixLength is the number of hex characters kept from the branch digest.
const slugSuffixLength = 6

// BranchSlug converts a branch name to the slug used in worktree directory
// names by replacing every "/" with "-".
//
// Example: "feature/cool-thing" → "feature-cool-thing"
func BranchSlug(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}

// SlugSuffix returns the short deterministic suffix that separates two branch
// names with the same slug. The suffix comes from the full branch name, so one
// branch always gets the same suffix.
//
// Example: "feature/login" → "df7c7a"
func SlugSuffix(branch string) string {
	digest := sha256.Sum256([]byte(branch))
	return hex.EncodeToString(digest[:])[:slugSuffixLength]
}

// PathForBranch builds the plain worktree path for a given branch inside the
// resolved worktree parent directory. It does not examine the worktrees that
// exist. Use ResolvePathForBranch to select the path of a new worktree.
//
// The formula is:
//
//	<parentDir>/<branchSlug>
//
// Example:
//
//	parentDir = "/home/user/Github/my-project/.worktrees"
//	branch    = "feature/cool-thing"
//	result    = "/home/user/Github/my-project/.worktrees/feature-cool-thing"
//
// parentDir comes from ResolveDir, which turns the project's worktree_dir
// setting into an absolute directory. The default is ".worktrees" inside the
// main worktree, so an unconfigured project keeps the paths it has today.
func PathForBranch(parentDir, branch string) string {
	slug := BranchSlug(branch)
	return filepath.Join(parentDir, slug)
}

// ResolvePathForBranch selects the worktree path for branch inside parentDir.
//
// Slug conversion changes "/" to "-", so two different branch names can want
// the same directory. For example, "feature/login" and "feature-login" both
// slug to "feature-login".
//
// The plain path from PathForBranch is used unless a different branch has a
// worktree there. Only in that condition the path gets the SlugSuffix of the
// full branch name. Therefore, a branch that does not collide keeps the path
// it has today.
//
// Paths are compared canonically, so a worktree Git recorded under a
// symlinked parent still counts as occupying the path it resolves to.
//
// Example:
//
//	parentDir = "/repo/.worktrees"
//	branch    = "feature/login"
//	existing  = worktree of "feature-login" at "/repo/.worktrees/feature-login"
//	result    = "/repo/.worktrees/feature-login-df7c7a"
//
// ResolvePathForBranch returns an error when a different branch also has a
// worktree at the suffixed path.
func ResolvePathForBranch(parentDir, branch string, existing []git.WorktreeEntry) (string, error) {
	occupants, err := occupantsByPath(existing)
	if err != nil {
		return "", err
	}

	plain := PathForBranch(parentDir, branch)
	owner, taken, err := occupants.at(plain)
	if err != nil {
		return "", err
	}
	if !taken || owner == branch {
		return plain, nil
	}

	suffixed := plain + "-" + SlugSuffix(branch)
	other, taken, err := occupants.at(suffixed)
	if err != nil {
		return "", err
	}
	if taken && other != branch {
		return "", fmt.Errorf(
			"cannot place a worktree for branch %q: %q belongs to %s and %q belongs to %s",
			branch, plain, describeOccupant(owner), suffixed, describeOccupant(other))
	}
	return suffixed, nil
}

// occupants maps the canonical path of every recorded worktree to its branch.
// A detached worktree occupies its path with an empty branch name.
type occupants map[string]string

func occupantsByPath(existing []git.WorktreeEntry) (occupants, error) {
	byPath := make(occupants, len(existing))
	for _, entry := range existing {
		canonical, err := fsutil.CanonicalPath(entry.Path)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve the path of the worktree at %q: %w", entry.Path, err)
		}
		byPath[canonical] = entry.Branch
	}
	return byPath, nil
}

// at reports the branch of the worktree at path, if a worktree is there.
func (o occupants) at(path string) (string, bool, error) {
	canonical, err := fsutil.CanonicalPath(path)
	if err != nil {
		return "", false, fmt.Errorf("cannot resolve worktree destination %q: %w", path, err)
	}
	branch, taken := o[canonical]
	return branch, taken, nil
}

func describeOccupant(branch string) string {
	if branch == "" {
		return "a detached worktree"
	}
	return fmt.Sprintf("branch %q", branch)
}
