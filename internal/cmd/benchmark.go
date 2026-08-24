package cmd

import (
	"fmt"
	"math"
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
		Use:   "benchmark [command]",
		Short: "Measure execution time of a treeman command",
		Long: `Measure execution time of a treeman command.

Runs the target command multiple times and reports mean, min, max, and
standard deviation. Warmup runs are excluded from results.

Available targets: list`,
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
	cmd.Flags().IntVar(&warmup, "warmup", 3, "Number of warmup runs (excluded from results)")
	return cmd
}

func validBenchmarkTargets() []string {
	keys := make([]string, 0, len(benchmarkTargets))
	for k := range benchmarkTargets {
		keys = append(keys, k)
	}
	return keys
}

func runBenchmark(cmd *cobra.Command, target string, warmup, runs int) error {
	fn, ok := benchmarkTargets[target]
	if !ok {
		return fmt.Errorf("unknown benchmark target %q -- available: %v", target, validBenchmarkTargets())
	}

	render := commandRenderer(cmd)
	errOut := cmd.ErrOrStderr()

	fmt.Fprintf(errOut, "\n%s\n\n", render.Title("BENCHMARK"))
	fmt.Fprintf(errOut, "  %s\n\n", render.Muted(fmt.Sprintf("target: %s   warmup: %d   runs: %d", target, warmup, runs)))

	// Warmup -- redirect output to discard so table doesn't flood the terminal.
	silenced := silenceOutput(cmd)
	for i := range warmup {
		fmt.Fprintf(errOut, "  %s\n", render.Status(ui.ToneMuted, "~", fmt.Sprintf("warmup %d/%d", i+1, warmup)))
		if err := fn(silenced); err != nil {
			return fmt.Errorf("warmup run %d failed: %w", i+1, err)
		}
	}
	if warmup > 0 {
		fmt.Fprintln(errOut)
	}

	// Timed runs.
	durations := make([]time.Duration, 0, runs)
	for i := range runs {
		start := time.Now()
		if err := fn(silenced); err != nil {
			return fmt.Errorf("run %d failed: %w", i+1, err)
		}
		d := time.Since(start)
		durations = append(durations, d)
		fmt.Fprintf(errOut, "  %s\n", render.Status(ui.ToneInfo, "->", fmt.Sprintf("run %2d/%d  %s", i+1, runs, formatDuration(d))))
	}

	mean, stddev, min, max := calcStats(durations)
	fmt.Fprintf(errOut, "\n  %s\n", render.Header("RESULTS"))
	fmt.Fprintf(errOut, "  %s\n", render.Muted(fmt.Sprintf("mean:   %s", formatDuration(mean))))
	fmt.Fprintf(errOut, "  %s\n", render.Muted(fmt.Sprintf("stddev: %s", formatDuration(stddev))))
	fmt.Fprintf(errOut, "  %s\n", render.Muted(fmt.Sprintf("min:    %s", formatDuration(min))))
	fmt.Fprintf(errOut, "  %s\n\n", render.Muted(fmt.Sprintf("max:    %s", formatDuration(max))))

	return nil
}

// silenceOutput returns a shallow copy of cmd with stdout/stderr discarded so
// benchmark runs don't flood the terminal with repeated list output.
func silenceOutput(cmd *cobra.Command) *cobra.Command {
	child := *cmd
	child.SetOut(io_discard{})
	child.SetErr(io_discard{})
	return &child
}

type io_discard struct{}

func (io_discard) Write(p []byte) (int, error) { return len(p), nil }

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
