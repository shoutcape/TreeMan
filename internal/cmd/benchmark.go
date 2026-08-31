package cmd

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/shoutcape/treeman/internal/envfile"
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
		ValidArgs: []string{"list", "branch", "review", "delete", "branch-results", "review-results"},
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

func runBenchmark(cmd *cobra.Command, target, argument string, warmup, runs int) (err error) {
	resolved, err := newBenchmarkTarget(target, argument)
	// Registered before the error check so a sandbox always gets removed, even
	// if a future target starts returning one alongside an error.
	defer func() { err = errors.Join(err, resolved.sandbox.close()) }()
	if err != nil {
		return err
	}
	return runBenchmarkIterations(cmd, resolved, warmup, runs, time.Now)
}

// benchmarkTarget is one resolved benchmark: the label reported in its header,
// the runner that produces each iteration, and the sandbox those iterations
// run in. A zero sandbox means the target measures the current repository in
// place, and closing it is a no-op.
type benchmarkTarget struct {
	label   string
	runner  benchmarkRunner
	sandbox benchmarkSandbox
}

// newBenchmarkTarget validates a target and its argument and builds the
// benchmark for it. On failure nothing is left open: the sandbox constructors
// close their own sandbox before returning an error.
func newBenchmarkTarget(target, argument string) (benchmarkTarget, error) {
	rejectArgument := func() error {
		if argument != "" {
			return fmt.Errorf("benchmark target %s does not accept an argument", target)
		}
		return nil
	}

	switch target {
	case "list":
		if err := rejectArgument(); err != nil {
			return benchmarkTarget{}, err
		}
		runner := func(cmd *cobra.Command) (*benchmarkIteration, error) {
			return &benchmarkIteration{run: func() error { return runList(cmd, false) }}, nil
		}
		return benchmarkTarget{label: target, runner: runner}, nil
	case "branch":
		if argument == "" {
			return benchmarkTarget{}, fmt.Errorf("benchmark target branch requires an exact remote branch name")
		}
		runner, sandbox, err := newBranchBenchmarkRunner(argument)
		if err != nil {
			return benchmarkTarget{}, err
		}
		return benchmarkTarget{label: target + " " + argument, runner: runner, sandbox: sandbox}, nil
	case "review":
		if argument == "" {
			return benchmarkTarget{}, fmt.Errorf("benchmark target review requires a PR or MR number")
		}
		if _, err := validate.PRNumber(argument); err != nil {
			return benchmarkTarget{}, fmt.Errorf("benchmark target review: %w", err)
		}
		runner, sandbox, err := newReviewBenchmarkRunner(argument)
		if err != nil {
			return benchmarkTarget{}, err
		}
		return benchmarkTarget{label: target + " " + argument, runner: runner, sandbox: sandbox}, nil
	case "delete":
		if err := rejectArgument(); err != nil {
			return benchmarkTarget{}, err
		}
		runner, sandbox, err := newDeleteBenchmarkRunner()
		if err != nil {
			return benchmarkTarget{}, err
		}
		return benchmarkTarget{label: target, runner: runner, sandbox: sandbox}, nil
	case "branch-results":
		if err := rejectArgument(); err != nil {
			return benchmarkTarget{}, err
		}
		return benchmarkTarget{label: target, runner: resultCountRunner(branchPickerResults)}, nil
	case "review-results":
		if err := rejectArgument(); err != nil {
			return benchmarkTarget{}, err
		}
		return benchmarkTarget{label: target, runner: resultCountRunner(reviewPickerResults)}, nil
	default:
		return benchmarkTarget{}, fmt.Errorf("unknown benchmark target %q (available: list, branch, review, delete, branch-results, review-results)", target)
	}
}

// benchmarkIteration is one warmup or measured pass. Only run is timed, so
// whatever a target has to set up per iteration belongs in the runner that
// builds this. run may fill in the remaining fields as it discovers them: the
// worktree cleanup has to remove is only known once the run has created it.
type benchmarkIteration struct {
	run         func() error
	cleanup     func() error
	resultCount *int
	firstResult *time.Duration
}

// benchmarkRunner builds the next iteration. It is called outside the clock.
type benchmarkRunner func(*cobra.Command) (*benchmarkIteration, error)

