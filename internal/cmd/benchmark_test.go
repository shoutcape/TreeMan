package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	assert.Equal(t, []string{"list", "branch", "review", "delete", "branch-results", "review-results"}, command.ValidArgs)
}

func TestBenchmarkValidatesTargetAndCounts(t *testing.T) {
	cmd := &cobra.Command{}

	require.EqualError(t, runBenchmark(cmd, benchmarkRequest{target: "unknown"}, 0, 1), `unknown benchmark target "unknown" (available: list, branch, review, delete, branch-results, review-results)`)
	require.EqualError(t, runBenchmark(cmd, benchmarkRequest{target: "list", argument: "target"}, 0, 1), "benchmark target list does not accept an argument")
	require.EqualError(t, runBenchmark(cmd, benchmarkRequest{target: "branch"}, 0, 1), "benchmark target branch requires an exact remote branch name")
	require.EqualError(t, runBenchmark(cmd, benchmarkRequest{target: "review"}, 0, 1), "benchmark target review requires a PR or MR number")
	require.EqualError(t, runBenchmark(cmd, benchmarkRequest{target: "review", argument: "invalid"}, 0, 1), "benchmark target review: PR/MR number must be numeric")
	require.EqualError(t, runBenchmark(cmd, benchmarkRequest{target: "delete", argument: "target"}, 0, 1), "benchmark target delete does not accept an argument")
	require.EqualError(t, runBenchmark(cmd, benchmarkRequest{target: "branch-results", argument: "target"}, 0, 1), "benchmark target branch-results does not accept an argument")
	require.EqualError(t, runBenchmark(cmd, benchmarkRequest{target: "review-results", argument: "target"}, 0, 1), "benchmark target review-results does not accept an argument")
	require.EqualError(t, runBenchmark(cmd, benchmarkRequest{target: "list", setupRequested: true}, 0, 1),
		"benchmark target list runs no project setup, so --skip-env, --skip-database, --skip-deps, --skip-hooks, --trust-hooks do not apply")
	require.EqualError(t, runBenchmark(cmd, benchmarkRequest{target: "list", setup: creationSetupOptions{trustHooks: true}, setupRequested: true}, 0, 1),
		"benchmark target list runs no project setup, so --skip-env, --skip-database, --skip-deps, --skip-hooks, --trust-hooks do not apply")
	require.EqualError(t, runBenchmark(cmd, benchmarkRequest{target: "list", setup: creationSetupOptions{skipHooks: true}, setupRequested: true}, 0, 1),
		"benchmark target list runs no project setup, so --skip-env, --skip-database, --skip-deps, --skip-hooks, --trust-hooks do not apply")
	require.EqualError(t, runBenchmark(cmd, benchmarkRequest{target: "branch", argument: "feature/test", setupRequested: true}, 0, 1),
		"benchmark target branch runs no project setup, so --skip-env, --skip-database, --skip-deps, --skip-hooks, --trust-hooks do not apply")
	require.EqualError(t, runBenchmark(cmd, benchmarkRequest{target: "list"}, 0, 0), "benchmark runs must be at least 1")
	require.EqualError(t, runBenchmark(cmd, benchmarkRequest{target: "list"}, -1, 1), "benchmark warmup cannot be negative")
}

func TestBenchmarkRejectsExplicitFalseSetupAndTrustFlags(t *testing.T) {
	for _, name := range []string{"skip-env", "skip-database", "skip-deps", "skip-hooks", "trust-hooks"} {
		t.Run(name, func(t *testing.T) {
			cmd := newBenchmarkCmd()
			cmd.SetArgs([]string{"list", "--" + name + "=false"})
			err := cmd.Execute()
			require.EqualError(t, err,
				"benchmark target list runs no project setup, so --skip-env, --skip-database, --skip-deps, --skip-hooks, --trust-hooks do not apply")
		})
	}
}

func TestBenchmarkReportsResultCounts(t *testing.T) {
	var stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&stderr)
	calls := 0

	target := benchmarkTarget{label: "branch-results", runner: resultCountRunner(func(*cobra.Command) (int, time.Duration, error) {
		calls++
		return calls, time.Duration(calls) * 100 * time.Millisecond, nil
	})}

	require.NoError(t, runBenchmarkIterations(cmd, target, 1, 2, time.Now))

	assert.Contains(t, stderr.String(), "run  1/2")
	assert.Contains(t, stderr.String(), "2 results")
	assert.Contains(t, stderr.String(), "3 results")
	assert.Contains(t, stderr.String(), "results: 2-3 (changed during benchmark)")
	assert.Contains(t, stderr.String(), "first 200.0 ms")
	assert.Contains(t, stderr.String(), "first result: mean 250.0 ms   min 200.0 ms   max 300.0 ms")
}

