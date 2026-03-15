package cli

import (
	"testing"

	"github.com/shoutcape/TreeMan/internal/forge"
)

func TestResolveReviewBranchName(t *testing.T) {
	tests := []struct {
		name    string
		meta    *forge.PRMetadata
		exists  map[string]bool
		want    string
		wantErr bool
	}{
		{
			name:   "uses head ref when available",
			meta:   &forge.PRMetadata{HeadRef: "feature/foo", Owner: "alice"},
			exists: map[string]bool{},
			want:   "feature/foo",
		},
		{
			name:   "falls back to owner namespaced branch",
			meta:   &forge.PRMetadata{HeadRef: "main", Owner: "forkuser"},
			exists: map[string]bool{"main": true},
			want:   "forkuser/main",
		},
		{
			name:    "errors when all candidates exist",
			meta:    &forge.PRMetadata{HeadRef: "main", Owner: "forkuser"},
			exists:  map[string]bool{"main": true, "forkuser/main": true},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveReviewBranchName(tt.meta, func(branch string) bool {
				return tt.exists[branch]
			}, func(string) string {
				return ""
			})

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
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
