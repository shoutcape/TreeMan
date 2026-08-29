package git

import (
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
	entries := parseWorktreePorcelain("")
	assert.Empty(t, entries)
}

func TestIgnoredPathsIncludesIgnoredFilesAndDirectories(t *testing.T) {
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
	repo := createGitTestRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(" leading.lock\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repo, " leading.lock"), nil, 0o644))

	paths, err := IgnoredPaths(repo)

	require.NoError(t, err)
	assert.Contains(t, paths, filepath.Join(repo, " leading.lock"))
}

func TestInspectWorktreeClassifiesFilesystemStates(t *testing.T) {
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
	repo := createGitTestRepo(t)
	gitTest(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/dev")

	previousDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repo))
	t.Cleanup(func() { require.NoError(t, os.Chdir(previousDir)) })

	branch, err := DetectDefaultBranch()

	require.NoError(t, err)
	assert.Equal(t, "dev", branch)
}

func TestDetectDefaultBranchFallsBackToOriginHeads(t *testing.T) {
	tests := []struct {
		name    string
		branch  string
		push    bool
		want    string
		wantErr string
	}{
		{name: "main", branch: "main", push: true, want: "main"},
		{name: "master", branch: "master", push: true, want: "master"},
		{name: "unsupported", branch: "trunk", wantErr: "could not find 'main' or 'master' on origin"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
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

			previousDir, err := os.Getwd()
			require.NoError(t, err)
			require.NoError(t, os.Chdir(repo))
			t.Cleanup(func() { require.NoError(t, os.Chdir(previousDir)) })

			branch, err := DetectDefaultBranch()

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

	previousDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repo))
	t.Cleanup(func() { require.NoError(t, os.Chdir(previousDir)) })

	heads, err := RemoteHeads([]string{"main", "feature/one", "missing"})
	require.NoError(t, err)
	assert.Equal(t, mainSHA, heads["main"])
	assert.Equal(t, mainSHA, heads["feature/one"])
	assert.NotContains(t, heads, "missing")

	tips, err := BranchSHAs([]string{"main", "feature/one", "missing"})
	require.NoError(t, err)
	assert.Equal(t, mainSHA, tips["main"])
	assert.Equal(t, mainSHA, tips["feature/one"])
	assert.NotContains(t, tips, "missing")

	sha, exists, err := RemoteTrackingBranchSHA("main")
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, mainSHA, sha)

	gitTest(t, repo, "update-ref", "-d", "refs/remotes/origin/main")
	sha, exists, err = RemoteTrackingBranchSHA("main")
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Empty(t, sha)
}

func TestAnyCommitIsAncestor(t *testing.T) {
	repo := createGitTestRepo(t)
	base := gitTestOutput(t, repo, "rev-parse", "HEAD")
	gitTest(t, repo, "commit", "--allow-empty", "-m", "next")
	tip := gitTestOutput(t, repo, "rev-parse", "HEAD")

	previousDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repo))
	t.Cleanup(func() { require.NoError(t, os.Chdir(previousDir)) })

	ancestor, err := AnyCommitIsAncestor([]string{"missing", base}, tip)
	require.NoError(t, err)
	assert.True(t, ancestor)

	ancestor, err = AnyCommitIsAncestor([]string{"missing"}, tip)
	require.NoError(t, err)
	assert.False(t, ancestor)
}

func TestDeleteBranchAtSHA(t *testing.T) {
	t.Run("deletes matching ref", func(t *testing.T) {
		repo := createGitTestRepo(t)
		gitTest(t, repo, "branch", "feature")
		sha := gitTestOutput(t, repo, "rev-parse", "feature")

		require.NoError(t, DeleteBranchAtSHA(repo, "feature", sha))
		gitTestFails(t, repo, "show-ref", "--verify", "refs/heads/feature")
	})

	t.Run("preserves moved ref", func(t *testing.T) {
		repo := createGitTestRepo(t)
		gitTest(t, repo, "branch", "feature")
		sha := gitTestOutput(t, repo, "rev-parse", "feature")
		gitTest(t, repo, "commit", "--allow-empty", "-m", "advance main")
		movedSHA := gitTestOutput(t, repo, "rev-parse", "HEAD")
		gitTest(t, repo, "update-ref", "refs/heads/feature", movedSHA)

		require.Error(t, DeleteBranchAtSHA(repo, "feature", sha))
		assert.Equal(t, movedSHA, gitTestOutput(t, repo, "rev-parse", "feature"))
	})

	t.Run("preserves checked out branch", func(t *testing.T) {
		repo := createGitTestRepo(t)
		worktree := filepath.Join(t.TempDir(), "feature")
		gitTest(t, repo, "worktree", "add", "-b", "feature", worktree)
		sha := gitTestOutput(t, repo, "rev-parse", "feature")

		err := DeleteBranchAtSHA(repo, "feature", sha)
		require.EqualError(t, err, `branch "feature" is still checked out at worktree "`+worktree+`"`)
		assert.Equal(t, sha, gitTestOutput(t, repo, "rev-parse", "feature"))
	})
}

func TestCommonDirAndWorktreeIDReturnNormalizedPaths(t *testing.T) {
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
	t.Run("TreeMan worktree add", func(t *testing.T) {
		repo := createGitTestRepo(t)
		worktree := filepath.Join(t.TempDir(), "feature")
		locked, release, finished := holdWorktreeMutationLock(t, repo)
		<-locked

		added := make(chan error, 1)
		go func() { added <- worktreeAdd(repo, worktree, "feature", "HEAD") }()
		assertBlocked(t, added)
		close(release)
		require.NoError(t, <-finished)
		require.NoError(t, <-added)
		assert.Equal(t, "feature", gitTestOutput(t, worktree, "branch", "--show-current"))
	})

	t.Run("TreeMan guarded deletion", func(t *testing.T) {
		repo := createGitTestRepo(t)
		gitTest(t, repo, "branch", "feature")
		sha := gitTestOutput(t, repo, "rev-parse", "feature")
		locked, release, finished := holdWorktreeMutationLock(t, repo)
		<-locked

		deleted := make(chan error, 1)
		go func() { deleted <- DeleteBranchAtSHA(repo, "feature", sha) }()
		assertBlocked(t, deleted)
		close(release)
		require.NoError(t, <-finished)
		require.NoError(t, <-deleted)
		gitTestFails(t, repo, "show-ref", "--verify", "refs/heads/feature")
	})
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

func createGitTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitTest(t, repo, "init", "--initial-branch=main")
	gitTest(t, repo, "config", "user.email", "test@example.com")
	gitTest(t, repo, "config", "user.name", "Test User")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "file"), []byte("base\n"), 0o644))
	gitTest(t, repo, "add", "file")
	gitTest(t, repo, "commit", "-m", "base")
	return repo
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
