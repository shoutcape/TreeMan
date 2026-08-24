package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBenchmarkCommandIsRegistered(t *testing.T) {
	command, _, err := New("test", "test", "test").Find([]string{"benchmark"})

	require.NoError(t, err)
	assert.Equal(t, "benchmark", command.Name())
	assert.Equal(t, []string{"list"}, command.ValidArgs)
}

func TestBenchmarkValidatesTargetAndCounts(t *testing.T) {
	cmd := &cobra.Command{}

	require.EqualError(t, runBenchmark(cmd, "unknown", 0, 1), `unknown benchmark target "unknown" (available: list)`)
	require.EqualError(t, runBenchmark(cmd, "list", 0, 0), "benchmark runs must be at least 1")
	require.EqualError(t, runBenchmark(cmd, "list", -1, 1), "benchmark warmup cannot be negative")
}

func TestBenchmarkRunsTargetAndSuppressesItsOutput(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	calls := 0
	runner := func(runCmd *cobra.Command) error {
		calls++
		fmt.Fprint(runCmd.OutOrStdout(), "target stdout")
		fmt.Fprint(runCmd.ErrOrStderr(), "target stderr")
		return nil
	}

	require.NoError(t, runBenchmarkWithRunner(cmd, "list", 1, 2, runner))

	assert.Equal(t, 3, calls)
	assert.Empty(t, stdout.String())
	assert.NotContains(t, stderr.String(), "target stdout")
	assert.NotContains(t, stderr.String(), "target stderr")
	assert.Contains(t, stderr.String(), "command: list   warmup: 1   runs: 2")
	assert.Contains(t, stderr.String(), "RESULTS")
	assert.Contains(t, stderr.String(), "mean:")
	assert.Contains(t, stderr.String(), "stddev:")
	assert.Contains(t, stderr.String(), "min:")
	assert.Contains(t, stderr.String(), "max:")
}

func TestBenchmarkReportsRunnerFailure(t *testing.T) {
	err := runBenchmarkWithRunner(&cobra.Command{}, "list", 1, 1, func(*cobra.Command) error {
		return errors.New("failed")
	})

	require.EqualError(t, err, "warmup run 1 failed: failed")
}

func TestCalculateBenchmarkStats(t *testing.T) {
	mean, stddev, minDuration, maxDuration := calcStats([]time.Duration{
		time.Millisecond,
		3 * time.Millisecond,
		5 * time.Millisecond,
	})

	assert.Equal(t, 3*time.Millisecond, mean)
	assert.Equal(t, time.Duration(1632993), stddev)
	assert.Equal(t, time.Millisecond, minDuration)
	assert.Equal(t, 5*time.Millisecond, maxDuration)
}

func TestFormatBenchmarkDuration(t *testing.T) {
	assert.Equal(t, "2.000 s", formatDuration(2*time.Second))
	assert.Equal(t, "2.5 ms", formatDuration(2500*time.Microsecond))
	assert.Equal(t, "2.5 us", formatDuration(2500*time.Nanosecond))
}
