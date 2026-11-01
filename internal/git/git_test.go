package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseWorktreePorcelain(t *testing.T) {
	t.Parallel()
	input := `worktree /home/user/Github/my-project
HEAD abc1234
branch refs/heads/main

worktree /home/user/Github/my-project.feature-cool
HEAD def5678
branch refs/heads/feature/cool

worktree /home/user/Github/my-project.detached
HEAD fff0000
detached

`
	entries := parseWorktreePorcelain(input)

	assert.Len(t, entries, 3)

	assert.Equal(t, "/home/user/Github/my-project", entries[0].Path)
	assert.Equal(t, "main", entries[0].Branch)

	assert.Equal(t, "/home/user/Github/my-project.feature-cool", entries[1].Path)
	assert.Equal(t, "feature/cool", entries[1].Branch)

	// Detached HEAD — branch should be empty string.
	assert.Equal(t, "/home/user/Github/my-project.detached", entries[2].Path)
	assert.Equal(t, "", entries[2].Branch)
}

func TestParseWorktreePorcelain_NoTrailingNewline(t *testing.T) {
	t.Parallel()
	// Some git versions omit the trailing blank line on the last entry.
	input := strings.TrimRight(`worktree /home/user/repo
HEAD abc1234
branch refs/heads/main
`, "\n")

	entries := parseWorktreePorcelain(input)
	assert.Len(t, entries, 1)
	assert.Equal(t, "/home/user/repo", entries[0].Path)
	assert.Equal(t, "main", entries[0].Branch)
}

func TestParseWorktreePorcelain_Empty(t *testing.T) {
	t.Parallel()
	entries := parseWorktreePorcelain("")
	assert.Empty(t, entries)
}

func TestIgnoredPathsIncludesIgnoredFilesAndDirectories(t *testing.T) {
	t.Parallel()
	repo := createGitTestRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("generated/\nprivate.lock\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "generated", "module"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "generated", "module", "go.mod"), []byte("module example.com/generated\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "private.lock"), nil, 0o644))

	paths, err := IgnoredPaths(repo)

	require.NoError(t, err)
	assert.Contains(t, paths, filepath.Join(repo, "generated"))
	assert.Contains(t, paths, filepath.Join(repo, "private.lock"))
}

