package worktree_test

import (
	"testing"

	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/worktree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestSlugSuffix(t *testing.T) {
	tests := []struct {
		branch string
		want   string
	}{
		{"feature/login", "df7c7a"},
		{"feature-login", "1a2efb"},
		{"feat/nested/deep", "442a4f"},
		{"hotfix", "eda660"},
	}

	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			assert.Equal(t, tt.want, worktree.SlugSuffix(tt.branch))
		})
	}
}

func TestSlugSuffixIsStable(t *testing.T) {
	assert.Equal(t, worktree.SlugSuffix("feature/login"), worktree.SlugSuffix("feature/login"))
	assert.NotEqual(t, worktree.SlugSuffix("feature/login"), worktree.SlugSuffix("feature-login"))
}

func TestResolvePathForBranch(t *testing.T) {
	const mainRoot = "/repo"

	slashLogin := git.WorktreeEntry{Path: "/repo/.worktrees/feature-login", Branch: "feature/login"}
	dashLogin := git.WorktreeEntry{Path: "/repo/.worktrees/feature-login", Branch: "feature-login"}

	tests := []struct {
		name     string
		branch   string
		existing []git.WorktreeEntry
		want     string
	}{
		{
			name:   "no worktrees keeps the plain path",
			branch: "feature/login",
			want:   "/repo/.worktrees/feature-login",
		},
		{
			name:     "unrelated worktrees keep the plain path",
			branch:   "feature/login",
			existing: []git.WorktreeEntry{{Path: "/repo", Branch: "main"}, {Path: "/repo/.worktrees/fix-bug-123", Branch: "fix/bug-123"}},
			want:     "/repo/.worktrees/feature-login",
		},
		{
			name:     "slash branch after dash branch gets a suffix",
			branch:   "feature/login",
			existing: []git.WorktreeEntry{dashLogin},
			want:     "/repo/.worktrees/feature-login-df7c7a",
		},
		{
			name:     "dash branch after slash branch gets a suffix",
			branch:   "feature-login",
			existing: []git.WorktreeEntry{slashLogin},
			want:     "/repo/.worktrees/feature-login-1a2efb",
		},
		{
			name:     "the same branch keeps its own plain path",
			branch:   "feature/login",
			existing: []git.WorktreeEntry{slashLogin},
			want:     "/repo/.worktrees/feature-login",
		},
		{
			name:     "the same branch keeps its own suffixed path",
			branch:   "feature/login",
			existing: []git.WorktreeEntry{dashLogin, {Path: "/repo/.worktrees/feature-login-df7c7a", Branch: "feature/login"}},
			want:     "/repo/.worktrees/feature-login-df7c7a",
		},
		{
			name:     "a detached worktree on the plain path forces a suffix",
			branch:   "feature/login",
			existing: []git.WorktreeEntry{{Path: "/repo/.worktrees/feature-login"}},
			want:     "/repo/.worktrees/feature-login-df7c7a",
		},
		{
			name:     "a trailing separator still matches the plain path",
			branch:   "feature/login",
			existing: []git.WorktreeEntry{{Path: "/repo/.worktrees/feature-login/", Branch: "feature-login"}},
			want:     "/repo/.worktrees/feature-login-df7c7a",
		},
		{
			name:     "a nested branch keeps its plain path",
			branch:   "feat/nested/deep",
			existing: []git.WorktreeEntry{dashLogin},
			want:     "/repo/.worktrees/feat-nested-deep",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := worktree.ResolvePathForBranch(mainRoot, tt.branch, tt.existing)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolvePathForBranchErrors(t *testing.T) {
	const mainRoot = "/repo"

	tests := []struct {
		name     string
		branch   string
		existing []git.WorktreeEntry
		contains []string
	}{
		{
			name:   "both paths belong to other branches",
			branch: "feature/login",
			existing: []git.WorktreeEntry{
				{Path: "/repo/.worktrees/feature-login", Branch: "feature-login"},
				{Path: "/repo/.worktrees/feature-login-df7c7a", Branch: "other/branch"},
			},
			contains: []string{`branch "feature/login"`, `branch "feature-login"`, `branch "other/branch"`, "/repo/.worktrees/feature-login-df7c7a"},
		},
		{
			name:   "a detached worktree holds the suffixed path",
			branch: "feature/login",
			existing: []git.WorktreeEntry{
				{Path: "/repo/.worktrees/feature-login", Branch: "feature-login"},
				{Path: "/repo/.worktrees/feature-login-df7c7a"},
			},
			contains: []string{"a detached worktree"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := worktree.ResolvePathForBranch(mainRoot, tt.branch, tt.existing)
			require.Error(t, err)
			for _, want := range tt.contains {
				assert.Contains(t, err.Error(), want)
			}
		})
	}
}
