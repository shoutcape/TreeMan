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

	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/worktree"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteBenchmarkRunsSetupAndDeletesEveryPreparedWorktree(t *testing.T) {
	fixture := newDeleteBenchmarkFixture(t, deleteBenchmarkProject{})

	fetchHeadPath := filepath.Join(fixture.repo, ".git", "FETCH_HEAD")
	require.NoError(t, os.WriteFile(fetchHeadPath, []byte("source fetch state\n"), 0o644))
	refsBefore := gitTestOutput(t, fixture.repo, "show-ref")
	worktreesBefore := gitTestOutput(t, fixture.repo, "worktree", "list", "--porcelain")

	require.NoError(t, runBenchmark(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), benchmarkRequest{target: "delete"}, 1, 2))

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
	fixture := newDeleteBenchmarkFixture(t, deleteBenchmarkProject{})

	runner, sandbox, err := newDeleteBenchmarkRunner(creationSetupOptions{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sandbox.close()) })

	run, err := runner(&cobra.Command{})
	require.NoError(t, err)

	preparedPath := fixture.lastPreparedPath(t)
	assert.FileExists(t, filepath.Join(preparedPath, ".env"))
	assert.FileExists(t, filepath.Join(preparedPath, "node_modules", "installed"))
	assert.FileExists(t, filepath.Join(preparedPath, ".benchmark-hook"))
	assert.Contains(t, readTestFile(t, filepath.Join(preparedPath, ".env")), "DATABASE_URL=postgres://payload:secret@localhost:5432/payload_")

	measurement, runErr := run()
	require.NoError(t, finishBenchmarkIteration(runErr, measurement.cleanup))
	assert.NoDirExists(t, preparedPath)
	gitTestFails(t, sandbox.repo, "show-ref", "--verify", "refs/heads/"+deleteBenchmarkBranch)
}

func TestDeleteBenchmarkOwnsConfiguredExternalDestination(t *testing.T) {
	external := filepath.Join(t.TempDir(), "production-worktrees")
	fixture := newDeleteBenchmarkFixture(t, deleteBenchmarkProject{worktreeDir: external})

	runner, sandbox, err := newDeleteBenchmarkRunner(creationSetupOptions{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sandbox.close()) })

	run, err := runner(&cobra.Command{})
	require.NoError(t, err)
	preparedPath := fixture.lastPreparedPath(t)
	assert.Equal(t, filepath.Join(sandbox.worktreeDir(), deleteBenchmarkBranch), preparedPath)
	assert.NoDirExists(t, external)

	measurement, runErr := run()
	require.NoError(t, finishBenchmarkIteration(runErr, measurement.cleanup))
}

func TestDeleteBenchmarkClearsSetupDriftBeforeTimedDeletion(t *testing.T) {
	fixture := newDeleteBenchmarkFixture(t, deleteBenchmarkProject{})

	runner, sandbox, err := newDeleteBenchmarkRunner(creationSetupOptions{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sandbox.close()) })

	run, err := runner(&cobra.Command{})
	require.NoError(t, err)

	// Setup rewrote a tracked file. The deletion being measured refuses a dirty
	// worktree without --force, so preparation has to hand the clock a clean
	// one or the benchmark could never run on a project like that.
	preparedPath := fixture.lastPreparedPath(t)
	assert.Empty(t, gitTestOutput(t, preparedPath, "status", "--porcelain", "--untracked-files=all"))

	// Ignored setup output must survive that cleanup: .env and node_modules are
	// bulk the deletion genuinely has to remove, and a real worktree still has
	// them when it is deleted.
	assert.FileExists(t, filepath.Join(preparedPath, ".env"))
	assert.FileExists(t, filepath.Join(preparedPath, "node_modules", "installed"))
	assert.FileExists(t, filepath.Join(preparedPath, ".benchmark-hook"))

	measurement, runErr := run()
	require.NoError(t, finishBenchmarkIteration(runErr, measurement.cleanup))
	assert.NoDirExists(t, preparedPath)
	gitTestFails(t, sandbox.repo, "show-ref", "--verify", "refs/heads/"+deleteBenchmarkBranch)
}

