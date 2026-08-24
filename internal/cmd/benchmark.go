package cmd

import (
	"fmt"
	"io"
	"math"
	"time"

	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
)

func newBenchmarkCmd() *cobra.Command {
	var runs int
	var warmup int
	cmd := &cobra.Command{
		Use:   "benchmark",
		Short: "Measure execution time of treeman list",
		Long: `Measure execution time of treeman list.

Runs list multiple times and reports mean, min, max, and
standard deviation. Warmup runs are excluded from results.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBenchmark(cmd, warmup, runs)
		},
	}
	cmd.Flags().IntVar(&runs, "runs", 10, "Number of timed runs")
	cmd.Flags().IntVar(&warmup, "warmup", 3, "Number of warmup runs (excluded from results)")
	return cmd
}

func runBenchmark(cmd *cobra.Command, warmup, runs int) error {
	return runBenchmarkWithRunner(cmd, warmup, runs, discardedListRunner())
}

func runBenchmarkWithRunner(cmd *cobra.Command, warmup, runs int, run func() error) error {
	if runs < 1 {
		return fmt.Errorf("benchmark runs must be at least 1")
	}
	if warmup < 0 {
		return fmt.Errorf("benchmark warmup cannot be negative")
	}

	render := commandRenderer(cmd)
	errOut := cmd.ErrOrStderr()

	fmt.Fprintf(errOut, "\n%s\n\n", render.Title("BENCHMARK"))
	fmt.Fprintf(errOut, "  %s\n\n", render.Muted(fmt.Sprintf("command: list   warmup: %d   runs: %d", warmup, runs)))

	for i := range warmup {
		fmt.Fprintf(errOut, "  %s\n", render.Status(ui.ToneMuted, "~", fmt.Sprintf("warmup %d/%d", i+1, warmup)))
		if err := run(); err != nil {
			return fmt.Errorf("warmup run %d failed: %w", i+1, err)
		}
	}
	if warmup > 0 {
		fmt.Fprintln(errOut)
	}

	durations := make([]time.Duration, 0, runs)
	for i := range runs {
		fmt.Fprintf(errOut, "  %s\n", render.Status(ui.ToneInfo, "->", fmt.Sprintf("run %2d/%d", i+1, runs)))
		duration, err := timeRun(run)
		if err != nil {
			return fmt.Errorf("run %d failed: %w", i+1, err)
		}
		durations = append(durations, duration)
	}
	mean, stddev, min, max := calcStats(durations)
	result := benchmarkResult{Durations: durations, Mean: mean, Stddev: stddev, Min: min, Max: max}

	fmt.Fprintf(errOut, "\n  %s\n", render.Header("RESULTS"))
	fmt.Fprintf(errOut, "  %s\n", render.Muted(fmt.Sprintf("mean:   %s", formatDuration(result.Mean))))
	fmt.Fprintf(errOut, "  %s\n", render.Muted(fmt.Sprintf("stddev: %s", formatDuration(result.Stddev))))
	fmt.Fprintf(errOut, "  %s\n", render.Muted(fmt.Sprintf("min:    %s", formatDuration(result.Min))))
	fmt.Fprintf(errOut, "  %s\n\n", render.Muted(fmt.Sprintf("max:    %s", formatDuration(result.Max))))

	return nil
}

func discardedListRunner() func() error {
	return func() error {
		cmd := newListCmd()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		return runList(cmd, false)
	}
}

type benchmarkResult struct {
	Durations []time.Duration
	Mean      time.Duration
	Stddev    time.Duration
	Min       time.Duration
	Max       time.Duration
}

func timeRun(run func() error) (time.Duration, error) {
	start := time.Now()
	if err := run(); err != nil {
		return 0, err
	}
	return time.Since(start), nil
}

func calcStats(durations []time.Duration) (mean, stddev, min, max time.Duration) {
	if len(durations) == 0 {
		return
	}
	min = durations[0]
	max = durations[0]
	var sum time.Duration
	for _, d := range durations {
		sum += d
		if d < min {
			min = d
		}
		if d > max {
			max = d
		}
	}
	mean = sum / time.Duration(len(durations))

	var variance float64
	for _, d := range durations {
		diff := float64(d - mean)
		variance += diff * diff
	}
	variance /= float64(len(durations))
	stddev = time.Duration(math.Sqrt(variance))
	return
}

func formatDuration(d time.Duration) string {
	switch {
	case d >= time.Second:
		return fmt.Sprintf("%.3f s", d.Seconds())
	case d >= time.Millisecond:
		return fmt.Sprintf("%.1f ms", float64(d)/float64(time.Millisecond))
	default:
		return fmt.Sprintf("%.1f us", float64(d)/float64(time.Microsecond))
	}
}
