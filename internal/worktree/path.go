// Package worktree provides high-level worktree business logic.
// This file handles path naming conventions for sibling worktrees.
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

// PathForBranch builds the sibling worktree path for a given branch.
//
// The formula (mirrors _wt_worktree_path_for_branch in wt.sh:66) is:
//
//	<dirname(mainRoot)>/<basename(mainRoot)>.<branchSlug>
//
// Example:
//
//	mainRoot = "/home/user/Github/my-project"
//	branch   = "feature/cool-thing"
//	result   = "/home/user/Github/my-project.feature-cool-thing"
func PathForBranch(mainRoot, branch string) string {
	parent := filepath.Dir(mainRoot)
	repoName := filepath.Base(mainRoot)
	slug := BranchSlug(branch)
	return filepath.Join(parent, repoName+"."+slug)
}
