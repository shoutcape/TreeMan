package ui

import (
	"fmt"
	"path/filepath"
	"strings"
)

// WorktreeRow formats a single worktree entry for the fzf picker.
//
// The display shows the last two path components in ColorPath and the branch
// name in ColorBranch, mirroring _wt_display_worktrees in wt.sh:219-231.
//
// Column width for the path component is 40 characters.
func WorktreeRow(path, branch string) string {
	short := shortPath(path)
	return fmt.Sprintf("%s%-40s%s  %s%s%s",
		ColorPath, short, ColorReset,
		ColorBranch, branch, ColorReset,
	)
}

// PRHeader returns the column header row for the PR/MR fzf picker.
// Mirrors the header in _wt_pr_picker_display in wt.sh:347-351.
func PRHeader() string {
	return fmt.Sprintf("%s%-8s%s  %s%-32s%s  %s%s%s",
		ColorPR, "PR/MR", ColorReset,
		ColorBranch, "Branch", ColorReset,
		ColorPath, "Title", ColorReset,
	)
}

// PRRow formats a single PR/MR entry for the fzf picker.
// Mirrors the data rows in _wt_pr_picker_display in wt.sh:353-362.
//
// number is the PR/MR number, branch is truncated to 32 chars, title is the remainder.
func PRRow(number int, branch, title string) string {
	prNum := fmt.Sprintf("#%d", number)
	truncBranch := truncate(branch, 32)
	return fmt.Sprintf("%s%-8s%s  %s%-32s%s  %s%s%s",
		ColorPR, prNum, ColorReset,
		ColorBranch, truncBranch, ColorReset,
		ColorPath, title, ColorReset,
	)
}

// shortPath returns the last two path components of a filesystem path.
// If the path has only one component, that component is returned.
//
// Example: "/home/user/Github/my-project.feat-cool" → "Github/my-project.feat-cool"
func shortPath(path string) string {
	cleaned := filepath.Clean(path)
	base := filepath.Base(cleaned)
	parent := filepath.Base(filepath.Dir(cleaned))
	if parent == "." || parent == "/" {
		return base
	}
	return parent + "/" + base
}

// truncate returns s truncated to at most n runes.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// StripANSI removes ANSI escape sequences from s.
// Used when mapping fzf selections back to plain text for comparison.
func StripANSI(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\033' && i+1 < len(s) && s[i+1] == '[' {
			// skip until 'm'
			i += 2
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++ // skip 'm'
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
