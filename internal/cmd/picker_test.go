package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFZFHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_FZF_HELPER_PROCESS") != "1" {
		return
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	if marker := os.Getenv("FZF_EOF_MARKER"); marker != "" {
		_ = os.WriteFile(marker, nil, 0o600)
	}
	time.Sleep(30 * time.Second)
}

func TestRunStreamingPickerStopsFZFWhenParentIsCancelledAfterRowsFinish(t *testing.T) {
	t.Setenv("GO_WANT_FZF_HELPER_PROCESS", "1")
	directory := t.TempDir()
	marker := filepath.Join(directory, "eof")
	t.Setenv("FZF_EOF_MARKER", marker)
	helper := filepath.Join(directory, "fzf")
	script := "#!/bin/sh\nexec \"" + os.Args[0] + "\" -test.run=^TestFZFHelperProcess$\n"
	require.NoError(t, os.WriteFile(helper, []byte(script), 0o700))
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithCancel(context.Background())
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	done := make(chan error, 1)
	go func() {
		_, _, err := runStreamingPicker(cmd, pickerRequest{header: "HEADER"}, func(_ context.Context, emit func(string) error) error {
			if err := emit("one"); err != nil {
				return err
			}
			return emit("two")
		})
		done <- err
	}()

	require.Eventually(t, func() bool {
		_, err := os.Stat(marker)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("parent cancellation left fzf running")
	}
}

func TestStreamPickerRowsWritesHeaderThenIndexedRows(t *testing.T) {
	out := &bytes.Buffer{}

	count, err := streamPickerRows(context.Background(), out, "HEADER", func(_ context.Context, emit func(string) error) error {
		if err := emit("first"); err != nil {
			return err
		}
		return emit("second")
	})

	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Equal(t, "HEADER\nfirst\t0\nsecond\t1\n", out.String())
}

func TestStreamPickerRowsReportsAnEmptyProducer(t *testing.T) {
	out := &bytes.Buffer{}

	count, err := streamPickerRows(context.Background(), out, "HEADER", func(context.Context, func(string) error) error { return nil })

	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, "HEADER\n", out.String())
}

// The picker is only faster than a buffered payload if each row reaches fzf
// before the next one is produced.
func TestStreamPickerRowsDeliversEachRowBeforeProducingTheNext(t *testing.T) {
	out := &bytes.Buffer{}
	var seen []string

	_, err := streamPickerRows(context.Background(), out, "HEADER", func(_ context.Context, emit func(string) error) error {
		seen = append(seen, out.String())
		if err := emit("first"); err != nil {
			return err
		}
		seen = append(seen, out.String())
		return emit("second")
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"HEADER\n", "HEADER\nfirst\t0\n"}, seen)
}

func TestStreamPickerRowsPropagatesProducerFailure(t *testing.T) {
	out := &bytes.Buffer{}

	_, err := streamPickerRows(context.Background(), out, "HEADER", func(_ context.Context, emit func(string) error) error {
		require.NoError(t, emit("first"))
		return assert.AnError
	})

	assert.ErrorIs(t, err, assert.AnError)
}

// fzf closes its stdin as soon as the user selects, which must read as "the
// consumer is done", not as a failure.
func TestStreamPickerRowsTreatsAClosedConsumerAsDone(t *testing.T) {
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { writer.Close() })
	require.NoError(t, reader.Close())

	count, err := streamPickerRows(context.Background(), writer, "HEADER", func(_ context.Context, emit func(string) error) error {
		for range 100000 {
			if err := emit(strings.Repeat("row", 100)); err != nil {
				return err
			}
		}
		return nil
	})

	require.NoError(t, err)
	assert.Less(t, count, 100000)
}

// A picker whose list is ready up front can let fzf settle it, because there
// is no wait to shorten.
func TestPickerArgsLetFzfSettleACompleteList(t *testing.T) {
	args := pickerArgs(false, " worktrees ", "switch > ")

	assert.Contains(t, args, "--select-1")
	assert.Contains(t, args, "--exit-0")
}

// fzf paints nothing while --select-1 or --exit-0 is set, so a streamed picker
// without a query must not use them: the terminal would stay blank until the
// first row arrived.
func TestStreamingPickerArgsDropTheFlagsThatDelayThefirstPaint(t *testing.T) {
	args := streamingPickerArgs(false, pickerRequest{label: " branches ", prompt: "branch > "})

	assert.NotContains(t, args, "--select-1")
	assert.NotContains(t, args, "--exit-0")
	assert.NotContains(t, args, "--query")
	assert.Contains(t, args, "--header-lines=1")
	assert.Contains(t, args, "--prompt=branch > ")
	assert.False(t, pickerSettlesItself(pickerRequest{}))
}

// With a query only fzf knows how many rows match it, so it keeps the flags
// and the first paint waits.
func TestStreamingPickerArgsKeepTheFlagsForAQuery(t *testing.T) {
	request := pickerRequest{label: " branches ", prompt: "branch > ", query: "feature"}
	args := streamingPickerArgs(false, request)

	assert.Contains(t, args, "--select-1")
	assert.Contains(t, args, "--exit-0")
	assert.Contains(t, args, "--query")
	assert.Contains(t, args, "feature")
	assert.True(t, pickerSettlesItself(request))
}

// The picker resolves how fzf exited into a row index, so each way it can end
// has to map onto the right outcome.
func TestPickerSelectionMapsHowFzfExited(t *testing.T) {
	index, count, err := pickerSelection(nil, "branch\t2\n", 3)
	require.NoError(t, err)
	assert.Equal(t, 2, index)
	assert.Equal(t, 3, count)

	_, _, err = pickerSelection(nil, "", 3)
	assert.ErrorIs(t, err, errPickerCancelled, "an empty selection is an abort")

	_, _, err = pickerSelection(nil, "branch\t9\n", 3)
	assert.ErrorContains(t, err, "could not map the fzf selection")

	_, _, err = pickerSelection(assert.AnError, "", 0)
	assert.ErrorIs(t, err, errPickerCancelled, "nothing was ever offered to pick")

	_, _, err = pickerSelection(assert.AnError, "", 3)
	assert.ErrorContains(t, err, "fzf failed while selecting")
}
