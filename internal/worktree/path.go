// Package worktree provides high-level worktree business logic.
// This file handles path naming conventions for worktrees.
package worktree

import (
	"path/filepath"
	"strings"
)

// BranchSlug converts a branch name to the slug used in worktree directory
// names by replacing every "/" with "-".
//
// Mirrors _wt_branch_slug in wt.sh:60.
//
// Example: "feature/cool-thing" → "feature-cool-thing"
func BranchSlug(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}

// PathForBranch builds the worktree path for a given branch inside the repo.
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
