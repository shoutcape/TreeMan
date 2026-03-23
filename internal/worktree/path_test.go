package worktree_test

import (
	"testing"

	"github.com/shoutcape/treeman/internal/worktree"
	"github.com/stretchr/testify/assert"
)

func TestBranchSlug(t *testing.T) {
	tests := []struct {
		branch string
		want   string
	}{
		{"feature/cool-thing", "feature-cool-thing"},
		{"fix/bug-123", "fix-bug-123"},
		{"hotfix", "hotfix"},
		{"feat/nested/deep", "feat-nested-deep"},
		{"no-slashes", "no-slashes"},
	}

	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			assert.Equal(t, tt.want, worktree.BranchSlug(tt.branch))
		})
	}
}

func TestPathForBranch(t *testing.T) {
	tests := []struct {
		name     string
		mainRoot string
		branch   string
		want     string
	}{
		{
			name:     "feature branch with slash",
			mainRoot: "/home/user/Github/my-project",
			branch:   "feature/cool-thing",
			want:     "/home/user/Github/my-project/.worktrees/feature-cool-thing",
		},
		{
			name:     "fix branch with slash",
			mainRoot: "/home/user/Github/my-project",
			branch:   "fix/bug-123",
			want:     "/home/user/Github/my-project/.worktrees/fix-bug-123",
		},
		{
			name:     "simple branch no slash",
			mainRoot: "/home/user/Github/my-project",
			branch:   "hotfix",
			want:     "/home/user/Github/my-project/.worktrees/hotfix",
		},
		{
			name:     "review branch matches smoke test naming",
			mainRoot: "/tmp/project",
			branch:   "feature/review-alpha",
			want:     "/tmp/project/.worktrees/feature-review-alpha",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, worktree.PathForBranch(tt.mainRoot, tt.branch))
		})
	}
}