func TestIgnoredPathsPreservesWhitespaceInNames(t *testing.T) {
	t.Parallel()
	repo := createGitTestRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(" leading.lock\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repo, " leading.lock"), nil, 0o644))

	paths, err := IgnoredPaths(repo)

	require.NoError(t, err)
	assert.Contains(t, paths, filepath.Join(repo, " leading.lock"))
}

func TestInspectWorktreeClassifiesFilesystemStates(t *testing.T) {
	t.Parallel()
	dir := createGitTestRepo(t)
	file := filepath.Join(t.TempDir(), "worktree")
	require.NoError(t, os.WriteFile(file, nil, 0o644))

	state, err := InspectWorktree(filepath.Join(dir, "missing"))
	require.NoError(t, err)
	assert.Equal(t, WorktreeStateStale, state)

	state, err = InspectWorktree(dir)
	require.NoError(t, err)
	assert.Equal(t, WorktreeStateClean, state)

	state, err = InspectWorktree(file)
	require.NoError(t, err)
	assert.Equal(t, WorktreeStateStale, state)
}

func TestDetectDefaultBranchUsesOriginHEADName(t *testing.T) {
	t.Parallel()
	repo := createGitTestRepo(t)
	gitTest(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/dev")

	r := openTestRepo(t, repo)

	branch, err := r.DetectDefaultBranch()

	require.NoError(t, err)
	assert.Equal(t, "dev", branch)
}

func TestDetectDefaultBranchFallsBackToOriginHeads(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		branch  string
		push    bool
		want    string
		wantErr string
	}{
		{name: "main", branch: "main", push: true, want: "main"},
		{name: "master", branch: "master", push: true, want: "master"},
		{name: "unsupported", branch: "tree", wantErr: "could not find 'main' or 'master' on origin"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			origin := filepath.Join(t.TempDir(), "origin.git")
			output, err := exec.Command("git", "init", "--bare", "--initial-branch="+test.branch, origin).CombinedOutput()
			require.NoErrorf(t, err, "could not initialize origin: %s", output)

			repo := createGitTestRepo(t)
			if test.branch == "master" {
				gitTest(t, repo, "branch", "-M", "master")
			}
			gitTest(t, repo, "remote", "add", "origin", origin)
			if test.push {
				gitTest(t, repo, "push", "-u", "origin", test.branch)
			}
			gitTestFails(t, repo, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")

			r := openTestRepo(t, repo)

			branch, err := r.DetectDefaultBranch()

			if test.wantErr != "" {
				require.EqualError(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.want, branch)
		})
	}
}

func TestRemoteHeadsAndTrackingBranchSHA(t *testing.T) {
	t.Parallel()
	origin := filepath.Join(t.TempDir(), "origin.git")
	output, err := exec.Command("git", "init", "--bare", "--initial-branch=main", origin).CombinedOutput()
	require.NoErrorf(t, err, "could not initialize origin: %s", output)

	repo := createGitTestRepo(t)
	gitTest(t, repo, "remote", "add", "origin", origin)
	gitTest(t, repo, "push", "-u", "origin", "main")
	gitTest(t, repo, "branch", "feature/one")
	gitTest(t, repo, "push", "origin", "feature/one")
	gitTest(t, repo, "fetch", "origin", "refs/heads/main:refs/remotes/origin/main")
	mainSHA := gitTestOutput(t, repo, "rev-parse", "main")

	r := openTestRepo(t, repo)

	heads, err := r.RemoteHeads([]string{"main", "feature/one", "missing"})
	require.NoError(t, err)
	assert.Equal(t, mainSHA, heads["main"])
	assert.Equal(t, mainSHA, heads["feature/one"])
	assert.NotContains(t, heads, "missing")

	tips, err := r.BranchSHAs([]string{"main", "feature/one", "missing"})
	require.NoError(t, err)
	assert.Equal(t, mainSHA, tips["main"])
	assert.Equal(t, mainSHA, tips["feature/one"])
	assert.NotContains(t, tips, "missing")

	sha, exists, err := r.RemoteTrackingBranchSHA("main")
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, mainSHA, sha)

	gitTest(t, repo, "update-ref", "-d", "refs/remotes/origin/main")
	sha, exists, err = r.RemoteTrackingBranchSHA("main")
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Empty(t, sha)
}

func TestFetchRemoteBranchAcceptsForcePushedTip(t *testing.T) {
	t.Parallel()
	origin := filepath.Join(t.TempDir(), "origin.git")
	output, err := exec.Command("git", "init", "--bare", "--initial-branch=main", origin).CombinedOutput()
	require.NoErrorf(t, err, "could not initialize origin: %s", output)

	repo := createGitTestRepo(t)
	gitTest(t, repo, "remote", "add", "origin", origin)
	gitTest(t, repo, "push", "-u", "origin", "main")
	mainSHA := gitTestOutput(t, repo, "rev-parse", "main")
	gitTest(t, repo, "checkout", "-b", "feature/rewound")
	gitTest(t, repo, "commit", "--allow-empty", "-m", "feature commit")
	gitTest(t, repo, "push", "-u", "origin", "feature/rewound")
	gitTest(t, origin, "update-ref", "refs/heads/feature/rewound", mainSHA)

	r := openTestRepo(t, repo)

	require.NoError(t, r.FetchRemoteBranch("feature/rewound"))
	trackingSHA, exists, err := r.RemoteTrackingBranchSHA("feature/rewound")
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, mainSHA, trackingSHA)
}

func TestFetchCommitUsesTemporaryRefWithoutChangingFetchHead(t *testing.T) {
	t.Parallel()
	origin := filepath.Join(t.TempDir(), "origin.git")
	output, err := exec.Command("git", "init", "--bare", "--initial-branch=main", origin).CombinedOutput()
	require.NoErrorf(t, err, "could not initialize origin: %s", output)

	repo := createGitTestRepo(t)
	gitTest(t, repo, "remote", "add", "origin", origin)
	gitTest(t, repo, "branch", "feature")
	gitTest(t, repo, "push", "origin", "feature")
	wantSHA := gitTestOutput(t, repo, "rev-parse", "feature")
	fetchHeadPath := filepath.Join(repo, ".git", "FETCH_HEAD")
	require.NoError(t, os.WriteFile(fetchHeadPath, []byte("existing state\n"), 0o644))

	r := openTestRepo(t, repo)

	sha, err := r.FetchCommit("refs/heads/feature")

	require.NoError(t, err)
	assert.Equal(t, wantSHA, sha)
	assert.Equal(t, "existing state\n", string(mustReadFile(t, fetchHeadPath)))
	assert.Empty(t, gitTestOutput(t, repo, "for-each-ref", "--format=%(refname)", "refs/treeman/fetch"))
}

