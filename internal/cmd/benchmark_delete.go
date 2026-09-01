package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/shoutcape/treeman/internal/database"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/worktree"
	"github.com/spf13/cobra"
)

// deleteBenchmarkBranch is the fixed disposable branch every delete iteration
// creates and removes. It exists only inside the benchmark sandbox.
const deleteBenchmarkBranch = "treeman-benchmark-delete"

// newDeleteBenchmarkRunner measures a normal worktree deletion. Every
// iteration first creates and fully sets up a disposable treebranch; that
// happens in the runner, outside the clock, so only the deletion is measured.
func newDeleteBenchmarkRunner() (benchmarkRunner, benchmarkSandbox, error) {
	sandbox, err := newProjectBenchmarkSandbox()
	if err != nil {
		return nil, benchmarkSandbox{}, err
	}

	runner := func(runCmd *cobra.Command) (*benchmarkIteration, error) {
		created, err := prepareDeleteBenchmarkWorktree(runCmd, sandbox)
		if err != nil {
			return nil, err
		}
		return &benchmarkIteration{
			run: func() error {
				return sandbox.run(func() error {
					return runDeleteDirect(runCmd, created.Path, created.Branch, true, false)
				})
			},
			cleanup: func() error { return cleanupDeleteBenchmarkIteration(sandbox, created) },
		}, nil
	}
	return runner, sandbox, nil
}

// prepareDeleteBenchmarkWorktree creates the disposable treebranch and gives it
// the project setup a real worktree would get. Deleting a half-set-up worktree
// would not measure what the benchmark claims to, so any setup failure aborts
// the iteration after removing whatever was created.
func prepareDeleteBenchmarkWorktree(cmd *cobra.Command, sandbox benchmarkSandbox) (git.CreatedWorktree, error) {
	var created git.CreatedWorktree
	err := sandbox.run(func() error {
		defaultBranch, err := git.DetectDefaultBranch()
		if err != nil {
			return err
		}
		path := worktree.PathForBranch(sandbox.repo, deleteBenchmarkBranch)
		created, err = git.CreateWorktree(path, deleteBenchmarkBranch, "origin/"+defaultBranch)
		if err != nil {
			return err
		}

		var setupOutput bytes.Buffer
		summary := setupCreatedWorktree(&setupOutput, commandRenderer(cmd), sandbox.repo, created, creationSetupOptions{})
		failures := summary.failures()
		if len(failures) == 0 {
			// Setup can legitimately write to tracked files -- npm rewriting
			// package-lock.json is enough -- and the deletion being measured
			// refuses a dirty worktree without --force. Clearing that drift
			// here, outside the clock, keeps the timed path the same
			// non-forced deletion a user runs; passing --force instead would
			// also skip the merge check and measure a different path.
			return git.DiscardWorktreeChanges(created.Path)
		}
		setupErr := fmt.Errorf("benchmark worktree setup failed: %s", strings.Join(failures, "; "))
		if output := strings.TrimSpace(setupOutput.String()); output != "" {
			return fmt.Errorf("%w\n%s", setupErr, output)
		}
		return setupErr
	})
	if err == nil {
		return created, nil
	}
	if created.Path == "" {
		return git.CreatedWorktree{}, err
	}
	return git.CreatedWorktree{}, errors.Join(err, cleanupDeleteBenchmarkIteration(sandbox, created))
}

// cleanupDeleteBenchmarkIteration makes sure the iteration left nothing behind.
// A successful timed deletion already removed everything, so this is the
// recovery path for a failed one — which is why it goes through Git rather
// than through the delete command being measured.
func cleanupDeleteBenchmarkIteration(sandbox benchmarkSandbox, created git.CreatedWorktree) error {
	gitErr := removeDeleteBenchmarkRemains(sandbox.repo, created)
	// Drop the iteration's branch database. RetryPending finds it now that the
	// worktree and branch are gone, whether the timed deletion recorded the
	// drop as pending or never got that far.
	_, databaseErr := database.NewCleanupBatch().RetryPending(sandbox.repo)
	return errors.Join(gitErr, databaseErr)
}

// removeDeleteBenchmarkRemains removes whatever Git artifacts survived the
// iteration. The branch is the reliable signal: deletion removes the worktree
// first, so a surviving branch means RemoveCreatedWorktree still has both
// halves of its job to do, and a missing one means the iteration got at least
// that far. RemoveCreatedWorktree deletes the branch at the SHA captured when
// it was created and reports anything it could not remove, so no separate
// verification pass is needed — but it resolves that ref unconditionally, so
// calling it once the branch is gone would fail on an already-deleted ref.
func removeDeleteBenchmarkRemains(repo string, created git.CreatedWorktree) error {
	branchMissing, err := git.BranchMissing(repo, created.Branch)
	if err != nil {
		return err
	}
	if !branchMissing {
		return git.RemoveCreatedWorktree(repo, created)
	}

	entries, err := git.WorktreeListInDir(repo)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if samePath(entry.Path, created.Path) {
			return fmt.Errorf("benchmark worktree %q outlived its branch %q", created.Path, created.Branch)
		}
	}
	return nil
}
