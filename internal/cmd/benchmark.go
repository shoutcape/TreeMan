package cmd

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
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
		return runBenchmarkIterations(cmd, target, warmup, runs, func(cmd *cobra.Command) (benchmarkIteration, error) {
			return benchmarkIteration{}, runList(cmd, false)
		})
	case "branch":
		if argument == "" {
			return fmt.Errorf("benchmark target branch requires an exact remote branch name")
		}
		runner, closeSandbox, err := newBranchBenchmarkRunner(argument)
		if err != nil {
			return err
		}
		return runIsolatedBenchmark(cmd, target+" "+argument, warmup, runs, runner, closeSandbox)
	case "review":
		if argument == "" {
			return fmt.Errorf("benchmark target review requires a PR or MR number")
		}
		if _, err := validate.PRNumber(argument); err != nil {
			return fmt.Errorf("benchmark target review: %w", err)
		}
		runner, closeSandbox, err := newReviewBenchmarkRunner(argument)
		if err != nil {
			return err
		}
		return runIsolatedBenchmark(cmd, target+" "+argument, warmup, runs, runner, closeSandbox)
	case "branch-results":
		if argument != "" {
			return fmt.Errorf("benchmark target branch-results does not accept an argument")
		}
		return runBenchmarkIterations(cmd, target, warmup, runs, resultCountRunner(branchPickerResults))
	case "review-results":
		if argument != "" {
			return fmt.Errorf("benchmark target review-results does not accept an argument")
		}
		return runBenchmarkIterations(cmd, target, warmup, runs, resultCountRunner(reviewPickerResults))
	default:
		return fmt.Errorf("unknown benchmark target %q (available: list, branch, review, branch-results, review-results)", target)
	}
}

type benchmarkIteration struct {
	cleanup     func() error
	resultCount *int
}

type benchmarkRunner func(*cobra.Command) (benchmarkIteration, error)

type benchmarkResultRunner func(*cobra.Command) (int, error)

func resultCountRunner(runner benchmarkResultRunner) benchmarkRunner {
	return func(runCmd *cobra.Command) (benchmarkIteration, error) {
		count, err := runner(runCmd)
		return benchmarkIteration{resultCount: &count}, err
	}
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
		if iterationErr := finishBenchmarkIteration(err, iteration.cleanup); iterationErr != nil {
			return fmt.Errorf("run %d failed: %w", index+1, iterationErr)
		}
		durations = append(durations, duration)
		result := fmt.Sprintf("run %2d/%d  %s", index+1, runs, formatDuration(duration))
		if iteration.resultCount != nil {
			counts = append(counts, *iteration.resultCount)
			result += fmt.Sprintf("  %d results", *iteration.resultCount)
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
	return finishBenchmarkIteration(err, iteration.cleanup)
}

func finishBenchmarkIteration(runErr error, cleanup func() error) error {
	if cleanup == nil {
		return runErr
	}
	cleanupErr := cleanup()
	if cleanupErr != nil {
		cleanupErr = fmt.Errorf("cleanup failed: %w", cleanupErr)
	}
	return errors.Join(runErr, cleanupErr)
}

func newBranchBenchmarkRunner(branch string) (benchmarkRunner, func() error, error) {
	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return nil, nil, err
	}
	worktreePath := worktree.PathForBranch(mainRoot, branch)
	if git.BranchExists(branch) {
		return nil, nil, fmt.Errorf("cannot benchmark branch %q: it already exists locally", branch)
	}
	if _, err := os.Stat(worktreePath); err == nil {
		return nil, nil, fmt.Errorf("cannot benchmark branch %q: directory %q already exists", branch, worktreePath)
	} else if !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("cannot inspect benchmark worktree path %q: %w", worktreePath, err)
	}

	sandbox, err := newBenchmarkSandbox()
	if err != nil {
		return nil, nil, err
	}
	runner := func(runCmd *cobra.Command) (benchmarkIteration, error) {
		var iteration benchmarkIteration
		err := sandbox.run(func() error {
			created, err := createBranchWorktree(runCmd, branch)
			if created.worktree.Path != "" {
				iteration.cleanup = func() error { return git.RemoveCreatedWorktree(created.mainRoot, created.worktree) }
			}
			return err
		})
		return iteration, err
	}
	return runner, sandbox.close, nil
}

func newReviewBenchmarkRunner(prArg string) (benchmarkRunner, func() error, error) {
	sandbox, err := newBenchmarkSandbox()
	if err != nil {
		return nil, nil, err
	}
	runner := func(runCmd *cobra.Command) (benchmarkIteration, error) {
		var iteration benchmarkIteration
		err := sandbox.run(func() error {
			created, err := createReviewWorktree(runCmd, prArg)
			if created.worktree.Path != "" {
				iteration.cleanup = func() error { return git.RemoveCreatedWorktree(created.mainRoot, created.worktree) }
			}
			return err
		})
		return iteration, err
	}
	return runner, sandbox.close, nil
}

func runIsolatedBenchmark(cmd *cobra.Command, target string, warmup, runs int, runner benchmarkRunner, closeSandbox func() error) (err error) {
	defer func() { err = errors.Join(err, closeSandbox()) }()
	return runBenchmarkIterations(cmd, target, warmup, runs, runner)
}

type benchmarkSandbox struct {
	root string
	repo string
}

func newBenchmarkSandbox() (benchmarkSandbox, error) {
	remoteURL, err := git.OriginRemoteURL()
	if err != nil {
		return benchmarkSandbox{}, err
	}
	root, err := os.MkdirTemp("", "treeman-benchmark-")
	if err != nil {
		return benchmarkSandbox{}, fmt.Errorf("could not create benchmark sandbox: %w", err)
	}
	sandbox := benchmarkSandbox{root: root, repo: filepath.Join(root, "repo")}
	if err := git.Clone(remoteURL, sandbox.repo); err != nil {
		return benchmarkSandbox{}, errors.Join(err, sandbox.close())
	}
	return sandbox, nil
}

func (sandbox benchmarkSandbox) run(operation func() error) error {
	previous, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.Chdir(sandbox.repo); err != nil {
		return err
	}
	operationErr := operation()
	return errors.Join(operationErr, os.Chdir(previous))
}

func (sandbox benchmarkSandbox) close() error {
	if sandbox.root == "" {
		return nil
	}
	return os.RemoveAll(sandbox.root)
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