func TestAnyCommitIsAncestor(t *testing.T) {
	t.Parallel()
	repo := createGitTestRepo(t)
	base := gitTestOutput(t, repo, "rev-parse", "HEAD")
	gitTest(t, repo, "commit", "--allow-empty", "-m", "next")
	tip := gitTestOutput(t, repo, "rev-parse", "HEAD")

	r := openTestRepo(t, repo)

	ancestor, err := r.AnyCommitIsAncestor([]string{"missing", base}, tip)
	require.NoError(t, err)
	assert.True(t, ancestor)

	ancestor, err = r.AnyCommitIsAncestor([]string{"missing"}, tip)
	require.NoError(t, err)
	assert.False(t, ancestor)
}

// CommitIsAncestorOfAny answers the question a squash-merged pull request
// leaves behind: was the local tip already carried in by a merged head, even
// though the tip never became that head?
func TestCommitIsAncestorOfAny(t *testing.T) {
	t.Parallel()
	repo := createGitTestRepo(t)
	base := gitTestOutput(t, repo, "rev-parse", "HEAD")
	gitTest(t, repo, "commit", "--allow-empty", "-m", "next")
	tip := gitTestOutput(t, repo, "rev-parse", "HEAD")

	r := openTestRepo(t, repo)

	contained, err := r.CommitIsAncestorOfAny(base, []string{"missing", tip})
	require.NoError(t, err)
	assert.True(t, contained)

	contained, err = r.CommitIsAncestorOfAny(tip, []string{base})
	require.NoError(t, err)
	assert.False(t, contained)

	contained, err = r.CommitIsAncestorOfAny(base, []string{"missing"})
	require.NoError(t, err)
	assert.False(t, contained)

	contained, err = r.CommitIsAncestorOfAny(base, []string{base})
	require.NoError(t, err)
	assert.False(t, contained)

	contained, err = r.CommitIsAncestorOfAny(base, nil)
	require.NoError(t, err)
	assert.False(t, contained)
}

// A tip on a branch that shares no history with the merged head is not
// contained by it, and the unrelated histories must not read as an error.
func TestCommitIsAncestorOfAnyRejectsUnrelatedHistory(t *testing.T) {
	t.Parallel()
	repo := createGitTestRepo(t)
	base := gitTestOutput(t, repo, "rev-parse", "HEAD")
	gitTest(t, repo, "checkout", "--orphan", "unrelated")
	gitTest(t, repo, "commit", "--allow-empty", "-m", "unrelated root")
	unrelated := gitTestOutput(t, repo, "rev-parse", "HEAD")

	r := openTestRepo(t, repo)

	contained, err := r.CommitIsAncestorOfAny(base, []string{unrelated})
	require.NoError(t, err)
	assert.False(t, contained)
}

// UnpushedCommitCount counts what a deletion would make unreachable: commits
// no remote-tracking ref and not the default branch can reach.
func TestUnpushedCommitCount(t *testing.T) {
	t.Parallel()
	repo := createGitTestRepo(t)
	gitTest(t, repo, "checkout", "-b", "feature")
	gitTest(t, repo, "commit", "--allow-empty", "-m", "one")
	gitTest(t, repo, "commit", "--allow-empty", "-m", "two")

	count, err := openTestRepo(t, repo).UnpushedCommitCount("feature", "main")
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// A default branch that cannot be read only widens what counts as safe, so
	// losing it falls back to the conservative count rather than an error.
	count, err = openTestRepo(t, repo).UnpushedCommitCount("feature", "no-such-branch")
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	// Any remote-tracking ref that reaches the commits answers the question --
	// the branch does not have to track one.
	gitTest(t, repo, "update-ref", "refs/remotes/origin/feature", "refs/heads/feature")
	count, err = openTestRepo(t, repo).UnpushedCommitCount("feature", "main")
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	gitTest(t, repo, "commit", "--allow-empty", "-m", "after the push")
	count, err = openTestRepo(t, repo).UnpushedCommitCount("feature", "main")
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

// Git records the path it resolved, not the text the caller passed. Reached
// through a symlinked prefix -- which is every temporary directory on macOS --
// a rollback that matched on the text found nothing to remove and reported
// success while leaving the worktree and its branch in place.
func TestRemoveCreatedWorktreeMatchesThroughASymlinkedPrefix(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	real := filepath.Join(root, "real")
	require.NoError(t, os.Mkdir(real, 0o700))
	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(real, link))
	repo := filepath.Join(real, "repo")
	require.NoError(t, os.Mkdir(repo, 0o755))
	initGitTestRepo(t, repo)

	r := openTestRepo(t, repo)

	// The caller names the worktree through the link; Git will record the
	// directory it resolved.
	worktree := filepath.Join(link, "feature")
	created, err := r.CreatePlannedWorktree(fixedPlan(worktree), "feature", "HEAD")
	require.NoError(t, err)

	require.NoError(t, r.RemoveCreatedWorktree(created))

	assert.NoDirExists(t, filepath.Join(real, "feature"))
	assert.NotContains(t, gitTestOutput(t, repo, "worktree", "list", "--porcelain"), "feature")
	gitTestFails(t, repo, "show-ref", "--verify", "refs/heads/feature")
}

