package cmd

import (
	"fmt"
	"io"
	"math"
	"os"
	"time"

	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/shoutcape/treeman/internal/validate"
	"github.com/shoutcape/treeman/internal/worktree"
	"github.com/spf13/cobra"
)

func newBenchmarkCmd() *cobra.Command {
	var runs int
	var warmup int
	cmd := &cobra.Command{
		Use:       "benchmark [command] [target]",
		Short:     "Measure execution time of a treeman command",
		Args:      cobra.RangeArgs(0, 2),
		ValidArgs: []string{"list", "branch", "review", "branch-results", "review-results"},
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "list"
			if len(args) == 1 {
				target = args[0]
			}
			argument := ""
			if len(args) == 2 {
				target = args[0]
				argument = args[1]
			}
			return runBenchmark(cmd, target, argument, warmup, runs)
		},
	}
	cmd.Flags().IntVar(&runs, "runs", 10, "Number of timed runs")
	cmd.Flags().IntVar(&warmup, "warmup", 3, "Number of warmup runs")
	return cmd
}

func runBenchmark(cmd *cobra.Command, target, argument string, warmup, runs int) error {
	switch target {
	case "list":
		if argument != "" {
			return fmt.Errorf("benchmark target list does not accept an argument")
		}
		return runBenchmarkWithRunner(cmd, target, warmup, runs, func(cmd *cobra.Command) error { return runList(cmd, false) })
	case "branch":
		if argument == "" {
			return fmt.Errorf("benchmark target branch requires an exact remote branch name")
		}
		runner, err := newBranchBenchmarkRunner(argument)
		if err != nil {
			return err
		}
		return runBenchmarkWithCleanupRunner(cmd, target+" "+argument, warmup, runs, runner)
	case "review":
		if argument == "" {
			return fmt.Errorf("benchmark target review requires a PR or MR number")
		}
		if _, err := validate.PRNumber(argument); err != nil {
			return fmt.Errorf("benchmark target review: %w", err)
		}
		return runBenchmarkWithCleanupRunner(cmd, target+" "+argument, warmup, runs, newReviewBenchmarkRunner(argument))
	case "branch-results":
		if argument != "" {
			return fmt.Errorf("benchmark target branch-results does not accept an argument")
		}
		return runBenchmarkWithResultCount(cmd, target, warmup, runs, branchPickerResults)
	case "review-results":
		if argument != "" {
			return fmt.Errorf("benchmark target review-results does not accept an argument")
		}
		return runBenchmarkWithResultCount(cmd, target, warmup, runs, reviewPickerResults)
	default:
		return fmt.Errorf("unknown benchmark target %q (available: list, branch, review, branch-results, review-results)", target)
	}
}

func runBenchmarkWithRunner(cmd *cobra.Command, target string, warmup, runs int, runner func(*cobra.Command) error) error {
	return runBenchmarkWithCleanupRunner(cmd, target, warmup, runs, func(runCmd *cobra.Command) (func() error, error) {
		return nil, runner(runCmd)
	})
}

type benchmarkCleanupRunner func(*cobra.Command) (cleanup func() error, err error)

type benchmarkIteration struct {
	cleanup        func() error
	resultCount    int
	hasResultCount bool
}

type benchmarkRunner func(*cobra.Command) (benchmarkIteration, error)

func runBenchmarkWithCleanupRunner(cmd *cobra.Command, target string, warmup, runs int, runner benchmarkCleanupRunner) error {
	return runBenchmarkIterations(cmd, target, warmup, runs, func(runCmd *cobra.Command) (benchmarkIteration, error) {
		cleanup, err := runner(runCmd)
		return benchmarkIteration{cleanup: cleanup}, err
	})
}

type benchmarkResultRunner func(*cobra.Command) (int, error)

func runBenchmarkWithResultCount(cmd *cobra.Command, target string, warmup, runs int, runner benchmarkResultRunner) error {
	return runBenchmarkIterations(cmd, target, warmup, runs, func(runCmd *cobra.Command) (benchmarkIteration, error) {
		count, err := runner(runCmd)
		return benchmarkIteration{resultCount: count, hasResultCount: true}, err
	})
}

