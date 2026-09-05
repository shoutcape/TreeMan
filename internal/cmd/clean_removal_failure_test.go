package cmd

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"testing"

	gitpkg "github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/merge"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A batch removal continues only on a candidate the removal itself reports as
// unchanged. Every other failure -- and every failure that classifies itself as
// nothing in particular -- has to stop the run, so these exercise what `clean`
// does with each answer the removal can give.

// twoCleanCandidates gives a repository with two merged, clean worktrees, so a
// batch has a second candidate to reach -- or to be stopped before reaching.
func twoCleanCandidates(t *testing.T) (string, string, string, merge.ClassifierFunc) {
	t.Helper()
	repo, first := createMergedCleanWorktree(t)
	runGitInDir(t, repo, "checkout", "-b", "second")
	runGitInDir(t, repo, "push", "-u", "origin", "second")
	runGitInDir(t, repo, "checkout", "main")
	second := filepath.Join(t.TempDir(), "second-worktree")
	runGitInDir(t, repo, "worktree", "add", second, "second")
	changeToDir(t, repo)
	classifier := merge.ClassifierFunc(func(_ string, branches []string) (merge.Result, error) {
		cleanable := make([]merge.Candidate, 0, len(branches))
		for _, branch := range branches {
			cleanable = append(cleanable, merge.Candidate{Branch: branch, SHA: gitRevParse(t, repo, "refs/heads/"+branch)})
		}
		return merge.Result{Cleanable: cleanable}, nil
	})
	return repo, first, second, classifier
}