func TestCreatedWorktreeLifecycleIsAtomic(t *testing.T) {
	t.Parallel()
	t.Run("create and remove owned worktree", func(t *testing.T) {
		t.Parallel()
		repo := createGitTestRepo(t)
		worktree := filepath.Join(t.TempDir(), "feature")
		r := openTestRepo(t, repo)

		created, err := r.CreatePlannedWorktree(fixedPlan(worktree), "feature", "HEAD")
		require.NoError(t, err)
		assert.Equal(t, worktree, created.Path)
		assert.Equal(t, "feature", created.Branch)
		assert.NotEmpty(t, created.SHA)

		require.NoError(t, r.RemoveCreatedWorktree(created))
		assert.NoDirExists(t, worktree)
		gitTestFails(t, repo, "show-ref", "--verify", "refs/heads/feature")
	})

	t.Run("removal is idempotent once the artifacts are gone", func(t *testing.T) {
		t.Parallel()
		repo := createGitTestRepo(t)
		worktree := filepath.Join(t.TempDir(), "feature")
		r := openTestRepo(t, repo)

		created, err := r.CreatePlannedWorktree(fixedPlan(worktree), "feature", "HEAD")
		require.NoError(t, err)

		// Callers that clean up after an operation which may or may not have
		// completed the deletion itself get the same end state either way.
		require.NoError(t, r.RemoveCreatedWorktree(created))
		require.NoError(t, r.RemoveCreatedWorktree(created))
		assert.NoDirExists(t, worktree)
		gitTestFails(t, repo, "show-ref", "--verify", "refs/heads/feature")
	})

	t.Run("failed worktree add rolls back reserved branch", func(t *testing.T) {
		t.Parallel()
		repo := createGitTestRepo(t)
		worktree := filepath.Join(t.TempDir(), "feature")
		require.NoError(t, os.WriteFile(worktree, []byte("occupied"), 0o644))
		r := openTestRepo(t, repo)

		created, err := r.CreatePlannedWorktree(fixedPlan(worktree), "feature", "HEAD")

		require.Error(t, err)
		assert.Empty(t, created)
		gitTestFails(t, repo, "show-ref", "--verify", "refs/heads/feature")
	})

	t.Run("existing branch is never claimed or removed", func(t *testing.T) {
		t.Parallel()
		repo := createGitTestRepo(t)
		gitTest(t, repo, "branch", "feature")
		sha := gitTestOutput(t, repo, "rev-parse", "feature")
		worktree := filepath.Join(t.TempDir(), "feature")
		r := openTestRepo(t, repo)

		created, err := r.CreatePlannedWorktree(fixedPlan(worktree), "feature", "HEAD")

		require.Error(t, err)
		assert.Empty(t, created)
		assert.Equal(t, sha, gitTestOutput(t, repo, "rev-parse", "feature"))
	})

	t.Run("rollback preserves a branch checked out by another process", func(t *testing.T) {
		t.Parallel()
		repo := createGitTestRepo(t)
		otherWorktree := filepath.Join(t.TempDir(), "other")
		gitTest(t, repo, "worktree", "add", "-b", "feature", otherWorktree)
		sha := gitTestOutput(t, repo, "rev-parse", "feature")

		err := rollbackWorktreeCreation(repo, filepath.Join(t.TempDir(), "requested"), "feature", sha)

		require.ErrorContains(t, err, "it is checked out")
		assert.Equal(t, sha, gitTestOutput(t, repo, "rev-parse", "feature"))
		assert.DirExists(t, otherWorktree)
	})
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	return contents
}