func TestBenchmarkRunsTargetAndSuppressesItsOutput(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	calls := 0
	runner := func(runCmd *cobra.Command) (measuredRun, error) {
		return func() (benchmarkMeasurement, error) {
			calls++
			fmt.Fprint(runCmd.OutOrStdout(), "target stdout")
			fmt.Fprint(runCmd.ErrOrStderr(), "target stderr")
			return benchmarkMeasurement{}, nil
		}, nil
	}

	require.NoError(t, runBenchmarkIterations(cmd, benchmarkTarget{label: "list", runner: runner}, 1, 2, time.Now))

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

func TestBenchmarkPreparesEveryIterationOutsideTiming(t *testing.T) {
	var stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&stderr)
	preparations := 0
	runs := 0

	// A clock the iteration itself advances, so the reported duration shows
	// exactly which phases the benchmark measured.
	now := time.Unix(0, 0)
	runner := func(*cobra.Command) (measuredRun, error) {
		preparations++
		now = now.Add(time.Hour)
		return func() (benchmarkMeasurement, error) {
			runs++
			now = now.Add(50 * time.Millisecond)
			return benchmarkMeasurement{}, nil
		}, nil
	}

	require.NoError(t, runBenchmarkIterations(cmd, benchmarkTarget{label: "delete", runner: runner}, 1, 2, func() time.Time { return now }))

	assert.Equal(t, 3, preparations, "every warmup and measured run prepares its own iteration")
	assert.Equal(t, 3, runs)
	// Each iteration spends an hour preparing and 50ms running, so a reported
	// duration of 50ms is proof that only the run step was measured.
	assert.Contains(t, stderr.String(), "run  1/2  50.0 ms")
	assert.Contains(t, stderr.String(), "run  2/2  50.0 ms")
	assert.Contains(t, stderr.String(), "mean:   50.0 ms")
	assert.NotContains(t, stderr.String(), "3600", "preparation must stay outside the clock")
}

func TestBenchmarkCleansUpEveryIteration(t *testing.T) {
	calls := 0
	cleanups := 0
	runner := func(*cobra.Command) (measuredRun, error) {
		calls++
		return func() (benchmarkMeasurement, error) {
			return benchmarkMeasurement{cleanup: func() error {
				cleanups++
				return nil
			}}, nil
		}, nil
	}

	require.NoError(t, runBenchmarkIterations(&cobra.Command{}, benchmarkTarget{label: "branch feature/test", runner: runner}, 1, 2, time.Now))

	assert.Equal(t, 3, calls)
	assert.Equal(t, 3, cleanups)
}

func TestBenchmarkCleansUpFailedIteration(t *testing.T) {
	cleanups := 0
	runner := func(*cobra.Command) (measuredRun, error) {
		return func() (benchmarkMeasurement, error) {
			return benchmarkMeasurement{cleanup: func() error {
				cleanups++
				return nil
			}}, errors.New("failed")
		}, nil
	}

	err := runBenchmarkIterations(&cobra.Command{}, benchmarkTarget{label: "branch feature/test", runner: runner}, 0, 1, time.Now)

	require.EqualError(t, err, "run 1 failed: failed")
	assert.Equal(t, 1, cleanups)
}

func TestBenchmarkReportsRunAndCleanupFailures(t *testing.T) {
	err := finishBenchmarkIteration(errors.New("target failed"), func() error {
		return errors.New("remove failed")
	})

	require.ErrorContains(t, err, "target failed")
	require.ErrorContains(t, err, "cleanup failed: remove failed")
}

func TestBenchmarkBranchRemovesEveryCreatedWorktree(t *testing.T) {
	repo := createRemoteRepoWithNestedModule(t)
	configureBenchmarkHook(t, repo)
	gitTest(t, repo, "checkout", "-b", "feature/benchmark-branch")
	gitTest(t, repo, "push", "-u", "origin", "feature/benchmark-branch")
	gitTest(t, repo, "checkout", "main")
	gitTest(t, repo, "branch", "-D", "feature/benchmark-branch")
	gitTest(t, repo, "update-ref", "-d", "refs/remotes/origin/feature/benchmark-branch")
	chdirForTest(t, repo)
	pathWithOnlyGit(t)

	require.NoError(t, runBenchmark(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), benchmarkRequest{target: "branch", argument: "feature/benchmark-branch"}, 1, 2))
	assert.NoFileExists(t, os.Getenv("BENCHMARK_HOOK_LOG"))

	gitTestFails(t, repo, "show-ref", "--verify", "refs/heads/feature/benchmark-branch")
	gitTestFails(t, repo, "show-ref", "--verify", "refs/remotes/origin/feature/benchmark-branch")
	assert.NoDirExists(t, filepath.Join(repo, ".worktrees", "feature-benchmark-branch"))
}

