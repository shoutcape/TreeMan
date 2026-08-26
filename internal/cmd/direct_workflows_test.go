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
	parent := t.TempDir()
	remote := filepath.Join(parent, "remote.git")
	repo := filepath.Join(parent, "repo")
	gitTest(t, parent, "init", "--bare", remote)
	gitTest(t, parent, "init", "-b", "main", repo)
	gitTest(t, repo, "config", "user.name", "TreeMan Test")
	gitTest(t, repo, "config", "user.email", "test@example.com")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "README.md"), []byte("initial\n"), 0o644))
	gitTest(t, repo, "add", "README.md")
	gitTest(t, repo, "commit", "-m", "initial")
	gitTest(t, repo, "remote", "add", "origin", remote)
	gitTest(t, repo, "push", "-u", "origin", "main")
	gitTest(t, repo, "checkout", "-b", "feature/direct")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "Cargo.toml"), []byte("[package]\nname = \"example\"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "package-lock.json"), []byte("{}\n"), 0o644))
	gitTest(t, repo, "add", "Cargo.toml", "package-lock.json")
	gitTest(t, repo, "commit", "-m", "add dependencies")
	gitTest(t, repo, "push", "-u", "origin", "feature/direct")
	gitTest(t, repo, "checkout", "main")
	gitTest(t, repo, "branch", "-D", "feature/direct")
	chdirForTest(t, repo)
	pathWithOnlyGit(t)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command := commandWithOutput(stdout, stderr)
	require.NoError(t, runBranch(command, "feature/direct"))

	worktree := filepath.Join(repo, ".worktrees", "feature-direct")
	assert.Equal(t, worktree+"\n", stdout.String())
	assert.DirExists(t, worktree)
	output := stderr.String()
	assert.Contains(t, output, "dependency installation failed: package-lock.json found but npm is not installed, skipping")
	assert.Contains(t, output, "Unsupported dependency manifests were not bootstrapped: Cargo.toml")
	assert.Contains(t, output, "Worktree setup incomplete:")
	assert.NotContains(t, output, "Worktree ready:")
}

func TestRunBranchReportsUnsupportedDependencyManifests(t *testing.T) {
	repo, _ := createRemoteBranchWithCargoManifest(t, "feature/cargo")
	chdirForTest(t, repo)
	pathWithOnlyGit(t)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command := commandWithOutput(stdout, stderr)
	require.NoError(t, runBranch(command, "feature/cargo"))

	output := ui.StripANSI(stderr.String())
	assert.Contains(t, output, "Unsupported dependency manifests were not bootstrapped: Cargo.toml")
	assert.NotContains(t, output, "No known dependency file detected")
	assert.Contains(t, output, "Worktree setup incomplete:")
	assert.NotContains(t, output, "Worktree ready:")
}

func TestRunReviewReportsUnsupportedDependencyManifests(t *testing.T) {
	repo, remote := createRemoteBranchWithCargoManifest(t, "feature/cargo-review")
	runGit(t, "--git-dir="+remote, "update-ref", "refs/pull/1/head", "refs/heads/feature/cargo-review")
	chdirForTest(t, repo)
	t.Setenv("_TREEMAN_REMOTE_URL", "https://github.com/owner/repo.git")

	binDir := t.TempDir()
	gh := filepath.Join(binDir, "gh")
	require.NoError(t, os.WriteFile(gh, []byte("#!/bin/sh\nprintf '%s\\n' '{\"number\":1,\"title\":\"Cargo review\",\"head\":{\"ref\":\"feature/cargo-review\"}}'\n"), 0o755))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command := commandWithOutput(stdout, stderr)
	require.NoError(t, runReview(command, "1", creationSetupOptions{}))

	output := ui.StripANSI(stderr.String())
	assert.Contains(t, output, "Unsupported dependency manifests were not bootstrapped: Cargo.toml")
	assert.NotContains(t, output, "No known dependency file detected")
	assert.Contains(t, output, "Review worktree setup incomplete:")
	assert.NotContains(t, output, "Review worktree ready:")
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

			require.NoError(t, runSwitch(command, query))
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

func createRemoteBranchWithCargoManifest(t *testing.T, branch string) (string, string) {
	t.Helper()
	parent := t.TempDir()
	remote := filepath.Join(parent, "remote.git")
	repo := filepath.Join(parent, "repo")
	gitTest(t, parent, "init", "--bare", remote)
	gitTest(t, parent, "init", "-b", "main", repo)
	gitTest(t, repo, "config", "user.name", "TreeMan Test")
	gitTest(t, repo, "config", "user.email", "test@example.com")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "Cargo.toml"), []byte("[package]\nname = \"example\"\n"), 0o644))
	gitTest(t, repo, "add", "Cargo.toml")
	gitTest(t, repo, "commit", "-m", "initial")
	gitTest(t, repo, "remote", "add", "origin", remote)
	gitTest(t, repo, "push", "-u", "origin", "main")
	gitTest(t, repo, "checkout", "-b", branch)
	gitTest(t, repo, "push", "-u", "origin", branch)
	gitTest(t, repo, "checkout", "main")
	gitTest(t, repo, "branch", "-D", branch)

	return repo, remote
}
