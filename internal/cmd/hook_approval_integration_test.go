package cmd

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shoutcape/treeman/internal/terminal"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreationCommandsRequireHookApprovalBeforeMutation(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		repo := createRemoteRepoWithNestedModule(t)
		configureHookApprovalRepo(t, repo, `echo created`)
		chdirForTest(t, repo)
		pathWithOnlyGit(t)

		cmd := interactiveApprovalCommand("n\n")
		err := runCreate(cmd, "feature/blocked", creationSetupOptions{skipEnv: true, skipDeps: true, skipDatabase: true}, "")
		require.ErrorContains(t, err, "hook approval refused")
		assertBranchAndWorktreeAbsent(t, repo, "feature/blocked", filepath.Join(repo, ".worktrees", "feature-blocked"))
	})

	t.Run("branch", func(t *testing.T) {
		repo := createRemoteRepoWithNestedModule(t)
		gitTest(t, repo, "checkout", "-b", "feature/blocked")
		gitTest(t, repo, "push", "-u", "origin", "feature/blocked")
		gitTest(t, repo, "checkout", "main")
		gitTest(t, repo, "branch", "-D", "feature/blocked")
		configureHookApprovalRepo(t, repo, `echo branched`)
		chdirForTest(t, repo)
		pathWithOnlyGit(t)

		err := runBranch(interactiveApprovalCommand("n\n"), "feature/blocked", creationSetupOptions{skipEnv: true, skipDeps: true, skipDatabase: true})
		require.ErrorContains(t, err, "hook approval refused")
		assertBranchAndWorktreeAbsent(t, repo, "feature/blocked", filepath.Join(repo, ".worktrees", "feature-blocked"))
	})

	t.Run("review", func(t *testing.T) {
		repo := createRemoteRepoWithNestedModule(t)
		gitTest(t, repo, "checkout", "-b", "feature/blocked-review")
		gitTest(t, repo, "push", "-u", "origin", "feature/blocked-review")
		gitTest(t, repo, "push", "origin", "feature/blocked-review:refs/pull/1/head")
		gitTest(t, repo, "checkout", "main")
		gitTest(t, repo, "branch", "-D", "feature/blocked-review")
		configureHookApprovalRepo(t, repo, `echo reviewed`)
		chdirForTest(t, repo)
		pathWithGitAndGH(t)
		t.Setenv("_TREEMAN_FORGE", "github")
		t.Setenv("_TREEMAN_GH_REPO", "owner/repo")

		err := runReview(interactiveApprovalCommand("n\n"), "1", creationSetupOptions{skipEnv: true, skipDeps: true, skipDatabase: true}, "")
		require.ErrorContains(t, err, "hook approval refused")
		assertBranchAndWorktreeAbsent(t, repo, "feature/blocked-review", filepath.Join(repo, ".worktrees", "feature-blocked-review"))
		assert.Error(t, runApprovalGit(repo, "show-ref", "--verify", "--quiet", "refs/pull/1/head"), "review ref must not be fetched before approval")
	})
}

func TestRunCreateNonInteractiveRequiresApprovalBeforeMutation(t *testing.T) {
	repo := createRemoteRepoWithNestedModule(t)
	configureHookApprovalRepo(t, repo, `echo created`)
	chdirForTest(t, repo)
	pathWithOnlyGit(t)

	cmd := commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString("y\n"))
	err := runCreate(cmd, "feature/noninteractive", creationSetupOptions{skipEnv: true, skipDeps: true, skipDatabase: true}, "")
	require.ErrorContains(t, err, "hook approval required")
	assertBranchAndWorktreeAbsent(t, repo, "feature/noninteractive", filepath.Join(repo, ".worktrees", "feature-noninteractive"))
}

func TestApprovedHookFailureIsWarningOnlyDuringCreation(t *testing.T) {
	repo := createRemoteRepoWithNestedModule(t)
	configureHookApprovalRepo(t, repo, `false`)
	chdirForTest(t, repo)
	pathWithOnlyGit(t)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

	err := runCreate(interactiveApprovalCommandTo("y\n", stdout, stderr), "feature/hook-warning", creationSetupOptions{
		skipEnv: true, skipDeps: true, skipDatabase: true,
	}, "")
	require.NoError(t, err)
	assert.DirExists(t, filepath.Join(repo, ".worktrees", "feature-hook-warning"))
	assert.Contains(t, stderr.String(), `hook "false" failed`)
	assert.Contains(t, stderr.String(), "Hooks")
	assert.Contains(t, stderr.String(), "failed")
}

func configureHookApprovalRepo(t *testing.T, repo, hook string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte("[hooks]\npost_create = [\""+hook+"\"]\n"), 0o600))
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}

func interactiveApprovalCommand(input string) *cobra.Command {
	return interactiveApprovalCommandTo(input, &bytes.Buffer{}, &bytes.Buffer{})
}

func interactiveApprovalCommandTo(input string, stdout, stderr *bytes.Buffer) *cobra.Command {
	cmd := commandWithApprovalInput(input, stderr)
	cmd.SetOut(stdout)
	interactive := terminal.Capabilities{InputTTY: true, OutputTTY: true, Interactive: true, Width: 120}
	cmd.SetContext(context.WithValue(context.Background(), terminalSessionKey{}, terminalSession{
		errorOutput: interactive,
		standardOut: interactive,
	}))
	return cmd
}

func assertBranchAndWorktreeAbsent(t *testing.T, repo, branch, worktree string) {
	t.Helper()
	assert.Error(t, runApprovalGit(repo, "show-ref", "--verify", "--quiet", "refs/heads/"+branch))
	assert.NoDirExists(t, worktree)
}

func runApprovalGit(repo string, args ...string) error {
	return exec.Command("git", append([]string{"-C", repo}, args...)...).Run()
}