func TestCommonDirAndWorktreeIDReturnNormalizedPaths(t *testing.T) {
	t.Parallel()
	repo := createGitTestRepo(t)
	worktree := filepath.Join(t.TempDir(), "feature")
	gitTest(t, repo, "worktree", "add", "-b", "feature-normalized", worktree)
	commonDir, err := CommonDir(filepath.Join(worktree, "."))
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(commonDir))
	assert.Equal(t, filepath.Clean(commonDir), commonDir)
	id, err := WorktreeID(filepath.Join(worktree, "."))
	require.NoError(t, err)
	assert.NotEmpty(t, id)
	assert.Equal(t, filepath.Base(id), id)
}

func TestTreeManWorktreeMutationLockSerializesAddAndDelete(t *testing.T) {
	t.Parallel()
	t.Run("TreeMan worktree add", func(t *testing.T) {
		t.Parallel()
		repo := createGitTestRepo(t)
		worktree := filepath.Join(t.TempDir(), "feature")
		locked, release, finished := holdWorktreeMutationLock(t, repo)
		<-locked

		added := make(chan error, 1)
		go func() {
			_, err := createPlannedWorktree(repo, fixedPlan(worktree), "feature", "HEAD")
			added <- err
		}()
		assertBlocked(t, added)
		close(release)
		require.NoError(t, <-finished)
		require.NoError(t, <-added)
		assert.Equal(t, "feature", gitTestOutput(t, worktree, "branch", "--show-current"))
	})

	t.Run("TreeMan guarded deletion", func(t *testing.T) {
		t.Parallel()
		repo := createGitTestRepo(t)
		worktree := filepath.Join(t.TempDir(), "feature")
		gitTest(t, repo, "worktree", "add", "-b", "feature", worktree)
		sha := gitTestOutput(t, repo, "rev-parse", "feature")
		r := openTestRepo(t, repo)
		locked, release, finished := holdWorktreeMutationLock(t, repo)
		<-locked

		deleted := make(chan error, 1)
		go func() {
			_, err := r.RemoveWorktreeAndBranch(worktree, "feature", sha, false)
			deleted <- err
		}()
		assertBlocked(t, deleted)
		close(release)
		require.NoError(t, <-finished)
		require.NoError(t, <-deleted)
		gitTestFails(t, repo, "show-ref", "--verify", "refs/heads/feature")
		assert.NotContains(t, gitTestOutput(t, repo, "worktree", "list", "--porcelain"), worktree)
	})
}

func TestCreatePlannedWorktreeReselectsAfterWaitingForLock(t *testing.T) {
	t.Parallel()
	repo := createGitTestRepo(t)
	destination := filepath.Join(t.TempDir(), "occupied")
	locked, release, finished := holdWorktreeMutationLock(t, repo)
	<-locked

	created := make(chan error, 1)
	go func() {
		_, err := createPlannedWorktree(repo, func(existing []WorktreeEntry) (string, error) {
			for _, entry := range existing {
				if sameRemovalPath(entry.Path, destination) {
					return "", fmt.Errorf("destination became occupied by %s", entry.Branch)
				}
			}
			return destination, nil
		}, "feature-second", "HEAD")
		created <- err
	}()
	assertBlocked(t, created)

	gitTest(t, repo, "worktree", "add", "-b", "feature-first", destination)
	close(release)
	require.NoError(t, <-finished)
	err := <-created
	require.Error(t, err)
	assert.Contains(t, err.Error(), "destination became occupied")
	assert.Equal(t, "feature-first", gitTestOutput(t, destination, "branch", "--show-current"))
	gitTestFails(t, repo, "show-ref", "--verify", "refs/heads/feature-second")
}

func holdWorktreeMutationLock(t *testing.T, repo string) (locked <-chan struct{}, release chan<- struct{}, finished <-chan error) {
	t.Helper()
	lockedSignal := make(chan struct{})
	releaseSignal := make(chan struct{})
	finishedSignal := make(chan error, 1)
	go func() {
		finishedSignal <- withWorktreeMutationLock(repo, func() error {
			close(lockedSignal)
			<-releaseSignal
			return nil
		})
	}()
	return lockedSignal, releaseSignal, finishedSignal
}

