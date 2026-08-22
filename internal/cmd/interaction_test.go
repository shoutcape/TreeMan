package cmd

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/shoutcape/treeman/internal/forge"
	"github.com/shoutcape/treeman/internal/terminal"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfirmYNRejectsNonInteractiveInput(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("y\n"))
	cmd.SetErr(&bytes.Buffer{})

	confirmed, err := confirmYN(cmd, "Remove these worktrees and branches? [y/N] ")

	assert.False(t, confirmed)
	require.EqualError(t, err, "confirmation required; rerun with --yes")
}

func TestConfirmYNAcceptsInteractiveYes(t *testing.T) {
	previousCapabilities := terminalCapabilities
	terminalCapabilities = func(io.Reader, io.Writer) terminal.Capabilities {
		return terminal.Capabilities{Interactive: true}
	}
	t.Cleanup(func() { terminalCapabilities = previousCapabilities })

	stderr := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("Y\n"))
	cmd.SetErr(stderr)

	confirmed, err := confirmYN(cmd, "Remove these worktrees and branches? [y/N] ")

	require.NoError(t, err)
	assert.True(t, confirmed)
	assert.Equal(t, "Remove these worktrees and branches? [y/N] ", stderr.String())
}

func TestPickBranchBypassesInteractionForExactMatch(t *testing.T) {
	cmd := &cobra.Command{}
	branch, err := pickBranch(cmd, []forge.BranchInfo{{Name: "feature/exact"}}, "feature/exact", nil)

	require.NoError(t, err)
	assert.Equal(t, "feature/exact", branch.Name)
}

func TestPickBranchExplainsUnavailableInteraction(t *testing.T) {
	cmd := &cobra.Command{}
	_, err := pickBranch(cmd, []forge.BranchInfo{{Name: "feature/exact"}}, "", nil)

	require.EqualError(t, err, "interactive selection is unavailable; pass an exact branch name")
}

func TestTerminalCapabilitiesAreCachedPerCommandStream(t *testing.T) {
	previousCapabilities := terminalCapabilities
	var calls int
	terminalCapabilities = func(io.Reader, io.Writer) terminal.Capabilities {
		calls++
		return terminal.Capabilities{}
	}
	t.Cleanup(func() { terminalCapabilities = previousCapabilities })

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	_ = commandRenderer(cmd)
	_ = outputRenderer(cmd)
	assert.False(t, canInteract(cmd))

	assert.Equal(t, 2, calls)
}
