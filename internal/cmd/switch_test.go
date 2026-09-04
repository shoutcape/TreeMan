package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// symlinkedDir returns a real directory and a symlink that reaches it. It
// reproduces the platform layout that makes raw path comparison fail, such as
// /tmp on macOS.
func symlinkedDir(t *testing.T, name string) (string, string) {
	t.Helper()
	parent := t.TempDir()
	target := filepath.Join(parent, name)
	require.NoError(t, os.Mkdir(target, 0o755))
	link := filepath.Join(parent, name+"-link")
	require.NoError(t, os.Symlink(target, link))
	return target, link
}

// chdirThroughSymlink makes the process working directory reach dir through
// link. os.Getwd reports $PWD when it names the same directory.
func chdirThroughSymlink(t *testing.T, dir, link string) {
	t.Helper()
	chdirForTest(t, dir)
	t.Setenv("PWD", link)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.Equal(t, link, cwd)
}

func TestPrintSwitchDestination_SymlinkedCurrentDirectory(t *testing.T) {
	target, link := symlinkedDir(t, "worktree")
	chdirThroughSymlink(t, target, link)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	require.NoError(t, printSwitchDestination(commandWithOutput(stdout, stderr), target))

	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "Already in this worktree.")
}

func TestPrintSwitchDestination_SymlinkedDestination(t *testing.T) {
	target, link := symlinkedDir(t, "worktree")
	chdirForTest(t, target)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	require.NoError(t, printSwitchDestination(commandWithOutput(stdout, stderr), link))

	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "Already in this worktree.")
}

func TestPrintSwitchDestination_DifferentWorktreePrintsPath(t *testing.T) {
	target, link := symlinkedDir(t, "worktree")
	other := filepath.Join(filepath.Dir(target), "other")
	require.NoError(t, os.Mkdir(other, 0o755))
	chdirThroughSymlink(t, target, link)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	require.NoError(t, printSwitchDestination(commandWithOutput(stdout, stderr), other))

	assert.Equal(t, other+"\n", stdout.String())
	assert.Contains(t, stderr.String(), "cd .../other")
}

// A destination that cannot be resolved must still print, not fail.
func TestPrintSwitchDestination_UnresolvablePathStillPrints(t *testing.T) {
	chdirForTest(t, t.TempDir())
	missing := filepath.Join(t.TempDir(), "removed")

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	require.NoError(t, printSwitchDestination(commandWithOutput(stdout, stderr), missing))

	assert.Equal(t, missing+"\n", stdout.String())
	assert.Contains(t, stderr.String(), "cd .../removed")
}

func TestSamePath_UnresolvablePathsFallBackToRawComparison(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "removed")

	assert.True(t, samePath(missing, missing))
	assert.True(t, samePath(missing, missing+string(filepath.Separator)+"."))
	assert.False(t, samePath(missing, missing+"-other"))
}

func TestRunSwitch_SymlinkedQueryMatchesWorktree(t *testing.T) {
	repo, worktree := createTestWorktree(t, "feature/symlink")
	link := filepath.Join(filepath.Dir(worktree), "worktree-link")
	require.NoError(t, os.Symlink(worktree, link))
	chdirForTest(t, repo)
	pathWithOnlyGit(t)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	require.NoError(t, runSwitch(commandWithOutput(stdout, stderr), link, worktreeLaunchOptions{}))

	assert.Equal(t, worktree+"\n", stdout.String())
}

func TestRunSwitch_SymlinkedCurrentWorktreeReportsAlreadyThere(t *testing.T) {
	_, worktree := createTestWorktree(t, "feature/symlink")
	link := filepath.Join(filepath.Dir(worktree), "worktree-link")
	require.NoError(t, os.Symlink(worktree, link))
	chdirThroughSymlink(t, worktree, link)
	pathWithOnlyGit(t)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	require.NoError(t, runSwitch(commandWithOutput(stdout, stderr), "feature/symlink", worktreeLaunchOptions{}))

	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "Already in this worktree.")
}