func assertBlocked(t *testing.T, result <-chan error) {
	t.Helper()
	select {
	case err := <-result:
		t.Fatalf("TreeMan worktree mutation completed while lock was held: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}

// openTestRepo opens the repository containing dir, failing the test when it
// cannot.
func openTestRepo(t *testing.T, dir string) Repo {
	t.Helper()
	repo, err := Open(dir)
	require.NoError(t, err)
	return repo
}

func createGitTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	initGitTestRepo(t, repo)
	return repo
}

// initGitTestRepo seeds a repository at a directory the caller chose, for tests
// that need to control where it sits.
func initGitTestRepo(t *testing.T, repo string) {
	t.Helper()
	gitTest(t, repo, "init", "--initial-branch=main")
	gitTest(t, repo, "config", "user.email", "test@example.com")
	gitTest(t, repo, "config", "user.name", "Test User")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "file"), []byte("base\n"), 0o644))
	gitTest(t, repo, "add", "file")
	gitTest(t, repo, "commit", "-m", "base")
}

func gitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	output, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	require.NoErrorf(t, err, "git %v failed: %s", args, output)
}

func gitTestOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	output, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	require.NoErrorf(t, err, "git %v failed: %s", args, output)
	return strings.TrimSpace(string(output))
}

func gitTestFails(t *testing.T, dir string, args ...string) {
	t.Helper()
	output, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	require.Errorf(t, err, "git %v unexpectedly succeeded: %s", args, output)
}

func TestLocalBranchNamesListsEveryLocalBranch(t *testing.T) {
	t.Parallel()
	repo := createGitTestRepo(t)
	gitTest(t, repo, "branch", "feature/one")
	gitTest(t, repo, "branch", "feature/two")

	r := openTestRepo(t, repo)

	names, err := r.LocalBranchNames()

	require.NoError(t, err)
	assert.Equal(t, map[string]struct{}{
		"main":        {},
		"feature/one": {},
		"feature/two": {},
	}, names)
}

func TestDiscardWorktreeChangesReportsWhatItRemoved(t *testing.T) {
	t.Parallel()
	repo := createGitTestRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("ignored/\n"), 0o644))
	gitTest(t, repo, "add", ".gitignore")
	gitTest(t, repo, "commit", "-m", "ignore build output")

	// Drift a caller's setup can leave behind: a rewritten tracked file,
	// output the project ignores, and output it does not.
	require.NoError(t, os.WriteFile(filepath.Join(repo, "file"), []byte("rewritten\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "ignored"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "ignored", "artifact"), []byte("kept\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "generated"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "generated", "artifact"), []byte("removed\n"), 0o644))

	removed, err := DiscardWorktreeChanges(repo)
	require.NoError(t, err)

	// The untracked paths are reported so a caller that populated the worktree
	// can tell how much of its own output this cleared.
	assert.Equal(t, []string{"generated" + string(os.PathSeparator)}, removed)
	assert.Equal(t, "base\n", readGitTestFile(t, filepath.Join(repo, "file")))
	assert.FileExists(t, filepath.Join(repo, "ignored", "artifact"))
	assert.NoDirExists(t, filepath.Join(repo, "generated"))
	assert.Empty(t, gitTestOutput(t, repo, "status", "--porcelain", "--untracked-files=all"))
}

func readGitTestFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(content)
}

func TestHasCommitsDistinguishesAnEmptyRepository(t *testing.T) {
	t.Parallel()
	empty := t.TempDir()
	gitTest(t, empty, "init", "--initial-branch=main")

	hasCommits, err := openTestRepo(t, empty).HasCommits()
	require.NoError(t, err)
	assert.False(t, hasCommits)

	hasCommits, err = openTestRepo(t, createGitTestRepo(t)).HasCommits()
	require.NoError(t, err)
	assert.True(t, hasCommits)
}

func TestParseWorktreePorcelainReadsLockAndPrunable(t *testing.T) {
	t.Parallel()
	out := strings.Join([]string{
		"worktree /repo",
		"branch refs/heads/main",
		"",
		"worktree /repo/.worktrees/locked",
		"branch refs/heads/feature/locked",
		"locked on a removable disk",
		"",
		"worktree /repo/.worktrees/bare-lock",
		"branch refs/heads/feature/bare-lock",
		"locked",
		"",
		"worktree /repo/.worktrees/gone",
		"branch refs/heads/feature/gone",
		"prunable gitdir file points to non-existent location",
		"",
	}, "\n")

	entries := parseWorktreePorcelain(out)

	require.Len(t, entries, 4)
	assert.False(t, entries[0].Locked)
	assert.False(t, entries[0].Prunable)

	assert.True(t, entries[1].Locked)
	assert.Equal(t, "on a removable disk", entries[1].LockReason)

	assert.True(t, entries[2].Locked)
	assert.Equal(t, "", entries[2].LockReason)

	assert.True(t, entries[3].Prunable)
	assert.False(t, entries[3].Locked)
}
