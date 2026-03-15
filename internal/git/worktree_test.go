package git

import "testing"

func TestBranchSlug(t *testing.T) {
	tests := []struct {
		branch string
		want   string
	}{
		{"feature/foo", "feature-foo"},
		{"fix/bug-123", "fix-bug-123"},
		{"hotfix", "hotfix"},
		{"feature/nested/deep/branch", "feature-nested-deep-branch"},
		{"main", "main"},
	}

	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			got := BranchSlug(tt.branch)
			if got != tt.want {
				t.Errorf("BranchSlug(%q) = %q, want %q", tt.branch, got, tt.want)
			}
		})
	}
}

func TestWorktreePathForBranch(t *testing.T) {
	tests := []struct {
		mainRoot string
		branch   string
		want     string
	}{
		{"/home/user/Github/TreeMan", "feature/cool-thing", "/home/user/Github/TreeMan.feature-cool-thing"},
		{"/home/user/Github/TreeMan", "fix/bug-123", "/home/user/Github/TreeMan.fix-bug-123"},
		{"/home/user/Github/TreeMan", "hotfix", "/home/user/Github/TreeMan.hotfix"},
	}

	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			got := WorktreePathForBranch(tt.mainRoot, tt.branch)
			if got != tt.want {
				t.Errorf("WorktreePathForBranch(%q, %q) = %q, want %q", tt.mainRoot, tt.branch, got, tt.want)
			}
		})
	}
}

func TestParseWorktreePorcelain(t *testing.T) {
	input := `worktree /home/user/project
HEAD abc123def456
branch refs/heads/main

worktree /home/user/project.feature-foo
HEAD def456abc123
branch refs/heads/feature/foo

`
	worktrees := parseWorktreePorcelain(input)

	if len(worktrees) != 2 {
		t.Fatalf("expected 2 worktrees, got %d", len(worktrees))
	}

	if worktrees[0].Path != "/home/user/project" {
		t.Errorf("worktrees[0].Path = %q, want %q", worktrees[0].Path, "/home/user/project")
	}
	if worktrees[0].Branch != "main" {
		t.Errorf("worktrees[0].Branch = %q, want %q", worktrees[0].Branch, "main")
	}

	if worktrees[1].Path != "/home/user/project.feature-foo" {
		t.Errorf("worktrees[1].Path = %q, want %q", worktrees[1].Path, "/home/user/project.feature-foo")
	}
	if worktrees[1].Branch != "feature/foo" {
		t.Errorf("worktrees[1].Branch = %q, want %q", worktrees[1].Branch, "feature/foo")
	}
}

func TestValidateBranchName(t *testing.T) {
	tests := []struct {
		name    string
		branch  string
		wantErr bool
	}{
		{"valid simple", "feature-foo", false},
		{"valid with slash", "feature/foo", false},
		{"valid with dots", "fix.bug.123", false},
		{"invalid space", "feature foo", true},
		{"invalid tilde", "feature~foo", true},
		{"invalid caret", "feature^foo", true},
		{"invalid colon", "feature:foo", true},
		{"invalid question", "feature?foo", true},
		{"invalid star", "feature*foo", true},
		{"invalid bracket", "feature[foo", true},
		{"invalid backslash", "feature\\foo", true},
		{"invalid tab", "feature\tfoo", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBranchName(tt.branch)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
