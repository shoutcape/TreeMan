package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/shoutcape/treeman/internal/database"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/spf13/cobra"
)

// deleteBenchmarkBranch is the fixed disposable branch every delete iteration
// creates and removes. It exists only inside the benchmark sandbox.
const deleteBenchmarkBranch = "treeman-benchmark-delete"

// newDeleteBenchmarkRunner measures a normal worktree deletion. Every
// iteration first creates and sets up a disposable treebranch; that happens in
// the runner, outside the clock, so only the deletion is measured. The setup
// options are the caller's: a project whose dependency install, database, or
// hooks cannot run on this machine is still worth benchmarking without them,
// as long as the report says which steps ran.
func newDeleteBenchmarkRunner(setup creationSetupOptions, consent *cobra.Command) (benchmarkRunner, benchmarkSandbox, error) {
	sandbox, err := newProjectBenchmarkSandbox()
	if err != nil {
		return nil, benchmarkSandbox{}, err
	}

	runner := func(runCmd *cobra.Command) (measuredRun, error) {
		// Consent uses the original terminal; measured execution stays silenced.
		created, preparation, err := prepareDeleteBenchmarkWorktree(consent, sandbox, setup)
		if err != nil {
			return nil, err
		}
		return func() (benchmarkMeasurement, error) {
			err := sandbox.run(func() error {
				return runDeleteDirect(runCmd, created.Path, created.Branch, true, false)
			})
			return benchmarkMeasurement{
				preparation: preparation,
				cleanup:     func() error { return cleanupDeleteBenchmarkIteration(sandbox, created) },
			}, err
		}, nil
	}
	return runner, sandbox, nil
}

// prepareDeleteBenchmarkWorktree creates the disposable treebranch and gives it
// the project setup a real worktree would get. Deleting a half-set-up worktree
// would not measure what the benchmark claims to, so a setup step that fails
// when it was asked to run aborts the iteration after removing whatever was
// created. It returns a description of what the timed deletion is handed.
func prepareDeleteBenchmarkWorktree(cmd *cobra.Command, sandbox benchmarkSandbox, setup creationSetupOptions) (git.CreatedWorktree, string, error) {
	var created git.CreatedWorktree
	preparation := ""
	err := sandbox.run(func() error {
		defaultBranch, err := git.DetectDefaultBranch()
		if err != nil {
			return err
		}
		// The fixture goes through production path validation but stays under
		// the sandbox, which owns every disposable benchmark artifact.
		paths, err := prepareApprovedCreationPaths(cmd, sandbox.repo, deleteBenchmarkBranch, sandbox.worktreeDir(), setup)
		if err != nil {
			return err
		}
		created, err = git.CreatePlannedWorktree(paths.plan(deleteBenchmarkBranch), deleteBenchmarkBranch, "origin/"+defaultBranch)
		if err != nil {
			return err
		}

		var setupOutput bytes.Buffer
		summary := setupCreatedWorktree(&setupOutput, commandRenderer(cmd), paths, created, setup)
		if failures := summary.failures(); len(failures) > 0 {
			setupErr := fmt.Errorf("benchmark worktree setup failed: %s (rerun with %s to measure deletion without that step)",
				strings.Join(failures, "; "), strings.Join(failedSetupFlags(summary), " "))
			if output := strings.TrimSpace(setupOutput.String()); output != "" {
				return fmt.Errorf("%w\n%s", setupErr, output)
			}
			return setupErr
		}

		// Setup can legitimately write to tracked files -- npm rewriting
		// package-lock.json is enough -- and the deletion being measured
		// refuses a dirty worktree without --force. Clearing that drift here,
		// outside the clock, keeps the timed path the same non-forced deletion
		// a user runs; passing --force instead would waive the clean check the
		// timed path is supposed to be running.
		cleared, err := git.DiscardWorktreeChanges(created.Path)
		if err != nil {
			return err
		}
		preparation = describeDeletePreparation(summary, cleared)
		return nil
	})
	if err == nil {
		return created, preparation, nil
	}
	if created.Path == "" {
		return git.CreatedWorktree{}, "", err
	}
	return git.CreatedWorktree{}, "", errors.Join(err, cleanupDeleteBenchmarkIteration(sandbox, created))
}

// describeDeletePreparation says what the timed deletion was handed. What a
// deletion costs is decided here -- an installed dependency tree and a branch
// database are most of the work -- so a number only means something next to
// another one prepared the same way.
func describeDeletePreparation(summary setupSummary, cleared []string) string {
	steps := summary.steps()
	parts := make([]string, 0, len(steps)+1)
	for _, step := range steps {
		parts = append(parts, strings.ToLower(step.name)+" "+setupStatusWord(step.status.kind))
	}
	if len(cleared) > 0 {
		// The clean that keeps the worktree deletable also removed setup
		// output this project does not ignore, so the timed deletion has less
		// to remove than the same deletion would in a project that ignores it.
		parts = append(parts, fmt.Sprintf("cleared %d untracked setup path(s)", len(cleared)))
	}
	return strings.Join(parts, ", ")
}

// failedSetupFlags names the flags that skip the steps that failed, so an
// abort tells the caller how to benchmark this project anyway.
func failedSetupFlags(summary setupSummary) []string {
	var flags []string
	for _, step := range summary.steps() {
		if step.status.kind == setupStatusFailed {
			flags = append(flags, "--"+step.skipFlag)
		}
	}
	return flags
}

// cleanupDeleteBenchmarkIteration makes sure the iteration left nothing behind.
// A successful timed deletion already removed everything, so this is the
// recovery path for a failed one — which is why it goes through Git rather
// than through the delete command being measured.
func cleanupDeleteBenchmarkIteration(sandbox benchmarkSandbox, created git.CreatedWorktree) error {
	// RemoveCreatedWorktree deletes the branch at the SHA captured when it was
	// created, reports anything it could not remove, and treats artifacts the
	// timed deletion already removed as nothing left to do.
	gitErr := git.RemoveCreatedWorktree(sandbox.repo, created)
	// Drop the iteration's branch database. RetryPending finds it now that the
	// worktree and branch are gone, whether the timed deletion recorded the
	// drop as pending or never got that far.
	_, databaseErr := database.NewCleanupBatch().RetryPending(sandbox.repo)
	return errors.Join(gitErr, databaseErr)
}