func TestDeleteBenchmarkCleansUpPreparationFailure(t *testing.T) {
	fixture := newDeleteBenchmarkFixture(t, deleteBenchmarkProject{failingHook: true})

	runner, sandbox, err := newDeleteBenchmarkRunner(creationSetupOptions{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sandbox.close()) })

	_, err = runner(&cobra.Command{})
	require.ErrorContains(t, err, "benchmark worktree setup failed: hooks")
	// A project whose setup cannot complete here is still worth measuring
	// without that step, so the abort names the flag that skips it.
	require.ErrorContains(t, err, "rerun with --skip-hooks")

	assert.NoDirExists(t, fixture.lastPreparedPath(t))
	gitTestFails(t, sandbox.repo, "show-ref", "--verify", "refs/heads/"+deleteBenchmarkBranch)
	dockerCalls := fixture.dockerCalls(t)
	assert.Equal(t, 1, strings.Count(dockerCalls, "CREATE DATABASE"))
	assert.Equal(t, 1, strings.Count(dockerCalls, "DROP DATABASE"))
	require.NoError(t, sandbox.close())
	assert.NoDirExists(t, sandbox.root)
}

func TestDeleteBenchmarkCleansUpDeletionFailure(t *testing.T) {
	fixture := newDeleteBenchmarkFixture(t, deleteBenchmarkProject{})

	runner, sandbox, err := newDeleteBenchmarkRunner(creationSetupOptions{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sandbox.close()) })

	run, err := runner(&cobra.Command{})
	require.NoError(t, err)

	previousRemove := removeWorktreeAndBranch
	failedOnce := false
	removeWorktreeAndBranch = func(mainRoot, path, branch, expectedSHA string, force bool) (git.RemoveWorktreeResult, error) {
		if !failedOnce {
			failedOnce = true
			return git.RemoveWorktreeResult{}, errors.New("injected removal failure")
		}
		return previousRemove(mainRoot, path, branch, expectedSHA, force)
	}
	t.Cleanup(func() { removeWorktreeAndBranch = previousRemove })

	measurement, runErr := run()
	require.ErrorContains(t, runErr, "injected removal failure")
	require.NoError(t, measurement.cleanup())

	assert.NoDirExists(t, fixture.lastPreparedPath(t))
	gitTestFails(t, sandbox.repo, "show-ref", "--verify", "refs/heads/"+deleteBenchmarkBranch)
	assert.Equal(t, 1, strings.Count(fixture.dockerCalls(t), "DROP DATABASE"))
	require.NoError(t, sandbox.close())
	assert.NoDirExists(t, sandbox.root)
}

func TestDeleteBenchmarkRunsInARepositoryWithoutARemote(t *testing.T) {
	fixture := newDeleteBenchmarkFixture(t, deleteBenchmarkProject{})
	// Nothing the delete benchmark measures comes from the remote, so a
	// repository that has none -- or none reachable -- still gets a number.
	gitTest(t, fixture.repo, "remote", "remove", "origin")

	var stderr bytes.Buffer
	require.NoError(t, runBenchmark(commandWithOutput(&bytes.Buffer{}, &stderr), benchmarkRequest{target: "delete"}, 0, 1))

	preparedPaths := fixture.preparedPaths(t)
	require.Len(t, preparedPaths, 1)
	assert.NoDirExists(t, preparedPaths[0])
	assert.Contains(t, stderr.String(), "run  1/1")
}

func TestDeleteBenchmarkRejectsARepositoryWithNoCommits(t *testing.T) {
	empty := t.TempDir()
	gitTest(t, empty, "init", "--initial-branch=main")
	chdirForTest(t, empty)

	err := runBenchmark(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), benchmarkRequest{target: "delete"}, 0, 1)
	require.ErrorContains(t, err, "has no commits to create a worktree from")
}

func TestDeleteBenchmarkReportsWhatEachIterationWasPrepared(t *testing.T) {
	newDeleteBenchmarkFixture(t, deleteBenchmarkProject{})

	var stderr bytes.Buffer
	require.NoError(t, runBenchmark(commandWithOutput(&bytes.Buffer{}, &stderr), benchmarkRequest{target: "delete"}, 0, 1))

	// What a deletion costs is decided by what setup put in the worktree, so
	// the report says which steps ran rather than leaving the number to be
	// compared against one prepared differently.
	assert.Contains(t, stderr.String(),
		"prepared: environment completed, dependencies completed, database completed, hooks completed")
}

