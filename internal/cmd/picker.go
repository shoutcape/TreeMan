package cmd

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/shoutcape/treeman/internal/ui"
)

var errPickerCancelled = errors.New("picker cancelled")

func pickerArgs(color bool, label, prompt string) []string {
	args := []string{
		"--height=40%",
		"--layout=reverse",
		"--border=rounded",
		"--delimiter=\t",
		"--with-nth=1",
		"--border-label", label,
		"--prompt=" + prompt,
		"--select-1",
		"--exit-0",
	}
	if color {
		return append(args, "--ansi", "--color="+ui.FZFColors())
	}
	return append(args, "--no-color")
}

func pickerRow(display string, index int) string {
	return fmt.Sprintf("%s\t%d", display, index)
}

func pickerSelectionIndex(selection string, count int) int {
	selection = strings.TrimSpace(selection)
	separator := strings.LastIndex(selection, "\t")
	if separator < 0 {
		return -1
	}
	index, err := strconv.Atoi(selection[separator+1:])
	if err != nil || index < 0 || index >= count {
		return -1
	}
	return index
}

func pickerCancelled(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 130
}
