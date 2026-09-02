package forge

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunForgeCLI(t *testing.T) {
	t.Setenv("GO_WANT_FORGE_HELPER_PROCESS", "1")
	output, err := runForgeCLI(context.Background(), testHelperCommand(), []string{"-test.run=^TestForgeCLIHelperProcess$", "--", "success"}, []byte("request"), "test request")
	require.NoError(t, err)
	assert.Contains(t, string(output), "stdout:request")
	assert.NotContains(t, string(output), "diagnostic")

	output, err = runForgeCLI(context.Background(), testHelperCommand(), []string{"-test.run=^TestForgeCLIHelperProcess$", "--", "args", "one", "two"}, nil, "test args")
	require.NoError(t, err)
	assert.Contains(t, string(output), "args,one,two")

	_, err = runForgeCLI(context.Background(), testHelperCommand(), []string{"-test.run=^TestForgeCLIHelperProcess$", "--", "failure"}, nil, "test failure")
	require.Error(t, err)
	assert.EqualError(t, err, "test failure: failed request")

	_, err = runForgeCLI(context.Background(), testHelperCommand(), []string{"-test.run=^TestForgeCLIHelperProcess$", "--", "silent-failure"}, nil, "test silent failure")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "test silent failure: exit status 4")
}

func testHelperCommand() string {
	return os.Args[0]
}

func TestForgeCLIHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_FORGE_HELPER_PROCESS") != "1" {
		return
	}
	separator := 0
	for index, arg := range os.Args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator == 0 {
		return
	}
	if len(os.Args) == separator+1 {
		os.Exit(2)
	}
	args := os.Args[separator+1:]
	switch args[0] {
	case "success":
		input, err := io.ReadAll(os.Stdin)
		if err != nil {
			os.Exit(3)
		}
		_, _ = os.Stdout.WriteString("stdout:" + string(input))
		_, _ = os.Stderr.WriteString("diagnostic")
	case "args":
		_, _ = os.Stdout.WriteString(strings.Join(args, ","))
	case "failure":
		_, _ = os.Stderr.WriteString("  failed request\n")
		os.Exit(4)
	case "silent-failure":
		os.Exit(4)
	case "stream":
		_, _ = os.Stdout.WriteString(`[{"name":"one"}]`)
		time.Sleep(200 * time.Millisecond)
		_, _ = os.Stdout.WriteString(`[{"name":"two"}]`)
		// Exit before the test framework prints its own summary, which is not
		// JSON and would reach the decoder as trailing garbage.
		os.Exit(0)
	case "stream-ndjson":
		_, _ = os.Stdout.WriteString("{\"name\":\"one\"}\n")
		time.Sleep(200 * time.Millisecond)
		_, _ = os.Stdout.WriteString("{\"name\":\"two\"}\n")
		os.Exit(0)
	case "stream-partial":
		_, _ = os.Stdout.WriteString(`{"name":`)
		time.Sleep(30 * time.Second)
	case "stream-failure":
		_, _ = os.Stdout.WriteString(`[{"name":"one"}]`)
		_, _ = os.Stderr.WriteString("token expired\n")
		os.Exit(4)
	case "stream-hang":
		_, _ = os.Stdout.WriteString(`[{"name":"one"}]`)
		time.Sleep(30 * time.Second)
	default:
		os.Exit(5)
	}
}

type streamName struct {
	Name string `json:"name"`
}

func streamHelperArgs(mode string) []string {
	return []string{"-test.run=^TestForgeCLIHelperProcess$", "--", mode}
}

// Streaming exists so a page can be acted on while the CLI is still fetching
// the next one; buffering the whole response would collapse the gap.
func TestRunForgeCLIStreamDeliversPagesAsTheyArePrinted(t *testing.T) {
	t.Setenv("GO_WANT_FORGE_HELPER_PROCESS", "1")

	var arrivals []time.Duration
	start := time.Now()
	err := runForgeCLIStream(context.Background(), testHelperCommand(), streamHelperArgs("stream"), "test stream",
		func(out io.Reader) error {
			return decodePaginatedStream(out, func(page []streamName) error {
				arrivals = append(arrivals, time.Since(start))
				assert.Len(t, page, 1)
				return nil
			})
		})

	require.NoError(t, err)
	require.Len(t, arrivals, 2)
	assert.Greater(t, arrivals[1]-arrivals[0], 100*time.Millisecond,
		"the first page should be delivered long before the second is printed")
}

