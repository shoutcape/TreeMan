package ui

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// FzfOptions configures the fzf picker.
type FzfOptions struct {
	BorderLabel string   // label for the fzf border
	Prompt      string   // prompt text
	Query       string   // initial query
	Ansi        bool     // enable ANSI color parsing
	ExtraArgs   []string // additional fzf arguments
}

// Fzf runs fzf with the given items and returns the selected item.
// Returns empty string if the user cancels (Esc/Ctrl-C).
func Fzf(items []string, opts FzfOptions) (string, error) {
	if _, err := exec.LookPath("fzf"); err != nil {
		return "", fmt.Errorf("fzf is required but not installed. Install it from https://github.com/junegunn/fzf")
	}

	args := []string{"--select-1", "--exit-0"}
	if opts.Ansi {
		args = append(args, "--ansi")
	}
	if opts.BorderLabel != "" {
		args = append(args, "--border-label="+opts.BorderLabel)
	}
	if opts.Prompt != "" {
		args = append(args, "--prompt="+opts.Prompt)
	}
	if opts.Query != "" {
		args = append(args, "--query="+opts.Query)
	}
	args = append(args, opts.ExtraArgs...)

	cmd := exec.Command("fzf", args...)
	cmd.Stdin = strings.NewReader(strings.Join(items, "\n"))
	cmd.Stderr = os.Stderr

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	err := cmd.Run()
	if err != nil {
		// Exit code 130 = user cancelled (Ctrl-C), 1 = no match
		if exitErr, ok := err.(*exec.ExitError); ok {
			code := exitErr.ExitCode()
			if code == 130 || code == 1 {
				return "", nil
			}
		}
		return "", fmt.Errorf("fzf: %w", err)
	}

	return strings.TrimSpace(stdout.String()), nil
}
