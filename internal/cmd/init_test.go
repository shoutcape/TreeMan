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
	assert.Contains(t, out, "treeman init bash")
	assert.Contains(t, out, "~/.bashrc")
	assert.Contains(t, out, "wt()")
	assert.Contains(t, out, "wtpr()")
	assert.Contains(t, out, "wtmr()")
	assert.Contains(t, out, "wts()")
	assert.Contains(t, out, "wtl()")
	assert.Contains(t, out, "wtc()")
	assert.Contains(t, out, "wtd()")
	assert.Contains(t, out, "lg()")
	assert.NotContains(t, out, "wto()")
	assert.Contains(t, out, "treeman create")
	assert.Contains(t, out, "treeman review")
	assert.Contains(t, out, "treeman switch")
	assert.Contains(t, out, "treeman list")
	assert.Contains(t, out, "treeman clean")
	assert.Contains(t, out, "treeman delete")
	assert.Contains(t, out, "if [ \"${1:-}\" = \"--dry-run\" ]; then")
	assert.Contains(t, out, "_tm_dir=$(treeman clean \"$@\") || return $?")
	assert.Contains(t, out, "_tm_dir=$(treeman delete \"$@\") || return $?")
	assert.Contains(t, out, "[ -n \"$_tm_dir\" ] && cd \"$_tm_dir\"")
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
	assert.Contains(t, overview.String(), "treeman --help")
	assert.Contains(t, help.String(), "Usage:")
	assert.NotEqual(t, overview.String(), help.String())
}

func TestInitCmd_Zsh(t *testing.T) {
	root := New("test", "", "")
	buf := &bytes.Buffer{}
	root.SetOut(buf)

	root.SetArgs([]string{"init", "zsh"})
	err := root.Execute()
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "treeman init zsh")
	assert.Contains(t, out, "~/.zshrc")
}

func TestInitCmd_Fish(t *testing.T) {
	root := New("test", "", "")
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{"init", "fish"})
	require.NoError(t, root.Execute())

	out := buf.String()
	assert.Contains(t, out, "treeman init fish | source")
	assert.Contains(t, out, "function wt")
	assert.Contains(t, out, "set -l _tm_dir (treeman create $argv); or return $status")
	assert.Contains(t, out, "test -n \"$_tm_dir\"; and cd \"$_tm_dir\"")
	assert.Contains(t, out, "if contains -- --dry-run $argv")
	assert.Contains(t, out, "set -l _tm_dir (treeman clean $argv); or return $status")
	assert.Contains(t, out, "function lg")
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
