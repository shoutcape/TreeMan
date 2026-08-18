package ui

import (
	"fmt"
	"path/filepath"
	"strings"
)

// WorktreeRow formats a single worktree entry for the fzf picker.
//
// The display shows the last two path components and branch name.
//
// Column width for the path component is 40 characters.
func WorktreeRow(path, branch string) string {
	short := shortPath(path)
	return RenderPath(fmt.Sprintf("%-40s", short)) + "  " + RenderBranch(branch)
}

// PRHeader returns the column header row for the PR/MR fzf picker.
func PRHeader() string {
	return RenderHeader(fmt.Sprintf("%-8s", "PR/MR")) + "  " +
		RenderHeader(fmt.Sprintf("%-32s", "Branch")) + "  " + RenderHeader("Title")
}

// PRRow formats a single PR/MR entry for the fzf picker.
//
// number is the PR/MR number, branch is truncated to 32 chars, title is the remainder.
func PRRow(number int, branch, title string) string {
	prNum := fmt.Sprintf("#%d", number)
	truncBranch := truncate(branch, 32)
	return RenderPR(fmt.Sprintf("%-8s", prNum)) + "  " +
		RenderBranch(fmt.Sprintf("%-32s", truncBranch)) + "  " + RenderPath(title)
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
		if s[i] == '\033' && i+1 < len(s) && s[i+1] == ']' {
			// Skip OSC sequences, including OSC 8 terminal hyperlinks.
			i += 2
			for i < len(s) {
				if s[i] == '\a' {
					i++
					break
				}
				if s[i] == '\033' && i+1 < len(s) && s[i+1] == '\\' {
					i += 2
					break
				}
				i++
			}
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// BranchHeader returns the column header row for the branch fzf picker.
func BranchHeader() string {
	return RenderHeader(fmt.Sprintf("%-50s", "Branch")) + "  " +
		RenderHeader(fmt.Sprintf("%-14s", "Last Updated")) + "  " + RenderHeader("MR/PR")
}

// BranchRow formats a single remote branch entry for the fzf picker.
// If mrNumber > 0, it displays the MR/PR number in the third column.
func BranchRow(branch, date string, mrNumber int) string {
	truncBranch := truncate(branch, 50)
	mr := ""
	if mrNumber > 0 {
		mr = fmt.Sprintf("#%d", mrNumber)
	}
	return RenderBranch(fmt.Sprintf("%-50s", truncBranch)) + "  " +
		RenderMuted(fmt.Sprintf("%-14s", date)) + "  " + RenderPR(mr)
}
