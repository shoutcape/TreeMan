package cmd

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteListHuman(t *testing.T) {
	buf := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(buf)

	writeListHuman(cmd, []listEntry{
		{Path: "/repo", Branch: "main", Main: true, Current: true},
		{Path: "/repo/.worktrees/feature", Branch: "feature", Dirty: true},
		{Path: "/repo/.worktrees/review", Detached: true},
	})

	assert.Equal(t, "\nWORKTREES\n\n    MARKERS  STATUS    BRANCH                       PATH                     \n    ───────  ──────    ───────────────────────────  ─────────────────────────\n    M▶       CLEAN     main                         /repo\n             DIRTY     feature                      /repo/.worktrees/feature\n             DETACHED  (detached)                   /repo/.worktrees/review\n", ui.StripANSI(buf.String()))
}

func TestWriteListJSON(t *testing.T) {
	buf := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(buf)
	entries := []listEntry{{Path: "/repo", Branch: "main", Main: true, Current: true}}

	require.NoError(t, writeListJSON(cmd, entries))
	var decoded []listEntry
	require.NoError(t, json.Unmarshal(buf.Bytes(), &decoded))
	assert.Equal(t, entries, decoded)
}

func TestListCmd_HasWTLAlias(t *testing.T) {
	cmd := newListCmd()

	assert.Contains(t, cmd.Aliases, "wtl")
}
