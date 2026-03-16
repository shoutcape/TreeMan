package cmd

import (
	"testing"

	"github.com/shoutcape/treeman/internal/ui"
	"github.com/stretchr/testify/assert"
)

func TestMatchIndex_Found(t *testing.T) {
	displayLines := []string{
		ui.WorktreeRow("/home/user/repo.feature-a", "feature/a"),
		ui.WorktreeRow("/home/user/repo.fix-b", "fix/b"),
	}

	// fzf returns the display line for "feature/a" (possibly with ANSI already
	// stripped since fzf --ansi returns plain text).
	plain := ui.StripANSI(displayLines[0])
	idx := matchIndex(displayLines, plain)
	assert.Equal(t, 0, idx)
}

func TestMatchIndex_Second(t *testing.T) {
	displayLines := []string{
		ui.WorktreeRow("/home/user/repo.feature-a", "feature/a"),
		ui.WorktreeRow("/home/user/repo.fix-b", "fix/b"),
	}
	plain := ui.StripANSI(displayLines[1])
	idx := matchIndex(displayLines, plain)
	assert.Equal(t, 1, idx)
}

func TestMatchIndex_NotFound(t *testing.T) {
	displayLines := []string{
		ui.WorktreeRow("/home/user/repo.feature-a", "feature/a"),
	}
	idx := matchIndex(displayLines, "does-not-exist")
	assert.Equal(t, -1, idx)
}

func TestMatchPath_Found(t *testing.T) {
	displayLines := []string{
		ui.WorktreeRow("/home/user/repo", "main"),
		ui.WorktreeRow("/home/user/repo.feat", "feat"),
	}
	fullPaths := []string{"/home/user/repo", "/home/user/repo.feat"}

	plain := ui.StripANSI(displayLines[1])
	dest := matchPath(displayLines, fullPaths, plain)
	assert.Equal(t, "/home/user/repo.feat", dest)
}

func TestMatchPath_NotFound(t *testing.T) {
	displayLines := []string{ui.WorktreeRow("/home/user/repo", "main")}
	fullPaths := []string{"/home/user/repo"}

	dest := matchPath(displayLines, fullPaths, "garbage")
	assert.Equal(t, "", dest)
}
