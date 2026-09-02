// Package worktree provides high-level worktree business logic.
// This file handles path naming conventions for worktrees.
package worktree

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

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
// repo. It does not examine the worktrees that exist. Use
// ResolvePathForBranch to select the path of a new worktree.
//
// The formula is:
//
//	<mainRoot>/.worktrees/<branchSlug>
//
// Example:
//
//	mainRoot = "/home/user/Github/my-project"
//	branch   = "feature/cool-thing"
//	result   = "/home/user/Github/my-project/.worktrees/feature-cool-thing"
func PathForBranch(mainRoot, branch string) string {
	slug := BranchSlug(branch)
	return filepath.Join(mainRoot, ".worktrees", slug)
}

// ResolvePathForBranch selects the worktree path for branch inside mainRoot.
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
// Example:
//
//	mainRoot = "/repo"
//	branch   = "feature/login"
//	existing = worktree of "feature-login" at "/repo/.worktrees/feature-login"
//	result   = "/repo/.worktrees/feature-login-df7c7a"
//
// ResolvePathForBranch returns an error when a different branch also has a
// worktree at the suffixed path.
func ResolvePathForBranch(mainRoot, branch string, existing []git.WorktreeEntry) (string, error) {
	plain := PathForBranch(mainRoot, branch)
	owner, taken := occupant(existing, plain)
	if !taken || owner == branch {
		return plain, nil
	}

	suffixed := plain + "-" + SlugSuffix(branch)
	if other, taken := occupant(existing, suffixed); taken && other != branch {
		return "", fmt.Errorf(
			"cannot place a worktree for branch %q: %q belongs to %s and %q belongs to %s",
			branch, plain, describeOccupant(owner), suffixed, describeOccupant(other))
	}
	return suffixed, nil
}

// occupant reports the branch of the worktree at path, if a worktree is there.
// A detached worktree occupies the path with an empty branch name.
func occupant(existing []git.WorktreeEntry, path string) (string, bool) {
	target := filepath.Clean(path)
	for _, entry := range existing {
		if filepath.Clean(entry.Path) == target {
			return entry.Branch, true
		}
	}
	return "", false
}

func describeOccupant(branch string) string {
	if branch == "" {
		return "a detached worktree"
	}
	return fmt.Sprintf("branch %q", branch)
}
