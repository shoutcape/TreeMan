package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamPickerRowsWritesHeaderThenIndexedRows(t *testing.T) {
	out := &bytes.Buffer{}

	count, err := streamPickerRows(out, "HEADER", func(emit func(string) error) error {
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

	count, err := streamPickerRows(out, "HEADER", func(func(string) error) error { return nil })

	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, "HEADER\n", out.String())
}

// The picker is only faster than a buffered payload if each row reaches fzf
// before the next one is produced.
func TestStreamPickerRowsDeliversEachRowBeforeProducingTheNext(t *testing.T) {
	out := &bytes.Buffer{}
	var seen []string

	_, err := streamPickerRows(out, "HEADER", func(emit func(string) error) error {
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

	_, err := streamPickerRows(out, "HEADER", func(emit func(string) error) error {
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

	count, err := streamPickerRows(writer, "HEADER", func(emit func(string) error) error {
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