func TestDeleteBenchmarkReportsSetupOutputItHadToClear(t *testing.T) {
	newDeleteBenchmarkFixture(t, deleteBenchmarkProject{unignoredOutput: true})

	var stderr bytes.Buffer
	require.NoError(t, runBenchmark(commandWithOutput(&bytes.Buffer{}, &stderr), benchmarkRequest{target: "delete"}, 0, 1))

	// Setup output the project does not ignore has to go before the timed,
	// non-forced deletion can run, which leaves that deletion less to remove
	// than the same deletion faces in the project itself. The report says so.
	assert.Contains(t, stderr.String(), "cleared 1 untracked setup path(s)")
}

func TestDeleteBenchmarkSkipsRequestedSetupSteps(t *testing.T) {
	fixture := newDeleteBenchmarkFixture(t, deleteBenchmarkProject{failingHook: true})

	// The hook cannot succeed here, but the deletion of everything else is
	// still worth measuring.
	runner, sandbox, err := newDeleteBenchmarkRunner(creationSetupOptions{skipHooks: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sandbox.close()) })

	run, err := runner(&cobra.Command{})
	require.NoError(t, err)

	measurement, runErr := run()
	require.NoError(t, finishBenchmarkIteration(runErr, measurement.cleanup))
	assert.Contains(t, measurement.preparation, "hooks skipped")
	assert.Contains(t, measurement.preparation, "dependencies completed")
	assert.NoDirExists(t, worktree.PathForBranch(sandbox.worktreeDir(), deleteBenchmarkBranch))
	gitTestFails(t, sandbox.repo, "show-ref", "--verify", "refs/heads/"+deleteBenchmarkBranch)
	// Skipping one step leaves the rest of the measured path intact: the
	// branch database is still created and still dropped by the deletion.
	dockerCalls := fixture.dockerCalls(t)
	assert.Equal(t, 1, strings.Count(dockerCalls, "CREATE DATABASE"))
	assert.Equal(t, 1, strings.Count(dockerCalls, "DROP DATABASE"))
}

// deleteBenchmarkFixture is a repository configured for the delete benchmark
// plus the logs its stubbed post-create hook and docker CLI write to.
type deleteBenchmarkFixture struct {
	repo           string
	preparationLog string
	dockerLog      string
}

// deleteBenchmarkProject describes the project the fixture repository stands
// for, so a test can pick the shape of project whose behaviour it is about.
type deleteBenchmarkProject struct {
	// failingHook makes the post-create hook fail, the shape of project whose
	// setup cannot complete on this machine.
	failingHook bool
	// unignoredOutput makes the post-create hook write output the project does
	// not ignore, the shape of project where clearing setup drift also removes
	// what the deletion would have had to remove.
	unignoredOutput bool
	// worktreeDir is committed as the project's production destination. The
	// benchmark must retain ownership by overriding it inside its sandbox.
	worktreeDir string
}

func newDeleteBenchmarkFixture(t *testing.T, project deleteBenchmarkProject) deleteBenchmarkFixture {
	t.Helper()
	fixture := deleteBenchmarkFixture{
		repo:           createDeleteBenchmarkRepo(t, project),
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

func createDeleteBenchmarkRepo(t *testing.T, project deleteBenchmarkProject) string {
	t.Helper()
	repo := createRemoteRepoWithNestedModule(t)
	hook := `test -f node_modules/installed && touch .benchmark-hook && pwd >> "$PREPARATION_LOG"`
	if project.unignoredOutput {
		hook += " && mkdir -p generated && touch generated/artifact"
	}
	if project.failingHook {
		hook += " && false"
	}
	config := fmt.Sprintf(`worktree_dir = %q

[database]
env_key = "DATABASE_URL"
container = "payload-testing-lab-postgres"

[hooks]
post_create = [%q]
`, project.worktreeDir, hook)
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