type benchmarkResultRunner func(*cobra.Command) (int, time.Duration, error)

func resultCountRunner(runner benchmarkResultRunner) benchmarkRunner {
	return func(runCmd *cobra.Command) (*benchmarkIteration, error) {
		iteration := &benchmarkIteration{}
		iteration.run = func() error {
			count, firstResult, err := runner(runCmd)
			iteration.resultCount = &count
			iteration.firstResult = &firstResult
			return err
		}
		return iteration, nil
	}
}

// firstRowWriter records how long the first picker row took to reach the
// consumer. A streaming picker is judged by that, not by how long the whole
// list takes: the header is written before any result is known, so only the
// write after it counts.
//
// The benchmark targets run no fzf, so what this times is when the producer
// had a row ready, not when a picker painted it.
type firstRowWriter struct {
	out    io.Writer
	now    func() time.Time
	start  time.Time
	writes int
	first  time.Duration
}

func newFirstRowWriter(out io.Writer, now func() time.Time) *firstRowWriter {
	return &firstRowWriter{out: out, now: now, start: now()}
}

// newPickerResultsWriter starts the clock the picker-results targets are
// judged by. Both targets take it from here — once the command knows it is in
// a repository, before any forge work — so their numbers are comparable
// rather than each timing from wherever its own preconditions happened to
// end.
func newPickerResultsWriter() *firstRowWriter {
	return newFirstRowWriter(io.Discard, time.Now)
}

func (writer *firstRowWriter) Write(payload []byte) (int, error) {
	writer.writes++
	if writer.writes == 2 {
		writer.first = writer.now().Sub(writer.start)
	}
	return writer.out.Write(payload)
}

func (writer *firstRowWriter) firstRow() time.Duration {
	return writer.first
}

func formatBenchmarkFirstResults(durations []time.Duration) string {
	if len(durations) == 0 {
		return "first result: unavailable"
	}
	mean, _, minDuration, maxDuration := calcStats(durations)
	return fmt.Sprintf("first result: mean %s   min %s   max %s",
		formatDuration(mean), formatDuration(minDuration), formatDuration(maxDuration))
}

