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

// pickerBaseArgs is the fzf configuration every picker shares.
func pickerBaseArgs(color bool, label, prompt string) []string {
	args := []string{
		"--height=40%",
		"--layout=reverse",
		"--border=rounded",
		"--delimiter=\t",
		"--with-nth=1",
		"--border-label", label,
		"--prompt=" + prompt,
	}
	if color {
		return append(args, "--ansi", "--color="+ui.FZFColors())
	}
	return append(args, "--no-color")
}

// pickerArgs configures a picker whose whole list is ready before fzf starts.
// --select-1 and --exit-0 let fzf settle a single-result or empty list without
// showing anything.
func pickerArgs(color bool, label, prompt string) []string {
	return append(pickerBaseArgs(color, label, prompt), "--select-1", "--exit-0")
}

// streamingPickerArgs configures a picker that is fed while it runs.
//
// fzf paints nothing at all while --select-1 or --exit-0 is set, because
// either can make it exit before it has shown a UI. On a streamed list that
// leaves the terminal blank until the first row lands, which is most of the
// wait. So those flags are used only alongside a query, where fzf is the only
// one that can tell how many rows match it. Without them runStreamingPicker
// settles the single-result and empty cases itself.
func streamingPickerArgs(color bool, request pickerRequest) []string {
	args := append(pickerBaseArgs(color, request.label, request.prompt), "--header-lines=1")
	if request.query != "" {
		args = append(args, "--query", request.query, "--select-1", "--exit-0")
	}
	return args
}

// pickerSettlesItself reports whether fzf was given the flags that let it
// resolve a single-result or empty list on its own.
func pickerSettlesItself(request pickerRequest) bool {
	return request.query != ""
}

// stopPicker ends an fzf that is still waiting for a choice.
//
// SIGTERM, not Kill: fzf restores the terminal when it is asked to terminate,
// while a killed fzf leaves the cursor hidden for everything printed after it.
func stopPicker(fzfCmd *exec.Cmd) {
	if err := fzfCmd.Process.Signal(syscall.SIGTERM); err != nil {
		_ = fzfCmd.Process.Kill()
	}
	_ = fzfCmd.Wait()
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
	args := streamingPickerArgs(sessionFor(cmd).errorOutput.Color, request)

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
		stopPicker(fzfCmd)
		return 0, count, produceErr
	}

	if !pickerSettlesItself(request) && count <= 1 {
		// Do what --select-1 and --exit-0 would have done at end of input.
		stopPicker(fzfCmd)
		if count == 0 {
			return 0, 0, errPickerCancelled
		}
		return 0, 1, nil
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
