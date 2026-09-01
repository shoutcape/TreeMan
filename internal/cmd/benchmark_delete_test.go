package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteBenchmarkRunsSetupAndDeletesEveryPreparedWorktree(t *testing.T) {
	fixture := newDeleteBenchmarkFixture(t, false)

	fetchHeadPath := filepath.Join(fixture.repo, ".git", "FETCH_HEAD")
	require.NoError(t, os.WriteFile(fetchHeadPath, []byte("source fetch state\n"), 0o644))
	refsBefore := gitTestOutput(t, fixture.repo, "show-ref")
	worktreesBefore := gitTestOutput(t, fixture.repo, "worktree", "list", "--porcelain")

	require.NoError(t, runBenchmark(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), "delete", "", 1, 2))

	preparedPaths := fixture.preparedPaths(t)
	require.Len(t, preparedPaths, 3)
	for _, path := range preparedPaths {
		assert.NoDirExists(t, path)
	}
	assert.Equal(t, refsBefore, gitTestOutput(t, fixture.repo, "show-ref"))
	assert.Equal(t, worktreesBefore, gitTestOutput(t, fixture.repo, "worktree", "list", "--porcelain"))
	assert.Equal(t, "source fetch state\n", readTestFile(t, fetchHeadPath))

	dockerCalls := fixture.dockerCalls(t)
	assert.Equal(t, 3, strings.Count(dockerCalls, "CREATE DATABASE"))
	assert.Equal(t, 3, strings.Count(dockerCalls, "DROP DATABASE"))
}

func TestDeleteBenchmarkPreparesArtifactsBeforeTimedDeletion(t *testing.T) {
	fixture := newDeleteBenchmarkFixture(t, false)

	runner, sandbox, err := newDeleteBenchmarkRunner()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sandbox.close()) })

	iteration, err := runner(&cobra.Command{})
	require.NoError(t, err)

	preparedPath := fixture.lastPreparedPath(t)
	assert.FileExists(t, filepath.Join(preparedPath, ".env"))
	assert.FileExists(t, filepath.Join(preparedPath, "node_modules", "installed"))
	assert.FileExists(t, filepath.Join(preparedPath, ".benchmark-hook"))
	assert.Contains(t, readTestFile(t, filepath.Join(preparedPath, ".env")), "DATABASE_URL=postgres://payload:secret@localhost:5432/payload_")

	require.NoError(t, finishBenchmarkIteration(iteration.run(), iteration.cleanup))
	assert.NoDirExists(t, preparedPath)
	gitTestFails(t, sandbox.repo, "show-ref", "--verify", "refs/heads/"+deleteBenchmarkBranch)
}

func TestDeleteBenchmarkClearsSetupDriftBeforeTimedDeletion(t *testing.T) {
	fixture := newDeleteBenchmarkFixture(t, false)

	runner, sandbox, err := newDeleteBenchmarkRunner()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sandbox.close()) })

	iteration, err := runner(&cobra.Command{})
	require.NoError(t, err)

	// Setup rewrote a tracked file. The deletion being measured refuses a dirty
	// worktree without --force, so preparation has to hand the clock a clean
	// one or the benchmark could never run on a project like that.
	preparedPath := fixture.lastPreparedPath(t)
	assert.Empty(t, gitTestOutput(t, preparedPath, "status", "--porcelain", "--untracked-files=all"))

	// Ignored setup output must survive that cleanup: the deletion reads
	// DATABASE_URL from .env to drop the branch database, and node_modules is
	// bulk the deletion genuinely has to remove.
	assert.FileExists(t, filepath.Join(preparedPath, ".env"))
	assert.FileExists(t, filepath.Join(preparedPath, "node_modules", "installed"))
	assert.FileExists(t, filepath.Join(preparedPath, ".benchmark-hook"))

	require.NoError(t, finishBenchmarkIteration(iteration.run(), iteration.cleanup))
	assert.NoDirExists(t, preparedPath)
	gitTestFails(t, sandbox.repo, "show-ref", "--verify", "refs/heads/"+deleteBenchmarkBranch)
}

func TestDeleteBenchmarkCleansUpPreparationFailure(t *testing.T) {
	fixture := newDeleteBenchmarkFixture(t, true)

	runner, sandbox, err := newDeleteBenchmarkRunner()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sandbox.close()) })

	_, err = runner(&cobra.Command{})
	require.ErrorContains(t, err, "benchmark worktree setup failed: hooks")

	assert.NoDirExists(t, fixture.lastPreparedPath(t))
	gitTestFails(t, sandbox.repo, "show-ref", "--verify", "refs/heads/"+deleteBenchmarkBranch)
	dockerCalls := fixture.dockerCalls(t)
	assert.Equal(t, 1, strings.Count(dockerCalls, "CREATE DATABASE"))
	assert.Equal(t, 1, strings.Count(dockerCalls, "DROP DATABASE"))
	require.NoError(t, sandbox.close())
	assert.NoDirExists(t, sandbox.root)
}

