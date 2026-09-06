package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestRepo builds a repository with one linked worktree, an environment
// file in the main worktree, and no installer on PATH.
func setupTestRepo(t *testing.T, branch string) (string, string) {
	t.Helper()
	return setupTestRepoWithTools(t, branch)
}

// setupTestRepoWithTools is setupTestRepo for a test whose hooks need more
// than Git. PATH holds exactly the named tools, so dependency detection still
// finds no installer and a hook can run only what the test allowed.
func setupTestRepoWithTools(t *testing.T, branch string, tools ...string) (string, string) {
	t.Helper()
	repo, worktree := createTestWorktree(t, branch)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env"), []byte("KEY=main\n"), 0o600))
	chdirForTest(t, repo)
	pathWithTools(t, append([]string{"git"}, tools...)...)
	return repo, worktree
}

func pathWithTools(t *testing.T, tools ...string) {
	t.Helper()
	binDir := t.TempDir()
	for _, tool := range tools {
		path, err := exec.LookPath(tool)
		require.NoError(t, err)
		require.NoError(t, os.Symlink(path, filepath.Join(binDir, tool)))
	}
	previous := os.Getenv("PATH")
	require.NoError(t, os.Setenv("PATH", binDir))
	t.Cleanup(func() { require.NoError(t, os.Setenv("PATH", previous)) })
}

// quietSetup is every step a targeting test does not care about.
func quietSetup() rerunSetupOptions {
	return rerunSetupOptions{skipEnv: true, skipDatabase: true, skipDeps: true, skipHooks: true}
}

func runSetupIn(t *testing.T, query string, options rerunSetupOptions) (string, string, error) {
	t.Helper()
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	err := runSetup(commandWithOutput(stdout, stderr), query, options)
	return stdout.String(), stderr.String(), err
}

func TestSetupSelectsTheCurrentWorktree(t *testing.T) {
	_, worktree := setupTestRepo(t, "feature/current")
	chdirForTest(t, worktree)

	_, _, err := runSetupIn(t, "", rerunSetupOptions{skipDatabase: true, skipDeps: true, skipHooks: true})
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(worktree, ".env"))
}

func TestSetupSelectsTheCurrentWorktreeFromASubdirectory(t *testing.T) {
	_, worktree := setupTestRepo(t, "feature/subdir")
	nested := filepath.Join(worktree, "a", "b")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	chdirForTest(t, nested)

	_, _, err := runSetupIn(t, "", rerunSetupOptions{skipDatabase: true, skipDeps: true, skipHooks: true})
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(worktree, ".env"))
}

func TestSetupSelectsAnExactTarget(t *testing.T) {
	_, worktree := setupTestRepo(t, "feature/exact")
	relative, err := filepath.Rel(mustGetwd(t), worktree)
	require.NoError(t, err)
	symlinked := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(worktree, symlinked))

	for name, query := range map[string]string{
		"branch":   "feature/exact",
		"path":     worktree,
		"relative": relative,
		"symlink":  symlinked,
	} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, os.RemoveAll(filepath.Join(worktree, ".env")))

			_, _, err := runSetupIn(t, query, rerunSetupOptions{skipDatabase: true, skipDeps: true, skipHooks: true})
			require.NoError(t, err)

			assert.FileExists(t, filepath.Join(worktree, ".env"))
		})
	}
}

func TestSetupRejectsTargetsItCannotRepair(t *testing.T) {
	repo, worktree := setupTestRepo(t, "feature/rejected")
	detached := filepath.Join(t.TempDir(), "detached")
	gitTest(t, repo, "worktree", "add", "--detach", detached)

	for name, target := range map[string]struct{ query, message string }{
		"main worktree":  {repo, "not a linked worktree"},
		"detached":       {detached, "detached HEAD"},
		"unregistered":   {t.TempDir(), "no worktree matches"},
		"unknown branch": {"feature/absent", "no worktree matches"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := runSetupIn(t, target.query, quietSetup())
			assert.ErrorContains(t, err, target.message)
		})
	}
	assert.DirExists(t, worktree)
}

func TestSetupRejectsAStaleRegistration(t *testing.T) {
	_, worktree := setupTestRepo(t, "feature/stale")
	require.NoError(t, os.RemoveAll(worktree))

	_, _, err := runSetupIn(t, "feature/stale", quietSetup())
	assert.ErrorContains(t, err, "directory is missing")
}

func TestSetupRejectsAnAmbiguousTarget(t *testing.T) {
	repo, _ := setupTestRepo(t, "feature/ambiguous")
	// "alpha" now names both a directory under the repository and a branch on
	// a different worktree, so one query matches two entries.
	gitTest(t, repo, "worktree", "add", "-b", "beta", filepath.Join(repo, "alpha"))
	gitTest(t, repo, "worktree", "add", "-b", "alpha", filepath.Join(repo, "other"))

	_, _, err := runSetupIn(t, "alpha", quietSetup())

	assert.ErrorContains(t, err, "matches more than one worktree")
}

func TestSetupRejectsContradictoryFlags(t *testing.T) {
	for name, args := range map[string][]string{
		"refresh and skip env":    {"--refresh-env", "--skip-env"},
		"rerun and skip hooks":    {"--rerun-hooks", "--skip-hooks"},
		"trust and skip hooks":    {"--trust-hooks", "--skip-hooks"},
		"trust without rerunning": {"--trust-hooks"},
	} {
		t.Run(name, func(t *testing.T) {
			cmd := newSetupCmd()
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(args)

			assert.Error(t, cmd.Execute())
		})
	}
}

