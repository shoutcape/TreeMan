package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitCmd_Bash(t *testing.T) {
	root := New("test", "abc123", "2026-01-01")
	buf := &bytes.Buffer{}
	root.SetOut(buf)

	root.SetArgs([]string{"init", "bash"})
	err := root.Execute()
	require.NoError(t, err)

	out := buf.String()
	assert.NotContains(t, out, "\x1b")
	assert.Contains(t, out, "treeman shell init bash")
	assert.Contains(t, out, "treeman()")
	assert.Contains(t, out, "command treeman \"$@\"")
	assert.Contains(t, out, "wt() { treeman create \"$@\"; }")
	assert.Contains(t, out, "wtpr() { treeman review \"$@\"; }")
	assert.Contains(t, out, "wtmr() { treeman review \"$@\"; }")
	assert.Contains(t, out, "wts() { treeman switch \"$@\"; }")
	assert.Contains(t, out, "wtl() { treeman list \"$@\"; }")
	assert.Contains(t, out, "wtc() { treeman clean \"$@\"; }")
	assert.Contains(t, out, "wtd() { treeman delete \"$@\"; }")
	assert.NotContains(t, out, "lg()")
	assert.NotContains(t, out, "wto()")
	assert.Contains(t, out, "create|branch|review|switch|clean|delete|wtb|wtpr|wtmr|wts|wtc|wtd)")
	// TreeMan reports where to cd through a file, so the wrapper never runs it
	// inside command substitution and never has to read its flags.
	assert.Contains(t, out, "TREEMAN_CD_FILE=\"$_tm_file\" command treeman \"$@\"")
	assert.Contains(t, out, "cd -- \"$(cat \"$_tm_file\")\"")
	assert.NotContains(t, out, "$(command treeman")
	assert.NotContains(t, out, "_tm_arg", "the wrapper does not scan TreeMan's arguments")
}

func TestRootCmd_HasNoOpenCommand(t *testing.T) {
	root := New("test", "", "")
	cmd, _, err := root.Find([]string{"open"})

	assert.Error(t, err)
	assert.Equal(t, root, cmd)
}

func TestRootCmd_HasDoctorCommand(t *testing.T) {
	root := New("test", "", "")
	command, _, err := root.Find([]string{"doctor"})

	require.NoError(t, err)
	assert.Equal(t, "doctor", command.Name())
}

func TestRootCmd_VersionFlags(t *testing.T) {
	for _, flag := range []string{"--version", "-v"} {
		t.Run(flag, func(t *testing.T) {
			root := New("test", "abc123", "2026-01-01")
			buf := &bytes.Buffer{}
			root.SetOut(buf)
			root.SetArgs([]string{flag})

			require.NoError(t, root.Execute())
			assert.Equal(t, "treeman test\ncommit  abc123\nbuilt   2026-01-01\n", buf.String())
		})
	}
}

func TestRootCmd_OverviewDiffersFromHelp(t *testing.T) {
	overviewRoot := New("test", "", "")
	overview := &bytes.Buffer{}
	overviewRoot.SetOut(overview)
	require.NoError(t, overviewRoot.Execute())

	helpRoot := New("test", "", "")
	help := &bytes.Buffer{}
	helpRoot.SetOut(help)
	helpRoot.SetArgs([]string{"--help"})
	require.NoError(t, helpRoot.Execute())

	assert.Contains(t, overview.String(), "TreeMan manages isolated Git worktrees.")
	assert.Contains(t, overview.String(), "TREEMAN")
	assert.Contains(t, overview.String(), "COMMANDS")
	assert.Contains(t, overview.String(), "MORE")
	assert.Contains(t, overview.String(), "treeman --help")
	assert.NotContains(t, overview.String(), "\x1b")
	assert.Contains(t, help.String(), "TREEMAN")
	assert.Contains(t, help.String(), "USAGE")
	assert.Contains(t, help.String(), "treeman [flags]")
	assert.Contains(t, help.String(), "treeman [command]")
	assert.Contains(t, help.String(), "COMMANDS")
	assert.Contains(t, help.String(), "FLAGS")
	assert.Contains(t, help.String(), "create")
	assert.Contains(t, help.String(), "help")
	assert.Contains(t, help.String(), "--version")
	assert.NotContains(t, help.String(), "\x1b")
	assert.NotEqual(t, overview.String(), help.String())
}

func TestRootCmd_HelpUsesShortDescriptionWhenLongIsUnset(t *testing.T) {
	root := New("test", "", "")
	help := &bytes.Buffer{}
	root.SetOut(help)
	root.SetArgs([]string{"doctor", "--help"})

	require.NoError(t, root.Execute())

	assert.Contains(t, help.String(), "TREEMAN DOCTOR")
	assert.Contains(t, help.String(), "Check repository readiness and configuration")
	assert.Contains(t, help.String(), "USAGE")
	assert.NotContains(t, help.String(), "\x1b")
}

func TestInitCmd_Zsh(t *testing.T) {
	root := New("test", "", "")
	buf := &bytes.Buffer{}
	root.SetOut(buf)

	root.SetArgs([]string{"init", "zsh"})
	err := root.Execute()
	require.NoError(t, err)

	out := buf.String()
	assert.NotContains(t, out, "\x1b")
	assert.Contains(t, out, "treeman shell init zsh")
	assert.Contains(t, out, "wt() { treeman create \"$@\"; }")
}

func TestInitCmd_Fish(t *testing.T) {
	root := New("test", "", "")
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{"init", "fish"})
	require.NoError(t, root.Execute())

	out := buf.String()
	assert.NotContains(t, out, "\x1b")
	assert.Contains(t, out, "treeman shell init fish")
	assert.Contains(t, out, "function wt")
	assert.Contains(t, out, "function treeman")
	assert.Contains(t, out, "set -lx TREEMAN_CD_FILE \"$_tm_file\"")
	assert.Contains(t, out, "cd (cat \"$_tm_file\")")
	assert.Contains(t, out, "if type -q mktemp",
		"fish reports a missing command itself, so the wrapper asks before calling")
	assert.NotContains(t, out, "(command treeman $argv)")
	assert.NotContains(t, out, "_tm_arg", "the wrapper does not scan TreeMan's arguments")
	assert.Contains(t, out, "function wtc; treeman clean $argv; end")
	assert.NotContains(t, out, "function lg")
}

func TestInitCmd_UnsupportedShell(t *testing.T) {
	root := New("test", "", "")
	root.SetArgs([]string{"init", "powershell"})
	err := root.Execute()
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "unsupported shell"))
}

func TestInitCmd_NoArgs(t *testing.T) {
	root := New("test", "", "")
	root.SetArgs([]string{"init"})
	err := root.Execute()
	assert.Error(t, err)
}
