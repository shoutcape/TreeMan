package cmd

import (
	"bytes"
	"testing"
	"time"

	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunBenchmarkValidatesCounts(t *testing.T) {
	err := runBenchmark(&cobra.Command{}, 0, 0)
	assert.EqualError(t, err, "benchmark runs must be at least 1")

	err = runBenchmark(&cobra.Command{}, -1, 1)
	assert.EqualError(t, err, "benchmark warmup cannot be negative")
}

func TestRunBenchmarkReportsProgressBeforeEachRun(t *testing.T) {
	output := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetErr(output)
	var calls int
	err := runBenchmarkWithRunner(cmd, 2, 3, func() error {
		calls++
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 5, calls)
	text := ui.StripANSI(output.String())
	assert.Contains(t, text, "warmup 1/2")
	assert.Contains(t, text, "warmup 2/2")
	assert.Contains(t, text, "run  1/3")
	assert.Contains(t, text, "run  3/3")
	assert.Contains(t, text, "RESULTS")
}

func TestRunBenchmarkReportsProgressBeforeFailure(t *testing.T) {
	output := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetErr(output)
	calls := 0
	err := runBenchmarkWithRunner(cmd, 1, 2, func() error {
		calls++
		if calls == 3 {
			return assert.AnError
		}
		return nil
	})

	assert.EqualError(t, err, "run 2 failed: assert.AnError general error for testing")
	text := ui.StripANSI(output.String())
	assert.Contains(t, text, "warmup 1/1")
	assert.Contains(t, text, "run  1/2")
	assert.Contains(t, text, "run  2/2")
	assert.NotContains(t, text, "RESULTS")
}

func TestTimeRunReturnsRunnerError(t *testing.T) {
	_, err := timeRun(func() error { return assert.AnError })
	assert.EqualError(t, err, "assert.AnError general error for testing")
}

func TestBenchmarkHelpers(t *testing.T) {
	mean, stddev, min, max := calcStats([]time.Duration{time.Millisecond, 3 * time.Millisecond})
	assert.Equal(t, 2*time.Millisecond, mean)
	assert.Equal(t, time.Millisecond, stddev)
	assert.Equal(t, time.Millisecond, min)
	assert.Equal(t, 3*time.Millisecond, max)
	assert.Equal(t, "1.0 ms", formatDuration(time.Millisecond))
	assert.Equal(t, "1.0 us", formatDuration(time.Microsecond))
}

func TestBenchmarkCommandIsRegistered(t *testing.T) {
	command, _, err := New("", "", "").Find([]string{"benchmark"})
	require.NoError(t, err)
	assert.Equal(t, "benchmark", command.Name())
}
