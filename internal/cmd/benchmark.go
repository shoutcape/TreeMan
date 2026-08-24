package cmd

import (
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
)

var benchmarkTargets = map[string]func(*cobra.Command) error{
	"list": func(cmd *cobra.Command) error { return runList(cmd, false) },
}

func newBenchmarkCmd() *cobra.Command {
	var runs int
	var warmup int
	cmd := &cobra.Command{
		Use:       "benchmark [command]",
		Short:     "Measure execution time of a treeman command",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: validBenchmarkTargets(),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "list"
			if len(args) == 1 {
				target = args[0]
			}
			return runBenchmark(cmd, target, warmup, runs)
		},
	}
	cmd.Flags().IntVar(&runs, "runs", 10, "Number of timed runs")
	cmd.Flags().IntVar(&warmup, "warmup", 3, "Number of warmup runs")
	return cmd
}

func validBenchmarkTargets() []string {
	targets := make([]string, 0, len(benchmarkTargets))
	for target := range benchmarkTargets {
		targets = append(targets, target)
	}
	slices.Sort(targets)
	return targets
}

func runBenchmark(cmd *cobra.Command, target string, warmup, runs int) error {
	runner, ok := benchmarkTargets[target]
	if !ok {
		return fmt.Errorf("unknown benchmark target %q (available: %s)", target, strings.Join(validBenchmarkTargets(), ", "))
	}
	return runBenchmarkWithRunner(cmd, target, warmup, runs, runner)
}

func runBenchmarkWithRunner(cmd *cobra.Command, target string, warmup, runs int, runner func(*cobra.Command) error) error {
	if runs < 1 {
		return fmt.Errorf("benchmark runs must be at least 1")
	}
	if warmup < 0 {
		return fmt.Errorf("benchmark warmup cannot be negative")
	}

	render := commandRenderer(cmd)
	errOut := cmd.ErrOrStderr()
	fmt.Fprintf(errOut, "\n%s\n\n", render.Title("BENCHMARK"))
	fmt.Fprintf(errOut, "  %s\n\n", render.Muted(fmt.Sprintf("command: %s   warmup: %d   runs: %d", target, warmup, runs)))

	silenced := silentCommand(cmd)
	for index := range warmup {
		fmt.Fprintf(errOut, "  %s\n", render.Status(ui.ToneMuted, "~", fmt.Sprintf("warmup %d/%d", index+1, warmup)))
		if err := runner(silenced); err != nil {
			return fmt.Errorf("warmup run %d failed: %w", index+1, err)
		}
	}
	if warmup > 0 {
		fmt.Fprintln(errOut)
	}

	durations := make([]time.Duration, 0, runs)
	for index := range runs {
		start := time.Now()
		if err := runner(silenced); err != nil {
			return fmt.Errorf("run %d failed: %w", index+1, err)
		}
		duration := time.Since(start)
		durations = append(durations, duration)
		fmt.Fprintf(errOut, "  %s\n", render.Status(ui.ToneInfo, "->", fmt.Sprintf("run %2d/%d  %s", index+1, runs, formatDuration(duration))))
	}

	mean, stddev, minDuration, maxDuration := calcStats(durations)
	fmt.Fprintf(errOut, "\n  %s\n", render.Header("RESULTS"))
	fmt.Fprintf(errOut, "  %s\n", render.Muted(fmt.Sprintf("mean:   %s", formatDuration(mean))))
	fmt.Fprintf(errOut, "  %s\n", render.Muted(fmt.Sprintf("stddev: %s", formatDuration(stddev))))
	fmt.Fprintf(errOut, "  %s\n", render.Muted(fmt.Sprintf("min:    %s", formatDuration(minDuration))))
	fmt.Fprintf(errOut, "  %s\n\n", render.Muted(fmt.Sprintf("max:    %s", formatDuration(maxDuration))))
	return nil
}

func silentCommand(cmd *cobra.Command) *cobra.Command {
	copy := *cmd
	copy.SetOut(io.Discard)
	copy.SetErr(io.Discard)
	return &copy
}

func calcStats(durations []time.Duration) (mean, stddev, minDuration, maxDuration time.Duration) {
	if len(durations) == 0 {
		return 0, 0, 0, 0
	}
	minDuration = durations[0]
	maxDuration = durations[0]
	var sum time.Duration
	for _, duration := range durations {
		sum += duration
		minDuration = min(minDuration, duration)
		maxDuration = max(maxDuration, duration)
	}
	mean = sum / time.Duration(len(durations))

	var variance float64
	for _, duration := range durations {
		difference := float64(duration - mean)
		variance += difference * difference
	}
	stddev = time.Duration(math.Sqrt(variance / float64(len(durations))))
	return mean, stddev, minDuration, maxDuration
}

func formatDuration(duration time.Duration) string {
	switch {
	case duration >= time.Second:
		return fmt.Sprintf("%.3f s", duration.Seconds())
	case duration >= time.Millisecond:
		return fmt.Sprintf("%.1f ms", float64(duration)/float64(time.Millisecond))
	default:
		return fmt.Sprintf("%.1f us", float64(duration)/float64(time.Microsecond))
	}
}
