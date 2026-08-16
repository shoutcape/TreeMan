package cmd

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stubDatabaseRepair(t *testing.T, entries []branchDatabase, dropErr error) *[]string {
	t.Helper()
	originalDiscover := discoverBranchDatabasesFn
	originalDrop := dropRepairDatabaseFn
	dropped := &[]string{}
	discoverBranchDatabasesFn = func() ([]branchDatabase, error) { return entries, nil }
	dropRepairDatabaseFn = func(_, _, name string) error {
		*dropped = append(*dropped, name)
		return dropErr
	}
	t.Cleanup(func() {
		discoverBranchDatabasesFn = originalDiscover
		dropRepairDatabaseFn = originalDrop
	})
	return dropped
}

func TestDatabaseInspectIsReadOnly(t *testing.T) {
	stubDatabaseRepair(t, []branchDatabase{{Name: "app__feature", Branch: "feature", Worktree: "/repo/.worktrees/feature", Active: true}}, nil)
	buf := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(buf)

	require.NoError(t, newDatabaseInspectCmd().RunE(cmd, nil))
	assert.Equal(t, "app__feature\tactive\tfeature\t/repo/.worktrees/feature\n", buf.String())
}

func TestRunDatabaseRepair(t *testing.T) {
	orphan := branchDatabase{Name: "app__feature", Branch: "feature", Worktree: "/repo/.worktrees/feature", Container: "pg", BaseURI: "postgres://postgres@host"}

	t.Run("confirmation declined", func(t *testing.T) {
		dropped := stubDatabaseRepair(t, []branchDatabase{orphan}, nil)
		cmd := &cobra.Command{}
		cmd.SetIn(bytes.NewBufferString("n\n"))
		require.NoError(t, runDatabaseRepair(cmd, orphan.Name, false))
		assert.Empty(t, *dropped)
	})

	t.Run("confirmed orphan removed", func(t *testing.T) {
		dropped := stubDatabaseRepair(t, []branchDatabase{orphan}, nil)
		require.NoError(t, runDatabaseRepair(&cobra.Command{}, orphan.Name, true))
		assert.Equal(t, []string{orphan.Name}, *dropped)
	})

	t.Run("active database rejected", func(t *testing.T) {
		active := orphan
		active.Active = true
		stubDatabaseRepair(t, []branchDatabase{active}, nil)
		err := runDatabaseRepair(&cobra.Command{}, active.Name, true)
		require.EqualError(t, err, fmt.Sprintf("database %q is not a repairable TreeMan orphan", active.Name))
	})

	t.Run("unowned database rejected", func(t *testing.T) {
		stubDatabaseRepair(t, []branchDatabase{orphan}, nil)
		err := runDatabaseRepair(&cobra.Command{}, "unrelated", true)
		require.EqualError(t, err, `database "unrelated" is not a repairable TreeMan orphan`)
	})
}