func runBenchmarkIterations(cmd *cobra.Command, target benchmarkTarget, warmup, runs int, now func() time.Time) error {
	if err := validateBenchmarkCounts(warmup, runs); err != nil {
		return err
	}

	render := commandRenderer(cmd)
	errOut := cmd.ErrOrStderr()
	fmt.Fprintf(errOut, "\n%s\n\n", render.Title("BENCHMARK"))
	fmt.Fprintf(errOut, "  %s\n\n", render.Muted(fmt.Sprintf("command: %s   warmup: %d   runs: %d", target.label, warmup, runs)))

	silenced := silentCommand(cmd)
	for index := range warmup {
		fmt.Fprintf(errOut, "  %s\n", render.Status(ui.ToneMuted, "~", fmt.Sprintf("warmup %d/%d", index+1, warmup)))
		if _, _, err := runBenchmarkIteration(silenced, target.runner, now); err != nil {
			return fmt.Errorf("warmup run %d failed: %w", index+1, err)
		}
	}
	if warmup > 0 {
		fmt.Fprintln(errOut)
	}

	durations := make([]time.Duration, 0, runs)
	counts := make([]int, 0, runs)
	firstResults := make([]time.Duration, 0, runs)
	for index := range runs {
		iteration, duration, err := runBenchmarkIteration(silenced, target.runner, now)
		if err != nil {
			return fmt.Errorf("run %d failed: %w", index+1, err)
		}
		durations = append(durations, duration)
		result := fmt.Sprintf("run %2d/%d  %s", index+1, runs, formatDuration(duration))
		if iteration.resultCount != nil {
			counts = append(counts, *iteration.resultCount)
			result += fmt.Sprintf("  %d results", *iteration.resultCount)
		}
		if iteration.firstResult != nil && *iteration.firstResult > 0 {
			firstResults = append(firstResults, *iteration.firstResult)
			result += fmt.Sprintf("  first %s", formatDuration(*iteration.firstResult))
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
	if len(firstResults) > 0 {
		fmt.Fprintf(errOut, "  %s\n", render.Muted(formatBenchmarkFirstResults(firstResults)))
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

// runBenchmarkIteration prepares, runs, and tears down one iteration. Only the
// run step is measured: preparation and cleanup stay outside the returned
// duration.
func runBenchmarkIteration(cmd *cobra.Command, runner benchmarkRunner, now func() time.Time) (*benchmarkIteration, time.Duration, error) {
	iteration, err := runner(cmd)
	if err != nil {
		return nil, 0, fmt.Errorf("preparation failed: %w", err)
	}
	if iteration == nil || iteration.run == nil {
		return nil, 0, fmt.Errorf("benchmark target prepared no operation to measure")
	}
	start := now()
	runErr := iteration.run()
	duration := now().Sub(start)
	return iteration, duration, finishBenchmarkIteration(runErr, iteration.cleanup)
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

func newBranchBenchmarkRunner(branch string) (benchmarkRunner, benchmarkSandbox, error) {
	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return nil, benchmarkSandbox{}, err
	}
	worktreePath := worktree.PathForBranch(mainRoot, branch)
	if git.BranchExists(branch) {
		return nil, benchmarkSandbox{}, fmt.Errorf("cannot benchmark branch %q: it already exists locally", branch)
	}
	if _, err := os.Stat(worktreePath); err == nil {
		return nil, benchmarkSandbox{}, fmt.Errorf("cannot benchmark branch %q: directory %q already exists", branch, worktreePath)
	} else if !os.IsNotExist(err) {
		return nil, benchmarkSandbox{}, fmt.Errorf("cannot inspect benchmark worktree path %q: %w", worktreePath, err)
	}

	sandbox, err := newBenchmarkSandbox()
	if err != nil {
		return nil, benchmarkSandbox{}, err
	}
	runner := func(runCmd *cobra.Command) (*benchmarkIteration, error) {
		iteration := &benchmarkIteration{}
		iteration.run = func() error {
			return sandbox.run(func() error {
				created, err := createBranchWorktree(runCmd, branch)
				if created.worktree.Path != "" {
					iteration.cleanup = func() error { return git.RemoveCreatedWorktree(created.mainRoot, created.worktree) }
				}
				return err
			})
		}
		return iteration, nil
	}
	return runner, sandbox, nil
}

func newReviewBenchmarkRunner(prArg string) (benchmarkRunner, benchmarkSandbox, error) {
	sandbox, err := newBenchmarkSandbox()
	if err != nil {
		return nil, benchmarkSandbox{}, err
	}
	runner := func(runCmd *cobra.Command) (*benchmarkIteration, error) {
		iteration := &benchmarkIteration{}
		iteration.run = func() error {
			return sandbox.run(func() error {
				created, err := createReviewWorktree(runCmd, prArg)
				if created.worktree.Path != "" {
					iteration.cleanup = func() error { return git.RemoveCreatedWorktree(created.mainRoot, created.worktree) }
				}
				return err
			})
		}
		return iteration, nil
	}
	return runner, sandbox, nil
}

type benchmarkSandbox struct {
	root string
	repo string
}

// newBenchmarkSandbox clones the current repository into a temporary directory
// so iterations never touch the user's checkout. The clone has no working
// tree; targets that need the project's files on disk use
// newProjectBenchmarkSandbox instead.
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

// newProjectBenchmarkSandbox additionally gives the clone a working tree and
// the source repository's environment files. Targets that run project setup
// need both: the setup steps read .treeman.toml and .env* from the repository
// root they are given.
func newProjectBenchmarkSandbox() (benchmarkSandbox, error) {
	sourceRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return benchmarkSandbox{}, err
	}
	sandbox, err := newBenchmarkSandbox()
	if err != nil {
		return benchmarkSandbox{}, err
	}
	if err := git.CheckOutHead(sandbox.repo); err != nil {
		return benchmarkSandbox{}, errors.Join(err, sandbox.close())
	}
	if _, err := envfile.Copy(sourceRoot, sandbox.repo); err != nil {
		return benchmarkSandbox{}, errors.Join(
			fmt.Errorf("could not copy environment files into benchmark sandbox: %w", err), sandbox.close())
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