func TestSetupReadsConfigurationFromTheMainRoot(t *testing.T) {
	repo, worktree := setupTestRepo(t, "feature/config")
	configureHookApprovalRepo(t, repo, "echo main")
	// The branch's own copy asks for a different command. Setup must not read
	// it, or a branch could name the commands it wants approved.
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".treeman.toml"), []byte("[hooks]\npost_create = [\"echo branch\"]\n"), 0o600))
	chdirForTest(t, worktree)

	stderr := &bytes.Buffer{}
	err := runSetup(interactiveApprovalCommandTo("n\n", &bytes.Buffer{}, stderr), "",
		rerunSetupOptions{skipEnv: true, skipDatabase: true, skipDeps: true, rerunHooks: true})

	require.ErrorContains(t, err, "hook approval refused")
	assert.Contains(t, stderr.String(), "echo main")
	assert.NotContains(t, stderr.String(), "echo branch")
}

func TestSetupWritesNothingToStandardOutput(t *testing.T) {
	_, worktree := setupTestRepo(t, "feature/stderr")
	chdirForTest(t, worktree)

	stdout, stderr, err := runSetupIn(t, "", rerunSetupOptions{skipDatabase: true, skipHooks: true})
	require.NoError(t, err)

	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "SETUP")
}

func TestSetupReportsNoDestination(t *testing.T) {
	_, worktree := setupTestRepo(t, "feature/nodest")
	destination := filepath.Join(t.TempDir(), "cd-file")
	t.Setenv(cdFileEnv, destination)
	chdirForTest(t, worktree)

	_, _, err := runSetupIn(t, "", quietSetup())
	require.NoError(t, err)

	assert.NoFileExists(t, destination)
}

func TestSetupLeavesBranchesAndRegistrationUnchanged(t *testing.T) {
	repo, worktree := setupTestRepo(t, "feature/unchanged")
	branchesBefore := gitTestOutput(t, repo, "branch", "--list")
	worktreesBefore := gitTestOutput(t, repo, "worktree", "list", "--porcelain")
	chdirForTest(t, worktree)

	_, _, err := runSetupIn(t, "", rerunSetupOptions{skipDatabase: true, skipHooks: true})
	require.NoError(t, err)

	assert.Equal(t, branchesBefore, gitTestOutput(t, repo, "branch", "--list"))
	assert.Equal(t, worktreesBefore, gitTestOutput(t, repo, "worktree", "list", "--porcelain"))
}

func TestSetupPreservesExistingEnvironmentFiles(t *testing.T) {
	repo, worktree := setupTestRepo(t, "feature/preserve")
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env.extra"), []byte("EXTRA=main\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte("KEY=branch\n"), 0o600))
	chdirForTest(t, worktree)

	_, stderr, err := runSetupIn(t, "", rerunSetupOptions{skipDatabase: true, skipDeps: true, skipHooks: true})
	require.NoError(t, err)

	assert.Equal(t, "KEY=branch\n", readTestFile(t, filepath.Join(worktree, ".env")))
	assert.Equal(t, "EXTRA=main\n", readTestFile(t, filepath.Join(worktree, ".env.extra")))
	assert.Contains(t, stderr, "Preserved existing .env")
	assert.Contains(t, stderr, "copied 1, preserved 1")
}

func TestSetupRefreshesEnvironmentFilesOnRequest(t *testing.T) {
	repo, worktree := setupTestRepo(t, "feature/refresh")
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env"), []byte("KEY=branch\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".env.local"), []byte("ONLY=branch\n"), 0o600))
	chdirForTest(t, worktree)

	_, _, err := runSetupIn(t, "", rerunSetupOptions{refreshEnv: true, skipDatabase: true, skipDeps: true, skipHooks: true})
	require.NoError(t, err)

	assert.Equal(t, readTestFile(t, filepath.Join(repo, ".env")), readTestFile(t, filepath.Join(worktree, ".env")))
	// A file the main worktree does not have is never removed by a refresh.
	assert.Equal(t, "ONLY=branch\n", readTestFile(t, filepath.Join(worktree, ".env.local")))
}

func TestSetupContinuesAfterAStepFails(t *testing.T) {
	repo, worktree := setupTestRepo(t, "feature/warning")
	// A source that is not a regular file fails that one file, and only it.
	require.NoError(t, os.Symlink("/nowhere", filepath.Join(repo, ".env.broken")))
	chdirForTest(t, worktree)

	_, stderr, err := runSetupIn(t, "", rerunSetupOptions{skipDatabase: true, skipHooks: true})
	require.NoError(t, err)

	assert.Contains(t, stderr, "could not copy .env.broken")
	assert.Contains(t, stderr, "Copied .env")
	assert.Contains(t, stderr, "Detecting dependencies")
}

func TestSetupSerializesRunsForOneWorktree(t *testing.T) {
	_, worktree := setupTestRepo(t, "feature/lock")

	var inner error
	require.NoError(t, withSetupLock(worktree, func() error {
		_, _, inner = runSetupIn(t, worktree, quietSetup())
		return nil
	}))

	assert.ErrorContains(t, inner, "another treeman setup is already running")
}

func TestSetupReleasesTheLockAfterAFailure(t *testing.T) {
	_, worktree := setupTestRepo(t, "feature/lock-release")

	require.Error(t, withSetupLock(worktree, func() error { return assert.AnError }))

	_, _, err := runSetupIn(t, worktree, quietSetup())
	assert.NoError(t, err)
}

func TestSetupAllowsConcurrentRunsInDifferentWorktrees(t *testing.T) {
	repo, first := setupTestRepo(t, "feature/one")
	second := filepath.Join(t.TempDir(), "second")
	gitTest(t, repo, "worktree", "add", "-b", "feature/two", second)

	var inner error
	require.NoError(t, withSetupLock(first, func() error {
		_, _, inner = runSetupIn(t, second, quietSetup())
		return nil
	}))

	assert.NoError(t, inner)
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	return dir
}
