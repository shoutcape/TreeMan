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

func pickerArgs(label, prompt string) []string {
	return []string{
		"--ansi",
		"--height=40%",
		"--layout=reverse",
		"--border=rounded",
		"--delimiter=\t",
		"--with-nth=1",
		"--border-label", label,
		"--prompt=" + prompt,
		"--color=" + ui.FZFColors(),
		"--select-1",
		"--exit-0",
	}
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
