package launch_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shoutcape/treeman/internal/launch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	helperEnv    = "TREEMAN_LAUNCH_HELPER"
	helperDirEnv = "TREEMAN_LAUNCH_DIR"
	helperCmdEnv = "TREEMAN_LAUNCH_COMMAND"
)

// TestInDir_ReplacesTheProcess runs InDir in a child copy of this test binary,
// because a successful handover never returns to the caller.
func TestInDir_ReplacesTheProcess(t *testing.T) {
	if os.Getenv(helperEnv) == "1" {
		if err := launch.InDir(os.Getenv(helperDirEnv), os.Getenv(helperCmdEnv)); err != nil {
			os.Stderr.WriteString("handover failed: " + err.Error())
		}
		os.Stdout.WriteString("RETURNED")
		return
	}

	t.Run("runs the command in the directory", func(t *testing.T) {
		dir := t.TempDir()

		output, err := runHelper(t, dir, "printf handover > marker && pwd")
		require.NoError(t, err, output)

		marker, readErr := os.ReadFile(filepath.Join(dir, "marker"))
		require.NoError(t, readErr)
		assert.Equal(t, "handover", string(marker))
		assert.Contains(t, output, dir, "the command sees the worktree as its working directory")
		// TreeMan is gone, so neither its output nor the test binary's own
		// "PASS" line can follow the command.
		assert.NotContains(t, output, "RETURNED")
		assert.NotContains(t, output, "PASS")
	})

	t.Run("the command reports its own exit status", func(t *testing.T) {
		_, err := runHelper(t, t.TempDir(), "exit 3")

		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr)
		assert.Equal(t, 3, exitErr.ExitCode())
	})

	t.Run("reports a directory it cannot enter", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "absent")

		output, err := runHelper(t, missing, "true")

		require.NoError(t, err, output)
		assert.Contains(t, output, "could not enter")
		assert.Contains(t, output, "RETURNED")
	})
}

func runHelper(t *testing.T, dir, command string) (string, error) {
	t.Helper()
	helper := exec.Command(os.Args[0], "-test.run=TestInDir_ReplacesTheProcess")
	helper.Env = append(os.Environ(), helperEnv+"=1", helperDirEnv+"="+dir, helperCmdEnv+"="+command)
	output, err := helper.CombinedOutput()
	return string(output), err
}
