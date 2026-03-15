package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateEnvFile(t *testing.T) {
	dir := t.TempDir()

	state := &RuntimeState{
		Repo:         "shop-app",
		Branch:       "feature/cart-redesign",
		BranchSlug:   "feature-cart-redesign",
		WorktreePath: dir,
		EnvFile:      ".env.treeman",
		Ports: map[string]int{
			"app": 3012,
			"api": 4012,
		},
	}

	if err := GenerateEnvFile(state); err != nil {
		t.Fatalf("GenerateEnvFile: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, ".env.treeman"))
	if err != nil {
		t.Fatalf("reading env file: %v", err)
	}

	text := string(content)

	// Check required variables
	if !strings.Contains(text, "TREEMAN_BRANCH=feature/cart-redesign") {
		t.Error("missing TREEMAN_BRANCH")
	}
	if !strings.Contains(text, "TREEMAN_BRANCH_SLUG=feature-cart-redesign") {
		t.Error("missing TREEMAN_BRANCH_SLUG")
	}
	if !strings.Contains(text, "TREEMAN_RUNTIME_NAME=shop-app-feature-cart-redesign") {
		t.Error("missing TREEMAN_RUNTIME_NAME")
	}
	if !strings.Contains(text, "API_PORT=4012") {
		t.Error("missing API_PORT")
	}
	if !strings.Contains(text, "APP_PORT=3012") {
		t.Error("missing APP_PORT")
	}
	if !strings.Contains(text, "PORT=3012") {
		t.Error("missing PORT (should be set for app port)")
	}
}

func TestComposeProjectName(t *testing.T) {
	tests := []struct {
		repo       string
		branchSlug string
		want       string
	}{
		{"shop-app", "feature-cart-redesign", "shop_app_feature_cart_redesign"},
		{"myapp", "main", "myapp_main"},
		{"My.App", "fix-bug", "my_app_fix_bug"},
	}

	for _, tt := range tests {
		t.Run(tt.repo+"/"+tt.branchSlug, func(t *testing.T) {
			got := ComposeProjectName(tt.repo, tt.branchSlug)
			if got != tt.want {
				t.Errorf("ComposeProjectName(%q, %q) = %q, want %q",
					tt.repo, tt.branchSlug, got, tt.want)
			}
		})
	}
}

func TestGenerateEnvFileWithCompose(t *testing.T) {
	dir := t.TempDir()

	state := &RuntimeState{
		Repo:               "shop-app",
		Branch:             "main",
		BranchSlug:         "main",
		WorktreePath:       dir,
		EnvFile:            ".env.treeman",
		Ports:              map[string]int{"app": 3000},
		ComposeProjectName: "shop_app_main",
	}

	if err := GenerateEnvFile(state); err != nil {
		t.Fatalf("GenerateEnvFile: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, ".env.treeman"))
	if err != nil {
		t.Fatalf("reading env file: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "COMPOSE_PROJECT_NAME=shop_app_main") {
		t.Error("missing COMPOSE_PROJECT_NAME")
	}
}