func TestCleanContinuesAfterARefusedRemoval(t *testing.T) {
	repo, refusedWorktree, removableWorktree, classifier := twoCleanCandidates(t)

	previousRemove := removeWorktreeAndBranch
	removeWorktreeAndBranch = func(mainRoot, path, branch, expectedSHA string, force bool) (gitpkg.RemoveWorktreeResult, error) {
		if branch == "feature" {
			return gitpkg.RemoveWorktreeResult{}, &gitpkg.RemovalError{Scope: gitpkg.RemovalScopeCandidate, Err: assert.AnError}
		}
		return previousRemove(mainRoot, path, branch, expectedSHA, force)
	}
	t.Cleanup(func() { removeWorktreeAndBranch = previousRemove })

	stderr := &bytes.Buffer{}
	command := &cobra.Command{}
	command.SetErr(stderr)

	err := runCleanWithClassifier(command, classifier, false, true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 worktree(s) could not be removed: feature")
	output := ui.StripANSI(stderr.String())
	assert.Contains(t, output, `skipping "feature"`)
	assert.Contains(t, output, "Removed 1 merged, clean worktree(s).")
	// The refused candidate keeps both Git resources; the later one is removed.
	assert.DirExists(t, refusedWorktree)
	runGitInDir(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/feature")
	assert.NoDirExists(t, removableWorktree)
	assert.Error(t, exec.Command("git", "-C", repo, "show-ref", "--verify", "--quiet", "refs/heads/second").Run())
}

// A removal that captured the worktree directory and could not put it back
// leaves the registration intact, so nothing about which Git resources survived
// tells it apart from a refusal. Only the removal's own classification does,
// and the batch has to stop on it: the worktree is not where Git says it is.
func TestCleanStopsWhenACaptureCouldNotBeRestored(t *testing.T) {
	repo, first, second, classifier := twoCleanCandidates(t)
	capture := filepath.Join(t.TempDir(), "worktree-job", "worktree")
	attempted := attemptedRemovals(t, func(string) (gitpkg.RemoveWorktreeResult, error) {
		return gitpkg.RemoveWorktreeResult{}, &gitpkg.RemovalError{
			Scope:   gitpkg.RemovalScopeCaptureRetained,
			Capture: capture,
			Err:     assert.AnError,
		}
	})

	stderr := &bytes.Buffer{}
	command := &cobra.Command{}
	command.SetErr(stderr)

	err := runCleanWithClassifier(command, classifier, false, true)

	require.Error(t, err)
	assert.ErrorIs(t, err, assert.AnError)
	assert.Contains(t, err.Error(), "was captured for removal and could not be restored")
	assert.Contains(t, err.Error(), capture)
	assert.NotContains(t, err.Error(), "mv ", "restoration failed on the destination; do not recommend overwriting it")
	assert.Len(t, *attempted, 1, "a removal that needs manual recovery stops the batch")
	assertBothCandidatesSurvive(t, repo, stderr, first, second)
}

// Repository-wide state is read by every removal, so a failure to read it
// during execution stops the run for the same reason it does during planning:
// the next candidate would meet the identical error.
func TestCleanStopsOnARepositoryWideRemovalFailure(t *testing.T) {
	repo, first, second, classifier := twoCleanCandidates(t)
	attempted := attemptedRemovals(t, func(string) (gitpkg.RemoveWorktreeResult, error) {
		return gitpkg.RemoveWorktreeResult{}, &gitpkg.RemovalError{Scope: gitpkg.RemovalScopeRepository, Err: assert.AnError}
	})

	stderr := &bytes.Buffer{}
	command := &cobra.Command{}
	command.SetErr(stderr)

	err := runCleanWithClassifier(command, classifier, false, true)

	require.Error(t, err)
	assert.ErrorIs(t, err, assert.AnError)
	assert.Len(t, *attempted, 1, "the second candidate would fail identically")
	assertBothCandidatesSurvive(t, repo, stderr, first, second)
}

// A failure with no classification has made no claim about what it left
// behind, so it cannot be read as a candidate that was merely declined.
func TestCleanStopsOnAnUnclassifiedRemovalFailure(t *testing.T) {
	repo, first, second, classifier := twoCleanCandidates(t)
	attempted := attemptedRemovals(t, func(string) (gitpkg.RemoveWorktreeResult, error) {
		return gitpkg.RemoveWorktreeResult{}, assert.AnError
	})

	stderr := &bytes.Buffer{}
	command := &cobra.Command{}
	command.SetErr(stderr)

	err := runCleanWithClassifier(command, classifier, false, true)

	require.Error(t, err)
	assert.ErrorIs(t, err, assert.AnError)
	assert.Contains(t, err.Error(), "Completed: unknown.")
	assert.Contains(t, err.Error(), "Remaining: unknown;", "a failure that classified nothing must not claim both resources survived")
	assert.Len(t, *attempted, 1, "an unclassified failure stops the batch")
	assertBothCandidatesSurvive(t, repo, stderr, first, second)
}

// A scope this build does not recognize is a newer removal reporting something
// this caller cannot weigh. Continuing would be a guess.
func TestCleanStopsOnAnUnrecognizedRemovalScope(t *testing.T) {
	repo, first, second, classifier := twoCleanCandidates(t)
	attempted := attemptedRemovals(t, func(string) (gitpkg.RemoveWorktreeResult, error) {
		return gitpkg.RemoveWorktreeResult{}, &gitpkg.RemovalError{Scope: gitpkg.RemovalScope(99), Err: assert.AnError}
	})

	stderr := &bytes.Buffer{}
	command := &cobra.Command{}
	command.SetErr(stderr)

	err := runCleanWithClassifier(command, classifier, false, true)

	require.Error(t, err)
	assert.ErrorIs(t, err, assert.AnError)
	assert.Contains(t, err.Error(), "Completed: unknown.")
	assert.Contains(t, err.Error(), "Remaining: unknown;")
	assert.Len(t, *attempted, 1, "an unrecognized scope stops the batch")
	assertBothCandidatesSurvive(t, repo, stderr, first, second)
}

// attemptedRemovals swaps in a removal that records the branches a batch got
// as far as attempting, which is how these tests tell a stopped run from one
// that reached every candidate.
func attemptedRemovals(t *testing.T, removal func(branch string) (gitpkg.RemoveWorktreeResult, error)) *[]string {
	t.Helper()
	attempted := []string{}
	previous := removeWorktreeAndBranch
	removeWorktreeAndBranch = func(_, _, branch, _ string, _ bool) (gitpkg.RemoveWorktreeResult, error) {
		attempted = append(attempted, branch)
		return removal(branch)
	}
	t.Cleanup(func() { removeWorktreeAndBranch = previous })
	return &attempted
}

// assertBothCandidatesSurvive checks that a stopped batch removed nothing and
// reported no candidate as skippable.
func assertBothCandidatesSurvive(t *testing.T, repo string, stderr *bytes.Buffer, first, second string) {
	t.Helper()
	assert.NotContains(t, ui.StripANSI(stderr.String()), "skipping")
	assert.DirExists(t, first)
	assert.DirExists(t, second)
	runGitInDir(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/feature")
	runGitInDir(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/second")
}