func TestDeleteBenchmarkCleansUpDeletionFailure(t *testing.T) {
	fixture := newDeleteBenchmarkFixture(t, false)

	runner, sandbox, err := newDeleteBenchmarkRunner()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sandbox.close()) })

	iteration, err := runner(&cobra.Command{})
	require.NoError(t, err)

	previousRemove := removeWorktree
	failedOnce := false
	removeWorktree = func(path string, force bool) error {
		if !failedOnce {
			failedOnce = true
			return errors.New("injected removal failure")
		}
		return previousRemove(path, force)
	}
	t.Cleanup(func() { removeWorktree = previousRemove })

	runErr := iteration.run()
	require.ErrorContains(t, runErr, "injected removal failure")
	require.NoError(t, iteration.cleanup())

	assert.NoDirExists(t, fixture.lastPreparedPath(t))
	gitTestFails(t, sandbox.repo, "show-ref", "--verify", "refs/heads/"+deleteBenchmarkBranch)
	assert.Equal(t, 1, strings.Count(fixture.dockerCalls(t), "DROP DATABASE"))
	require.NoError(t, sandbox.close())
	assert.NoDirExists(t, sandbox.root)
}

// deleteBenchmarkFixture is a repository configured for the delete benchmark
// plus the logs its stubbed post-create hook and docker CLI write to.
type deleteBenchmarkFixture struct {
	repo           string
	preparationLog string
	dockerLog      string
}

func newDeleteBenchmarkFixture(t *testing.T, failingHook bool) deleteBenchmarkFixture {
	t.Helper()
	fixture := deleteBenchmarkFixture{
		repo:           createDeleteBenchmarkRepo(t, failingHook),
		preparationLog: filepath.Join(t.TempDir(), "preparations.log"),
		dockerLog:      filepath.Join(t.TempDir(), "docker.log"),
	}
	t.Setenv("PREPARATION_LOG", fixture.preparationLog)
	installDeleteBenchmarkTools(t, fixture.dockerLog)
	chdirForTest(t, fixture.repo)
	return fixture
}

// preparedPaths are the worktrees the post-create hook saw, one per prepared
// iteration, in preparation order.
func (fixture deleteBenchmarkFixture) preparedPaths(t *testing.T) []string {
	t.Helper()
	return nonEmptyLines(readTestFile(t, fixture.preparationLog))
}

func (fixture deleteBenchmarkFixture) lastPreparedPath(t *testing.T) string {
	t.Helper()
	paths := fixture.preparedPaths(t)
	require.NotEmpty(t, paths, "the post-create hook recorded no prepared worktree")
	return paths[len(paths)-1]
}

func (fixture deleteBenchmarkFixture) dockerCalls(t *testing.T) string {
	t.Helper()
	return readTestFile(t, fixture.dockerLog)
}

func createDeleteBenchmarkRepo(t *testing.T, failingHook bool) string {
	t.Helper()
	repo := createRemoteRepoWithNestedModule(t)
	hook := `test -f node_modules/installed && touch .benchmark-hook && pwd >> "$PREPARATION_LOG"`
	if failingHook {
		hook += " && false"
	}
	config := fmt.Sprintf(`[database]
env_key = "DATABASE_URL"
container = "payload-testing-lab-postgres"

[hooks]
post_create = [%q]
`, hook)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte(config), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "package-lock.json"), []byte("{}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(".env\nnode_modules/\n.benchmark-hook\n"), 0o644))
	gitTest(t, repo, "add", ".treeman.toml", "package-lock.json", ".gitignore")
	gitTest(t, repo, "commit", "-m", "add benchmark setup")
	gitTest(t, repo, "push", "origin", "main")
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env"), []byte("DATABASE_URL=postgres://payload:secret@localhost:5432/payload\n"), 0o600))
	return repo
}

func installDeleteBenchmarkTools(t *testing.T, dockerLog string) {
	t.Helper()
	originalPath := os.Getenv("PATH")
	gitPath, err := exec.LookPath("git")
	require.NoError(t, err)
	binDir := t.TempDir()
	require.NoError(t, os.Symlink(gitPath, filepath.Join(binDir, "git")))
	// Real npm rewrites package-lock.json, a tracked file, whenever the local
	// npm disagrees with the committed lockfile. The stub does the same so the
	// fixture reproduces the drift setup leaves behind.
	npmScript := `#!/bin/sh
mkdir -p node_modules
touch node_modules/installed
printf '{"lockfileVersion": 3}\n' > package-lock.json
`
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "npm"), []byte(npmScript), 0o755))
	dockerScript := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "ps" ]; then
  printf '%%s\n' '{"ID":"postgres-container-id","Names":"payload-testing-lab-postgres","Image":"postgres:16","Ports":"0.0.0.0:5432->5432/tcp"}'
  exit 0
fi
printf '%%s\n' "$*" >> %q
`, dockerLog)
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "docker"), []byte(dockerScript), 0o755))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+originalPath)
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(content)
}

func nonEmptyLines(value string) []string {
	var lines []string
	for _, line := range strings.Split(value, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
