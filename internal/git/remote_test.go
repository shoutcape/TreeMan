package git

import "testing"

func TestParseRemoteHost(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"github ssh shorthand", "git@github.com:owner/repo.git", "github.com"},
		{"github https", "https://github.com/owner/repo.git", "github.com"},
		{"github ssh://", "ssh://git@github.com/owner/repo.git", "github.com"},
		{"gitlab.com ssh shorthand", "git@gitlab.com:group/project.git", "gitlab.com"},
		{"gitlab.com https", "https://gitlab.com/group/project.git", "gitlab.com"},
		{"self-hosted gitlab ssh", "git@gitlab.company.com:acme/frontend/webapp.git", "gitlab.company.com"},
		{"self-hosted gitlab https", "https://gitlab.company.com/acme/frontend/webapp.git", "gitlab.company.com"},
		{"ssh:// with port", "ssh://git@gitlab.company.com:2222/group/project.git", "gitlab.company.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRemoteHost(tt.url)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ParseRemoteHost(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestParseRemotePath(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"github ssh .git", "git@github.com:owner/repo.git", "owner/repo"},
		{"github ssh no .git", "git@github.com:owner/repo", "owner/repo"},
		{"github https .git", "https://github.com/owner/repo.git", "owner/repo"},
		{"github https no .git", "https://github.com/owner/repo", "owner/repo"},
		{"github ssh://", "ssh://git@github.com/owner/repo.git", "owner/repo"},
		{"gitlab nested groups ssh", "git@gitlab.company.com:acme/frontend/webapp.git", "acme/frontend/webapp"},
		{"gitlab nested groups https", "https://gitlab.company.com/acme/frontend/webapp.git", "acme/frontend/webapp"},
		{"gitlab nested groups ssh://", "ssh://git@gitlab.company.com/acme/frontend/webapp.git", "acme/frontend/webapp"},
		{"ssh:// with port", "ssh://git@gitlab.company.com:2222/group/project.git", "group/project"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRemotePath(tt.url)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ParseRemotePath(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestDetectForgeFromURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    ForgeType
		wantErr bool
	}{
		{"github from ssh", "git@github.com:owner/repo.git", ForgeGitHub, false},
		{"github from https", "https://github.com/owner/repo.git", ForgeGitHub, false},
		{"gitlab.com from https", "https://gitlab.com/group/project.git", ForgeGitLab, false},
		{"self-hosted gitlab from ssh", "git@gitlab.company.com:g/p.git", ForgeGitLab, false},
		{"unsupported host", "git@bitbucket.org:o/r.git", ForgeUnknown, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DetectForgeFromURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("DetectForgeFromURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}