func TestBenchmarkReviewRemovesEveryCreatedWorktree(t *testing.T) {
	repo := createRemoteRepoWithNestedModule(t)
	configureBenchmarkHook(t, repo)
	gitTest(t, repo, "checkout", "-b", "feature/benchmark-review")
	gitTest(t, repo, "push", "-u", "origin", "feature/benchmark-review")
	gitTest(t, repo, "push", "origin", "feature/benchmark-review:refs/pull/1/head")
	gitTest(t, repo, "checkout", "main")
	gitTest(t, repo, "branch", "-D", "feature/benchmark-review")
	chdirForTest(t, repo)
	pathWithGitAndGH(t)
	t.Setenv("_TREEMAN_FORGE", "github")
	t.Setenv("_TREEMAN_GH_REPO", "owner/repo")

	fetchHeadPath := filepath.Join(repo, ".git", "FETCH_HEAD")
	require.NoError(t, os.WriteFile(fetchHeadPath, []byte("user state\n"), 0o644))
	require.NoError(t, runBenchmark(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), benchmarkRequest{target: "review", argument: "1"}, 1, 2))
	assert.NoFileExists(t, os.Getenv("BENCHMARK_HOOK_LOG"))

	gitTestFails(t, repo, "show-ref", "--verify", "refs/heads/feature/review")
	assert.NoDirExists(t, filepath.Join(repo, ".worktrees", "feature-review"))
	afterFetchHead, err := os.ReadFile(fetchHeadPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("user state\n"), afterFetchHead)
}

func configureBenchmarkHook(t *testing.T, repo string) {
	t.Helper()
	config := `[hooks]
post_create = [` + fmt.Sprintf("%q", `printf hook >> "$BENCHMARK_HOOK_LOG"`) + `]
`
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte(config), 0o644))
	gitTest(t, repo, "add", ".treeman.toml")
	gitTest(t, repo, "commit", "-m", "configure benchmark hook")
	gitTest(t, repo, "push", "origin", "main")
	stateHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(stateHome, "treeman"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(stateHome, "treeman", "hook-approvals.json"), []byte("{"), 0o600))
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("BENCHMARK_HOOK_LOG", filepath.Join(t.TempDir(), "hook.log"))
}

func TestBenchmarkReportsRunnerFailure(t *testing.T) {
	runner := func(*cobra.Command) (measuredRun, error) {
		return func() (benchmarkMeasurement, error) {
			return benchmarkMeasurement{}, errors.New("failed")
		}, nil
	}

	err := runBenchmarkIterations(&cobra.Command{}, benchmarkTarget{label: "list", runner: runner}, 1, 1, time.Now)

	require.EqualError(t, err, "warmup run 1 failed: failed")
}

func TestBenchmarkReportsPreparationFailure(t *testing.T) {
	runs := 0
	preparations := 0
	runner := func(*cobra.Command) (measuredRun, error) {
		preparations++
		if preparations > 1 {
			return nil, errors.New("setup failed")
		}
		return func() (benchmarkMeasurement, error) {
			runs++
			return benchmarkMeasurement{}, nil
		}, nil
	}

	err := runBenchmarkIterations(&cobra.Command{}, benchmarkTarget{label: "delete", runner: runner}, 0, 2, time.Now)

	require.EqualError(t, err, "run 2 failed: preparation failed: setup failed")
	assert.Equal(t, 1, runs, "an iteration that could not be prepared is never measured")
}

func TestSilentCommandPreservesContextAndInput(t *testing.T) {
	type contextKey struct{}
	input := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetContext(context.WithValue(context.Background(), contextKey{}, "value"))
	cmd.SetIn(input)

	silenced := silentCommand(cmd)

	assert.Equal(t, "value", silenced.Context().Value(contextKey{}))
	assert.Same(t, input, silenced.InOrStdin())
	assert.Equal(t, io.Discard, silenced.OutOrStdout())
	assert.Equal(t, io.Discard, silenced.ErrOrStderr())
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

func TestFirstRowWriterTimesTheRowAfterTheHeader(t *testing.T) {
	clock := time.Unix(0, 0)
	out := &bytes.Buffer{}
	writer := newFirstRowWriter(out, func() time.Time { return clock })

	clock = clock.Add(2 * time.Second)
	_, err := writer.Write([]byte("HEADER\n"))
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), writer.firstRow(), "the header is not a result")

	clock = clock.Add(3 * time.Second)
	_, err = writer.Write([]byte("row one\n"))
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, writer.firstRow())

	clock = clock.Add(10 * time.Second)
	_, err = writer.Write([]byte("row two\n"))
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, writer.firstRow(), "later rows must not overwrite the first")

	assert.Equal(t, "HEADER\nrow one\nrow two\n", out.String())
}

func TestFirstRowWriterReportsNothingWithoutRows(t *testing.T) {
	writer := newFirstRowWriter(io.Discard, time.Now)

	_, err := writer.Write([]byte("HEADER\n"))
	require.NoError(t, err)

	assert.Equal(t, time.Duration(0), writer.firstRow())
}

func TestFormatBenchmarkFirstResults(t *testing.T) {
	assert.Equal(t, "first result: unavailable", formatBenchmarkFirstResults(nil))
	assert.Equal(t, "first result: mean 500.0 ms   min 400.0 ms   max 600.0 ms",
		formatBenchmarkFirstResults([]time.Duration{400 * time.Millisecond, 500 * time.Millisecond, 600 * time.Millisecond}))
}
