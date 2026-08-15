// Package ui provides ANSI output helpers for treeman.
package ui

// TreeMan brand color ANSI escape sequences (24-bit / true color).
const (
	// ColorPath is a warm brown (#C4915E) used for worktree paths.
	ColorPath = "\033[38;2;196;145;94m"

	// ColorBranch is a bright olive (#B2B644) used for branch names.
	ColorBranch = "\033[38;2;178;182;68m"

	// ColorPR is a golden yellow (#F2EA72) used for PR/MR numbers.
	ColorPR = "\033[38;2;242;234;114m"

	// ColorReset resets all ANSI attributes.
	ColorReset = "\033[0m"
)
