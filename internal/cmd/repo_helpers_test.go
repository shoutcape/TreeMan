package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openRepoForTest opens the repository containing dir, failing the test when
// it cannot.
func openRepoForTest(t *testing.T, dir string) git.Repo {
	t.Helper()
	repo, err := git.Open(dir)
	require.NoError(t, err)
	return repo
}

// currentRepoForTest opens the repository a command run from the current
// directory would open, for tests that changed into it.
func currentRepoForTest(t *testing.T) git.Repo {
	t.Helper()
	repo, err := openCurrentRepo()
	require.NoError(t, err)
	return repo
}

// chdirIntoDeletedDir leaves the process in a directory that no longer
// exists, the way a shell does after its directory is removed under it.
func chdirIntoDeletedDir(t *testing.T) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "gone")
	require.NoError(t, os.Mkdir(dir, 0o755))
	chdirForTest(t, dir)
	require.NoError(t, os.Remove(dir))
	if _, err := os.Getwd(); err == nil {
		t.Skip("this platform still reports a deleted working directory")
	}
}

func TestOpenCurrentRepoInADeletedDirectoryIsNotInRepository(t *testing.T) {
	chdirIntoDeletedDir(t)

	_, err := openCurrentRepo()

	assert.ErrorIs(t, err, errNotInRepository)
}

func TestDoctorInADeletedDirectoryReportsNoRepository(t *testing.T) {
	chdirIntoDeletedDir(t)
	t.Setenv("SHELL", "")
	stubLookPath(t, func(string) error { return nil })
	stubDockerDaemonReady(t, nil)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command := New("", "", "")
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs([]string{"doctor"})

	err := command.Execute()

	require.EqualError(t, err, "doctor found failed diagnostics; resolve them and rerun")
	assert.Contains(t, ui.StripANSI(stderr.String()), "✗  Repository           Not detected")
}

func TestPreflightInADeletedDirectoryFailsAtTheRepositoryStage(t *testing.T) {
	chdirIntoDeletedDir(t)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command := New("", "", "")
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs([]string{"preflight", "--json"})

	err := command.Execute()

	require.EqualError(t, err, "not inside a git repository")
	report := decodeReport(t, stdout)
	assert.False(t, report.OK)
	assert.Equal(t, []jsonCheck{{Name: "repository", Status: "fail", Message: "not inside a git repository", Hint: ""}}, report.Checks)
}
