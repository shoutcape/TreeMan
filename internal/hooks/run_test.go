package hooks

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunPostCreate_Empty(t *testing.T) {
	results := RunPostCreate(t.TempDir(), nil)
	assert.Nil(t, results)
}

func TestRunPostCreate_EmptySlice(t *testing.T) {
	results := RunPostCreate(t.TempDir(), []string{})
	assert.Nil(t, results)
}

func TestRunPostCreate_SuccessfulCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test not applicable on Windows")
	}

	dir := t.TempDir()
	results := RunPostCreate(dir, []string{"echo hello"})

	require.Len(t, results, 1)
	assert.Equal(t, "echo hello", results[0].Command)
	assert.NoError(t, results[0].Err)
}

func TestRunPostCreate_CommandCreatesFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test not applicable on Windows")
	}

	dir := t.TempDir()
	marker := filepath.Join(dir, "hook-ran.txt")
	results := RunPostCreate(dir, []string{"touch " + marker})

	require.Len(t, results, 1)
	assert.NoError(t, results[0].Err)

	_, err := os.Stat(marker)
	assert.NoError(t, err, "hook should have created the marker file")
}

func TestRunPostCreate_RunsInWorkdir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test not applicable on Windows")
	}

	dir := t.TempDir()
	// Create a file using a relative path -- proves the command runs in dir.
	results := RunPostCreate(dir, []string{"touch ran-here.txt"})

	require.Len(t, results, 1)
	assert.NoError(t, results[0].Err)

	_, err := os.Stat(filepath.Join(dir, "ran-here.txt"))
	assert.NoError(t, err, "command should have run in the worktree directory")
}

func TestRunPostCreate_FailingCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test not applicable on Windows")
	}

	dir := t.TempDir()
	results := RunPostCreate(dir, []string{"false"})

	require.Len(t, results, 1)
	assert.Error(t, results[0].Err)
	assert.Contains(t, results[0].Err.Error(), "false")
}

func TestRunPostCreate_ContinuesAfterFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test not applicable on Windows")
	}

	dir := t.TempDir()
	marker := filepath.Join(dir, "second-ran.txt")
	results := RunPostCreate(dir, []string{
		"false",
		"touch " + marker,
	})

	require.Len(t, results, 2)
	assert.Error(t, results[0].Err, "first command should fail")
	assert.NoError(t, results[1].Err, "second command should succeed")

	_, err := os.Stat(marker)
	assert.NoError(t, err, "second hook should have run despite first failing")
}

func TestRunPostCreate_MultipleSuccessful(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test not applicable on Windows")
	}

	dir := t.TempDir()
	results := RunPostCreate(dir, []string{
		"touch first.txt",
		"touch second.txt",
		"touch third.txt",
	})

	require.Len(t, results, 3)
	for _, r := range results {
		assert.NoError(t, r.Err)
	}

	for _, name := range []string{"first.txt", "second.txt", "third.txt"} {
		_, err := os.Stat(filepath.Join(dir, name))
		assert.NoError(t, err, "%s should exist", name)
	}
}
