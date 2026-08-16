// Package ui provides ANSI output helpers for treeman.
package ui

import (
	"fmt"
	"io"
	"os"
)

// TreeMan brand color ANSI escape sequences (24-bit / true color).
const (
	colorPath    = "\033[38;2;196;145;94m"
	colorBranch  = "\033[38;2;178;182;68m"
	colorPR      = "\033[38;2;242;234;114m"
	colorStatus  = "\033[38;2;123;216;143m"
	colorWarning = "\033[38;2;242;166;90m"
	colorCyan    = "\033[1;38;2;111;184;210m"
	colorDim     = "\033[2m"
	colorReset   = "\033[0m"
)

var (
	// ColorPath is a warm brown (#C4915E) used for worktree paths.
	ColorPath = colorPath

	// ColorBranch is a bright olive (#B2B644) used for branch names.
	ColorBranch = colorBranch

	// ColorPR is a golden yellow (#F2EA72) used for PR/MR numbers.
	ColorPR = colorPR

	// ColorStatus is a soft green (#7BD88F) used for clean and current state.
	ColorStatus = colorStatus

	// ColorWarning is a warm orange (#F2A65A) used for changed worktrees.
	ColorWarning = colorWarning

	// ColorCyan is a muted cyan used for table headings.
	ColorCyan = colorCyan

	// ColorDim reduces emphasis for table decorations.
	ColorDim = colorDim

	// ColorReset resets all ANSI attributes.
	ColorReset = colorReset
)

// ConfigureColor applies the requested color mode to output.
func ConfigureColor(mode string, output io.Writer, noColor bool) error {
	enabled, err := ColorEnabled(mode, output, noColor)
	if err != nil {
		return err
	}
	if enabled {
		ColorPath, ColorBranch, ColorPR, ColorStatus = colorPath, colorBranch, colorPR, colorStatus
		ColorWarning, ColorCyan, ColorDim, ColorReset = colorWarning, colorCyan, colorDim, colorReset
		return nil
	}
	ColorPath, ColorBranch, ColorPR, ColorStatus = "", "", "", ""
	ColorWarning, ColorCyan, ColorDim, ColorReset = "", "", "", ""
	return nil
}

// ColorEnabled reports whether mode should emit ANSI escape sequences.
func ColorEnabled(mode string, output io.Writer, noColor bool) (bool, error) {
	if noColor || mode == "never" {
		return false, nil
	}
	if mode == "always" {
		return true, nil
	}
	if mode != "auto" {
		return false, fmt.Errorf("invalid color mode %q: use auto, always, or never", mode)
	}
	file, ok := output.(*os.File)
	if !ok {
		return false, nil
	}
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	return info.Mode()&os.ModeCharDevice != 0, nil
}
