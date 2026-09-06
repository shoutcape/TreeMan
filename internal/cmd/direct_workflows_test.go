package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunBranchDirectDoesNotRequireForgeCLI(t *testing.T) {
	repo := createRemoteRepoWithNestedModule(t)
	gitTest(t, repo, "checkout", "-b", "feature/direct")
	gitTest(t, repo, "push", "-u", "origin", "feature/direct")
	gitTest(t, repo, "checkout", "main")
	gitTest(t, repo, "branch", "-D", "feature/direct")
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".envrc"), []byte("use nix\n"), 0o644))
	chdirForTest(t, repo)
	pathWithOnlyGit(t)

	buf, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command := commandWithOutput(buf, stderr)
	require.NoError(t, runBranch(command, "feature/direct"))

	worktree := filepath.Join(repo, ".worktrees", "feature-direct")
	assert.Equal(t, worktree+"\n", buf.String())
	assert.DirExists(t, worktree)
	assert.Contains(t, stderr.String(), "Nested module apps/web (package-lock.json): skipped; not installed automatically.")
	assert.NotContains(t, stderr.String(), "npm is not installed")
	assert.Contains(t, ui.StripANSI(stderr.String()), "direnv")
	assert.Contains(t, ui.StripANSI(stderr.String()), "Nix")
}

func TestRunReviewReportsNestedModules(t *testing.T) {
	repo := createRemoteRepoWithNestedModule(t)
	gitTest(t, repo, "checkout", "-b", "feature/review")
	gitTest(t, repo, "push", "-u", "origin", "feature/review")
	gitTest(t, repo, "push", "origin", "feature/review:refs/pull/1/head")
	gitTest(t, repo, "checkout", "main")
	gitTest(t, repo, "branch", "-D", "feature/review")
	chdirForTest(t, repo)
	pathWithGitAndGH(t)
	t.Setenv("_TREEMAN_FORGE", "github")
	t.Setenv("_TREEMAN_GH_REPO", "owner/repo")

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	require.NoError(t, runReview(commandWithOutput(stdout, stderr), "1", creationSetupOptions{
		skipEnv: true, skipDatabase: true, skipHooks: true,
	}, ""))

	worktree := filepath.Join(repo, ".worktrees", "feature-review")
	assert.Equal(t, worktree+"\n", stdout.String())
	assert.DirExists(t, worktree)
	assert.Contains(t, stderr.String(), "Nested module apps/web (package-lock.json): skipped; not installed automatically.")
	assert.NotContains(t, stderr.String(), "npm is not installed")
}

// TestRunBranchLaunchesTheExecCommand follows the whole branch workflow to the
// handover: the worktree it created and made ready is the one the launcher is
// given, and no destination goes out to the shell behind the command's back.
func TestRunBranchLaunchesTheExecCommand(t *testing.T) {
	repo := createRemoteRepoWithNestedModule(t)
	gitTest(t, repo, "checkout", "-b", "feature/direct")
	gitTest(t, repo, "push", "-u", "origin", "feature/direct")
	gitTest(t, repo, "checkout", "main")
	gitTest(t, repo, "branch", "-D", "feature/direct")
	chdirForTest(t, repo)
	pathWithOnlyGit(t)
	record := stubLaunch(t, nil)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	require.NoError(t, runBranchWithSetup(commandWithOutput(stdout, stderr), "feature/direct", creationSetupOptions{
		skipEnv: true, skipDatabase: true, skipHooks: true,
	}, "nvim ."))

	worktree := filepath.Join(repo, ".worktrees", "feature-direct")
	assert.DirExists(t, worktree)
	require.True(t, record.called, "a ready worktree is handed to the command")
	assert.Equal(t, worktree, record.dir)
	assert.Equal(t, "nvim .", record.command)
	assert.Empty(t, stdout.String(), "the launched command owns stdout, and there is no destination to report")
}

// TestRunReviewLaunchesTheExecCommand is the same handover for a review
// worktree, which reaches it by a different creation path.
func TestRunReviewLaunchesTheExecCommand(t *testing.T) {
	repo := createRemoteRepoWithNestedModule(t)
	gitTest(t, repo, "checkout", "-b", "feature/review")
	gitTest(t, repo, "push", "-u", "origin", "feature/review")
	gitTest(t, repo, "push", "origin", "feature/review:refs/pull/1/head")
	gitTest(t, repo, "checkout", "main")
	gitTest(t, repo, "branch", "-D", "feature/review")
	chdirForTest(t, repo)
	pathWithGitAndGH(t)
	t.Setenv("_TREEMAN_FORGE", "github")
	t.Setenv("_TREEMAN_GH_REPO", "owner/repo")
	record := stubLaunch(t, nil)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	require.NoError(t, runReview(commandWithOutput(stdout, stderr), "1", creationSetupOptions{
		skipEnv: true, skipDatabase: true, skipHooks: true,
	}, "nvim ."))

	worktree := filepath.Join(repo, ".worktrees", "feature-review")
	assert.DirExists(t, worktree)
	require.True(t, record.called, "a ready worktree is handed to the command")
	assert.Equal(t, worktree, record.dir)
	assert.Equal(t, "nvim .", record.command)
	assert.Empty(t, stdout.String(), "the launched command owns stdout, and there is no destination to report")
}

