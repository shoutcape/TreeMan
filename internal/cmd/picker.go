package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
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

// pickerRequest describes one fzf invocation.
type pickerRequest struct {
	label  string
	prompt string
	query  string
	header string
}

// streamPickerRows writes the header row and then every row produce emits,
// numbering rows in emission order so a selection can be mapped back.
//
// Each row is written before the producer is asked for the next one, which is
// what lets fzf display results while the rest are still being fetched. A
// consumer that closes its end of the pipe — fzf does that the moment the user
// selects — ends the stream without an error.
func streamPickerRows(out io.Writer, header string, produce func(emit func(display string) error) error) (int, error) {
	if _, err := fmt.Fprintln(out, header); err != nil {
		if consumerClosed(err) {
			return 0, nil
		}
		return 0, err
	}

	count := 0
	err := produce(func(display string) error {
		if _, err := fmt.Fprintln(out, pickerRow(display, count)); err != nil {
			return err
		}
		count++
		return nil
	})
	if consumerClosed(err) {
		return count, nil
	}
	return count, err
}

// consumerClosed reports whether a write failed because the reader is gone.
func consumerClosed(err error) bool {
	return errors.Is(err, syscall.EPIPE) || errors.Is(err, os.ErrClosed)
}

// runStreamingPicker starts fzf and feeds it rows as produce emits them, so
// the picker is on screen and usable before every result has arrived.
//
// It returns the zero-based index of the selected row and the number of rows
// that were streamed. errPickerCancelled is returned when the user aborts.
func runStreamingPicker(cmd *cobra.Command, request pickerRequest, produce func(emit func(display string) error) error) (int, int, error) {
	args := append(pickerArgs(sessionFor(cmd).errorOutput.Color, request.label, request.prompt), "--header-lines=1")
	if request.query != "" {
		args = append(args, "--query", request.query)
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		return 0, 0, fmt.Errorf("could not open the picker input pipe: %w", err)
	}

	fzfCmd := exec.Command("fzf", args...)
	fzfCmd.Stdin = reader
	fzfCmd.Stderr = cmd.ErrOrStderr()
	var selection strings.Builder
	fzfCmd.Stdout = &selection

	if err := fzfCmd.Start(); err != nil {
		reader.Close()
		writer.Close()
		return 0, 0, fmt.Errorf("could not start fzf: %w", err)
	}
	// fzf owns the read end now; holding it open would keep fzf from seeing EOF.
	reader.Close()

	count, produceErr := streamPickerRows(writer, request.header, produce)
	writer.Close()

	if produceErr != nil {
		// The rows are incomplete, so whatever fzf shows cannot be trusted.
		_ = fzfCmd.Process.Kill()
		_ = fzfCmd.Wait()
		return 0, count, produceErr
	}

	if err := fzfCmd.Wait(); err != nil {
		if pickerCancelled(err) || count == 0 {
			return 0, count, errPickerCancelled
		}
		return 0, count, fmt.Errorf("fzf failed while selecting: %w", err)
	}

	index := pickerSelectionIndex(selection.String(), count)
	if index < 0 {
		if strings.TrimSpace(selection.String()) == "" {
			return 0, count, errPickerCancelled
		}
		return 0, count, fmt.Errorf("could not map the fzf selection to a result")
	}
	return index, count, nil
}