func runBenchmarkIterations(cmd *cobra.Command, target string, warmup, runs int, runner benchmarkRunner) error {
	if err := validateBenchmarkCounts(warmup, runs); err != nil {
		return err
	}

	render := commandRenderer(cmd)
	errOut := cmd.ErrOrStderr()
	fmt.Fprintf(errOut, "\n%s\n\n", render.Title("BENCHMARK"))
	fmt.Fprintf(errOut, "  %s\n\n", render.Muted(fmt.Sprintf("command: %s   warmup: %d   runs: %d", target, warmup, runs)))

	silenced := silentCommand(cmd)
	for index := range warmup {
		fmt.Fprintf(errOut, "  %s\n", render.Status(ui.ToneMuted, "~", fmt.Sprintf("warmup %d/%d", index+1, warmup)))
		if err := runBenchmarkIteration(silenced, runner); err != nil {
			return fmt.Errorf("warmup run %d failed: %w", index+1, err)
		}
	}
	if warmup > 0 {
		fmt.Fprintln(errOut)
	}

	durations := make([]time.Duration, 0, runs)
	counts := make([]int, 0, runs)
	for index := range runs {
		start := time.Now()
		iteration, err := runner(silenced)
		duration := time.Since(start)
		if cleanupErr := cleanupBenchmarkRun(iteration.cleanup); cleanupErr != nil {
			return fmt.Errorf("run %d cleanup failed: %w", index+1, cleanupErr)
		}
		if err != nil {
			return fmt.Errorf("run %d failed: %w", index+1, err)
		}
		durations = append(durations, duration)
		result := fmt.Sprintf("run %2d/%d  %s", index+1, runs, formatDuration(duration))
		if iteration.hasResultCount {
			counts = append(counts, iteration.resultCount)
			result += fmt.Sprintf("  %d results", iteration.resultCount)
		}
		fmt.Fprintf(errOut, "  %s\n", render.Status(ui.ToneInfo, "->", result))
	}

	mean, stddev, minDuration, maxDuration := calcStats(durations)
	fmt.Fprintf(errOut, "\n  %s\n", render.Header("RESULTS"))
	fmt.Fprintf(errOut, "  %s\n", render.Muted(fmt.Sprintf("mean:   %s", formatDuration(mean))))
	fmt.Fprintf(errOut, "  %s\n", render.Muted(fmt.Sprintf("stddev: %s", formatDuration(stddev))))
	fmt.Fprintf(errOut, "  %s\n", render.Muted(fmt.Sprintf("min:    %s", formatDuration(minDuration))))
	fmt.Fprintf(errOut, "  %s\n", render.Muted(fmt.Sprintf("max:    %s", formatDuration(maxDuration))))
	if len(counts) > 0 {
		fmt.Fprintf(errOut, "  %s\n", render.Muted(formatBenchmarkResultCounts(counts)))
	}
	fmt.Fprintln(errOut)
	return nil
}

func validateBenchmarkCounts(warmup, runs int) error {
	if runs < 1 {
		return fmt.Errorf("benchmark runs must be at least 1")
	}
	if warmup < 0 {
		return fmt.Errorf("benchmark warmup cannot be negative")
	}
	return nil
}

func formatBenchmarkResultCounts(counts []int) string {
	if len(counts) == 0 {
		return "results: unavailable"
	}
	minCount, maxCount := counts[0], counts[0]
	for _, count := range counts[1:] {
		minCount = min(minCount, count)
		maxCount = max(maxCount, count)
	}
	if minCount == maxCount {
		return fmt.Sprintf("results: %d", minCount)
	}
	return fmt.Sprintf("results: %d-%d (changed during benchmark)", minCount, maxCount)
}

func runBenchmarkIteration(cmd *cobra.Command, runner benchmarkRunner) error {
	iteration, err := runner(cmd)
	if cleanupErr := cleanupBenchmarkRun(iteration.cleanup); cleanupErr != nil {
		return fmt.Errorf("cleanup failed: %w", cleanupErr)
	}
	return err
}

func cleanupBenchmarkRun(cleanup func() error) error {
	if cleanup == nil {
		return nil
	}
	return cleanup()
}

func newBranchBenchmarkRunner(branch string) (benchmarkCleanupRunner, error) {
	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return nil, err
	}
	worktreePath := worktree.PathForBranch(mainRoot, branch)
	if git.BranchExists(branch) {
		return nil, fmt.Errorf("cannot benchmark branch %q: it already exists locally", branch)
	}
	if _, err := os.Stat(worktreePath); err == nil {
		return nil, fmt.Errorf("cannot benchmark branch %q: directory %q already exists", branch, worktreePath)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("cannot inspect benchmark worktree path %q: %w", worktreePath, err)
	}

	return func(runCmd *cobra.Command) (func() error, error) {
		created, err := runBranchWithResult(runCmd, branch, creationSetupOptions{skipSetup: true})
		if created.path == "" {
			return nil, err
		}
		return func() error { return cleanupBenchmarkWorktree(created.mainRoot, created.path, created.branch) }, err
	}, nil
}

func newReviewBenchmarkRunner(prArg string) benchmarkCleanupRunner {
	return func(runCmd *cobra.Command) (func() error, error) {
		created, err := runReviewWithResult(runCmd, prArg, creationSetupOptions{skipSetup: true})
		if created.path == "" {
			return nil, err
		}
		return func() error { return cleanupBenchmarkWorktree(created.mainRoot, created.path, created.branch) }, err
	}
}

func cleanupBenchmarkWorktree(mainRoot, worktreePath, branch string) error {
	sha := ""
	if git.BranchExists(branch) {
		var err error
		sha, err = git.BranchSHA(branch)
		if err != nil {
			return err
		}
	}

	if _, err := os.Stat(worktreePath); err == nil {
		if err := git.WorktreeRemove(worktreePath, true); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("cannot inspect benchmark worktree path %q: %w", worktreePath, err)
	}

	if sha == "" {
		return nil
	}
	if err := git.DeleteBranchAtSHA(mainRoot, branch, sha); err != nil {
		return err
	}
	return nil
}

func silentCommand(cmd *cobra.Command) *cobra.Command {
	silenced := &cobra.Command{}
	silenced.SetContext(cmd.Context())
	silenced.SetIn(cmd.InOrStdin())
	silenced.SetOut(io.Discard)
	silenced.SetErr(io.Discard)
	return silenced
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
