package cmd

import (
	"context"
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

// stopPicker ends an fzf that is still waiting for a choice and reports how it
// exited, taken from the goroutine that owns its Wait.
//
// SIGTERM, not Kill: fzf restores the terminal when it is asked to terminate,
// while a killed fzf leaves the cursor hidden for everything printed after it.
func stopPicker(fzfCmd *exec.Cmd, exited <-chan error) error {
	if err := fzfCmd.Process.Signal(syscall.SIGTERM); err != nil {
		_ = fzfCmd.Process.Kill()
	}
	return <-exited
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

// pickerProducer emits the picker's rows. It is handed the picker's context so
// that a picker the user closes stops the work that was still fetching rows
// for it.
type pickerProducer func(ctx context.Context, emit func(display string) error) error

// streamPickerRows writes the header row and then every row produce emits,
// numbering rows in emission order so a selection can be mapped back.
//
// Each row is written before the producer is asked for the next one, which is
// what lets fzf display results while the rest are still being fetched. A
// consumer that closes its end of the pipe — fzf does that the moment the user
// selects — ends the stream without an error.
func streamPickerRows(ctx context.Context, out io.Writer, header string, produce pickerProducer) (int, error) {
	if _, err := fmt.Fprintln(out, header); err != nil {
		if consumerClosed(err) {
			return 0, nil
		}
		return 0, err
	}

	count := 0
	err := produce(ctx, func(display string) error {
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
// The producer and fzf run concurrently and each one ends the other: a picker
// the user closes cancels the context the producer's requests run under, and a
// producer that fails stops the picker whose rows can no longer be trusted.
// Either way the producer is joined before this returns, so the caller can
// safely read whatever state it filled in.
//
// It returns the zero-based index of the selected row and the number of rows
// that were streamed. errPickerCancelled is returned when the user aborts.
func runStreamingPicker(cmd *cobra.Command, request pickerRequest, produce pickerProducer) (int, int, error) {
	args := streamingPickerArgs(sessionFor(cmd).errorOutput.Color, request)

	ctx, cancel := context.WithCancel(commandContext(cmd))
	defer cancel()

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

	type streamed struct {
		count int
		err   error
	}
	rows := make(chan streamed, 1)
	go func() {
		count, err := streamPickerRows(ctx, writer, request.header, produce)
		// Closing the write end is what tells fzf the list is complete.
		writer.Close()
		rows <- streamed{count: count, err: err}
	}()

	exited := make(chan error, 1)
	go func() { exited <- fzfCmd.Wait() }()

	select {
	case <-ctx.Done():
		// Ending fzf closes the pipe if the producer is blocked writing. Then
		// cancel and join both sides before reporting the parent cancellation.
		exitErr := stopPicker(fzfCmd, exited)
		cancel()
		result := <-rows
		if err := commandContext(cmd).Err(); err != nil {
			return 0, result.count, err
		}
		return pickerSelection(exitErr, selection.String(), result.count)

	case result := <-rows:
		if result.err != nil {
			// The rows are incomplete, so whatever fzf shows cannot be trusted.
			cancel()
			_ = stopPicker(fzfCmd, exited)
			return 0, result.count, result.err
		}
		if !pickerSettlesItself(request) && result.count <= 1 {
			// Do what --select-1 and --exit-0 would have done at end of input.
			_ = stopPicker(fzfCmd, exited)
			if result.count == 0 {
				return 0, 0, errPickerCancelled
			}
			return 0, 1, nil
		}
		select {
		case exitErr := <-exited:
			return pickerSelection(exitErr, selection.String(), result.count)
		case <-ctx.Done():
			_ = stopPicker(fzfCmd, exited)
			cancel()
			return 0, result.count, ctx.Err()
		}

	case exitErr := <-exited:
		// fzf is gone: the user chose or aborted, so nothing is waiting for the
		// rest of the rows. Cancelling stops the requests still in flight
		// instead of blocking on them.
		cancel()
		result := <-rows
		// Cancelling is this function's own doing, so the error it raises is
		// not news; the user is done either way. A producer that failed on its
		// own before writing anything is different: its failure is the only
		// explanation there is for the empty list.
		if result.err != nil && result.count == 0 && !errors.Is(result.err, context.Canceled) {
			return 0, 0, result.err
		}
		return pickerSelection(exitErr, selection.String(), result.count)
	}
}

// pickerSelection maps how fzf exited, and what it printed, onto the selected
// row index.
func pickerSelection(exitErr error, selection string, count int) (int, int, error) {
	if exitErr != nil {
		if pickerCancelled(exitErr) || count == 0 {
			return 0, count, errPickerCancelled
		}
		return 0, count, fmt.Errorf("fzf failed while selecting: %w", exitErr)
	}

	index := pickerSelectionIndex(selection, count)
	if index < 0 {
		if strings.TrimSpace(selection) == "" {
			return 0, count, errPickerCancelled
		}
		return 0, count, fmt.Errorf("could not map the fzf selection to a result")
	}
	return index, count, nil
}
