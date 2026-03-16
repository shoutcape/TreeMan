// Package ui provides terminal output helpers for treeman.
package ui

// TreeMan brand color ANSI escape sequences (24-bit / true color).
// These match the palette used in wt.sh:223-229.
const (
	// ColorPath is a warm brown (#C4915E) used for worktree paths.
	ColorPath = "\033[38;2;196;145;94m"

	// ColorBranch is a bright olive (#B2B644) used for branch names.
	ColorBranch = "\033[38;2;178;182;68m"

	// ColorPR is a golden yellow (#F2EA72) used for PR/MR numbers.
	ColorPR = "\033[38;2;242;234;114m"

	// ColorReset resets all terminal attributes.
	ColorReset = "\033[0m"
)

// Colorize wraps text in the given color code and appends a reset.
func Colorize(color, text string) string {
	return color + text + ColorReset
}