func TestRunForgeCLIStreamDeliversNDJSONBeforeProcessCompletion(t *testing.T) {
	t.Setenv("GO_WANT_FORGE_HELPER_PROCESS", "1")

	var arrivals []time.Duration
	start := time.Now()
	err := runForgeCLIStream(context.Background(), testHelperCommand(), streamHelperArgs("stream-ndjson"), "test ndjson",
		func(out io.Reader) error {
			return decodeNDJSONStream(out, func(record streamName) error {
				arrivals = append(arrivals, time.Since(start))
				assert.NotEmpty(t, record.Name)
				return nil
			})
		})

	require.NoError(t, err)
	require.Len(t, arrivals, 2)
	assert.Greater(t, arrivals[1]-arrivals[0], 100*time.Millisecond)
}

// A consumer that stops — a closed picker, say — must not leave the CLI
// fetching the rest.
func TestRunForgeCLIStreamStopsTheCommandWhenTheConsumerFails(t *testing.T) {
	t.Setenv("GO_WANT_FORGE_HELPER_PROCESS", "1")

	start := time.Now()
	err := runForgeCLIStream(context.Background(), testHelperCommand(), streamHelperArgs("stream-hang"), "test hang",
		func(out io.Reader) error {
			return decodePaginatedStream(out, func([]streamName) error { return assert.AnError })
		})

	require.ErrorIs(t, err, assert.AnError)
	assert.Less(t, time.Since(start), 10*time.Second, "the CLI should be killed, not waited out")
}

// The CLI's own diagnostic explains a failure better than whatever the decoder
// made of its truncated output.
func TestRunForgeCLIStreamReportsTheCLIDiagnostic(t *testing.T) {
	t.Setenv("GO_WANT_FORGE_HELPER_PROCESS", "1")

	err := runForgeCLIStream(context.Background(), testHelperCommand(), streamHelperArgs("stream-failure"), "test stream failure",
		func(out io.Reader) error {
			return decodePaginatedStream(out, func([]streamName) error { return nil })
		})

	require.Error(t, err)
	assert.EqualError(t, err, "test stream failure: token expired")
}

func TestRunForgeCLIStreamStopsOnACancelledContext(t *testing.T) {
	t.Setenv("GO_WANT_FORGE_HELPER_PROCESS", "1")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runForgeCLIStream(ctx, testHelperCommand(), streamHelperArgs("stream-partial"), "test cancel",
			func(out io.Reader) error {
				return decodeNDJSONStream(out, func(streamName) error { return nil })
			})
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(10 * time.Second):
		t.Fatal("cancelling did not stop the CLI")
	}
}

// A consumer that reached its row budget is done, not broken: the CLI is
// stopped and the call succeeds, so a bounded list is not reported as a
// failed one.
func TestRunForgeCLIStreamStopsTheCommandWhenTheConsumerIsDone(t *testing.T) {
	t.Setenv("GO_WANT_FORGE_HELPER_PROCESS", "1")

	pages := 0
	start := time.Now()
	err := runForgeCLIStream(context.Background(), testHelperCommand(), streamHelperArgs("stream-hang"), "test done",
		func(out io.Reader) error {
			return decodePaginatedStream(out, func([]streamName) error {
				pages++
				return errStopStream
			})
		})

	require.NoError(t, err)
	assert.Equal(t, 1, pages)
	assert.Less(t, time.Since(start), 10*time.Second, "the CLI should be killed, not waited out")
}

// Closing the picker still reports the cancellation, whether or not the
// consumer had also finished with the stream.
func TestRunForgeCLIStreamReportsCancellationOverAFinishedConsumer(t *testing.T) {
	t.Setenv("GO_WANT_FORGE_HELPER_PROCESS", "1")

	ctx, cancel := context.WithCancel(context.Background())
	err := runForgeCLIStream(ctx, testHelperCommand(), streamHelperArgs("stream-hang"), "test cancel",
		func(out io.Reader) error {
			return decodePaginatedStream(out, func([]streamName) error {
				cancel()
				return errStopStream
			})
		})

	require.ErrorIs(t, err, context.Canceled)
}
