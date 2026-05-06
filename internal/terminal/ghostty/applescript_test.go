package ghostty

import (
	"testing"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/terminal"
	"github.com/stretchr/testify/assert"
)

func TestEscapeAppleScript(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain text", "hello world", "hello world"},
		{"quotes", `say "hi"`, `say \"hi\"`},
		{"backslash", `path\to`, `path\\to`},
		{"both", `"back\slash"`, `\"back\\slash\"`},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, escapeAppleScript(tt.in))
		})
	}
}

func TestBuildTitleHookCmd(t *testing.T) {
	cmd := buildTitleHookCmd("feat/cool")
	assert.Contains(t, cmd, `TREEMAN_BRANCH`)
	assert.Contains(t, cmd, `feat/cool`)
	assert.Contains(t, cmd, `_tm_precmd`)
	assert.Contains(t, cmd, `_tm_preexec`)
	assert.Contains(t, cmd, `add-zsh-hook`)
	assert.Contains(t, cmd, `clear`)
}

func TestBuildTitleHookCmd_EscapesQuotes(t *testing.T) {
	cmd := buildTitleHookCmd(`branch"with"quotes`)
	assert.Contains(t, cmd, `branch\"with\"quotes`)
}

func TestBuildOpenScript_NoLayout(t *testing.T) {
	wt := terminal.WorktreeInfo{
		Path:   "/tmp/my-project",
		Branch: "main",
		Slug:   "my-project",
	}
	script := BuildOpenScript(wt, nil)

	assert.Contains(t, script, `tell application "Ghostty"`)
	assert.Contains(t, script, `activate`)
	assert.Contains(t, script, `"/tmp/my-project"`)
	assert.Contains(t, script, `new tab`)
	assert.Contains(t, script, `new window`)
	assert.Contains(t, script, `focus term`)
	assert.NotContains(t, script, "split term")
	// Delay before main terminal input to avoid race condition
	assert.Contains(t, script, "delay 0.5")
	// Title hook injected into the main terminal
	assert.Contains(t, script, `TREEMAN_BRANCH`)
	assert.Contains(t, script, `input text`)
	assert.Contains(t, script, `to term`)
}

func TestBuildOpenScript_WithSplits(t *testing.T) {
	wt := terminal.WorktreeInfo{
		Path:   "/tmp/my-project",
		Branch: "feat/cool",
		Slug:   "feat-cool",
	}
	layout := &config.LayoutConfig{
		Splits: []config.SplitConfig{
			{Direction: "right", Command: "pnpm dev"},
			{Direction: "down", Command: ""},
		},
	}
	script := BuildOpenScript(wt, layout)

	assert.Contains(t, script, "split term direction right")
	assert.Contains(t, script, "delay 0.5")
	assert.Contains(t, script, `input text "pnpm dev\n" to pane1`)
	assert.Contains(t, script, "split term direction down")
	// Title hooks injected into main terminal and all panes
	assert.Contains(t, script, `TREEMAN_BRANCH`)
	assert.Contains(t, script, `to term`)
	assert.Contains(t, script, `to pane1`)
	assert.Contains(t, script, `to pane2`)
}

func TestBuildOpenScript_PathWithSpaces(t *testing.T) {
	wt := terminal.WorktreeInfo{
		Path:   `/tmp/my project/workspace`,
		Branch: "main",
		Slug:   "my-project",
	}
	script := BuildOpenScript(wt, nil)

	assert.Contains(t, script, `"/tmp/my project/workspace"`)
}

func TestBuildFocusScript(t *testing.T) {
	wt := terminal.WorktreeInfo{
		Path:   "/tmp/test-worktree",
		Branch: "feat",
		Slug:   "feat-branch",
	}
	script := BuildFocusScript(wt)

	assert.Contains(t, script, `tell application "Ghostty"`)
	assert.Contains(t, script, `working directory is "/tmp/test-worktree"`)
	assert.Contains(t, script, `focus matchedTerm`)
	assert.Contains(t, script, `return "found"`)
	assert.Contains(t, script, `return "not_found"`)
	// Should check if already focused to avoid unnecessary refocus.
	assert.Contains(t, script, `frontmost`)
	assert.Contains(t, script, `focused terminal`)
}

func TestBuildCloseScript(t *testing.T) {
	wt := terminal.WorktreeInfo{
		Path:   "/tmp/test-worktree",
		Branch: "feat",
		Slug:   "feat-branch",
	}
	script := BuildCloseScript(wt)

	assert.Contains(t, script, `tell application "Ghostty"`)
	assert.Contains(t, script, `working directory contains "/tmp/test-worktree"`)
	assert.Contains(t, script, "close t")
}
