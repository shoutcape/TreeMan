package git

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseWorktreePorcelain(t *testing.T) {
	input := `worktree /home/user/Github/my-project
HEAD abc1234
branch refs/heads/main

worktree /home/user/Github/my-project.feature-cool
HEAD def5678
branch refs/heads/feature/cool

worktree /home/user/Github/my-project.detached
HEAD fff0000
detached

`
	entries := parseWorktreePorcelain(input)

	assert.Len(t, entries, 3)

	assert.Equal(t, "/home/user/Github/my-project", entries[0].Path)
	assert.Equal(t, "main", entries[0].Branch)

	assert.Equal(t, "/home/user/Github/my-project.feature-cool", entries[1].Path)
	assert.Equal(t, "feature/cool", entries[1].Branch)

	// Detached HEAD — branch should be empty string.
	assert.Equal(t, "/home/user/Github/my-project.detached", entries[2].Path)
	assert.Equal(t, "", entries[2].Branch)
}

func TestParseWorktreePorcelain_NoTrailingNewline(t *testing.T) {
	// Some git versions omit the trailing blank line on the last entry.
	input := strings.TrimRight(`worktree /home/user/repo
HEAD abc1234
branch refs/heads/main
`, "\n")

	entries := parseWorktreePorcelain(input)
	assert.Len(t, entries, 1)
	assert.Equal(t, "/home/user/repo", entries[0].Path)
	assert.Equal(t, "main", entries[0].Branch)
}

func TestParseWorktreePorcelain_Empty(t *testing.T) {
	entries := parseWorktreePorcelain("")
	assert.Empty(t, entries)
}