func TestBranchAndReviewUseConfiguredExternalDirectory(t *testing.T) {
	t.Run("branch", func(t *testing.T) {
		repo := createRemoteRepoWithNestedModule(t)
		gitTest(t, repo, "checkout", "-b", "feature/external-branch")
		gitTest(t, repo, "push", "-u", "origin", "feature/external-branch")
		gitTest(t, repo, "checkout", "main")
		gitTest(t, repo, "branch", "-D", "feature/external-branch")
		external := filepath.Join(t.TempDir(), "trees")
		require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte("worktree_dir = \""+external+"\"\n"), 0o600))
		chdirForTest(t, repo)
		pathWithOnlyGit(t)

		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		require.NoError(t, runBranch(commandWithOutput(stdout, stderr), "feature/external-branch"))

		want := filepath.Join(external, "feature-external-branch")
		assert.Equal(t, want+"\n", stdout.String())
		assert.DirExists(t, want)
	})

	t.Run("review", func(t *testing.T) {
		repo := createRemoteRepoWithNestedModule(t)
		gitTest(t, repo, "checkout", "-b", "feature/review")
		gitTest(t, repo, "push", "-u", "origin", "feature/review")
		gitTest(t, repo, "push", "origin", "feature/review:refs/pull/1/head")
		gitTest(t, repo, "checkout", "main")
		gitTest(t, repo, "branch", "-D", "feature/review")
		external := filepath.Join(t.TempDir(), "trees")
		require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte("worktree_dir = \""+external+"\"\n"), 0o600))
		chdirForTest(t, repo)
		pathWithGitAndGH(t)
		t.Setenv("_TREEMAN_FORGE", "github")
		t.Setenv("_TREEMAN_GH_REPO", "owner/repo")

		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		require.NoError(t, runReview(commandWithOutput(stdout, stderr), "1", creationSetupOptions{
			skipEnv: true, skipDeps: true, skipDatabase: true, skipHooks: true,
		}, ""))

		want := filepath.Join(external, "feature-review")
		assert.Equal(t, want+"\n", stdout.String())
		assert.DirExists(t, want)
	})
}

func createRemoteRepoWithNestedModule(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	remote := filepath.Join(parent, "remote.git")
	repo := filepath.Join(parent, "repo")
	gitTest(t, parent, "init", "--bare", "--initial-branch=main", remote)
	gitTest(t, parent, "init", "-b", "main", repo)
	gitTest(t, repo, "config", "user.name", "TreeMan Test")
	gitTest(t, repo, "config", "user.email", "test@example.com")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "README.md"), []byte("initial\n"), 0o644))
	nestedManifest := filepath.Join(repo, "apps", "web", "package-lock.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(nestedManifest), 0o755))
	require.NoError(t, os.WriteFile(nestedManifest, []byte("{}\n"), 0o644))
	gitTest(t, repo, "add", "README.md", "apps/web/package-lock.json")
	gitTest(t, repo, "commit", "-m", "initial")
	gitTest(t, repo, "remote", "add", "origin", remote)
	gitTest(t, repo, "push", "-u", "origin", "main")
	return repo
}

func TestRunSwitchDirectMatchesBranchAndPathWithoutFzf(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/direct")
	chdirForTest(t, repo)
	pathWithOnlyGit(t)

	for _, query := range []string{"feature/direct", worktree} {
		t.Run(query, func(t *testing.T) {
			buf := &bytes.Buffer{}
			command := &cobra.Command{}
			command.SetOut(buf)

			require.NoError(t, runSwitch(command, query, ""))
			assert.Equal(t, worktree+"\n", buf.String())
		})
	}
}

func pathWithOnlyGit(t *testing.T) {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	require.NoError(t, err)
	binDir := t.TempDir()
	require.NoError(t, os.Symlink(gitPath, filepath.Join(binDir, "git")))
	previous := os.Getenv("PATH")
	require.NoError(t, os.Setenv("PATH", binDir))
	t.Cleanup(func() { require.NoError(t, os.Setenv("PATH", previous)) })
}

func pathWithGitAndGH(t *testing.T) {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	require.NoError(t, err)
	binDir := t.TempDir()
	require.NoError(t, os.Symlink(gitPath, filepath.Join(binDir, "git")))
	script := `#!/bin/sh
printf '{"number":1,"title":"Nested module review","head":{"ref":"feature/review"}}'
`
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "gh"), []byte(script), 0o755))
	previous := os.Getenv("PATH")
	require.NoError(t, os.Setenv("PATH", binDir))
	t.Cleanup(func() { require.NoError(t, os.Setenv("PATH", previous)) })
}
