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
	assert.Contains(t, out, "wtd()")
	assert.Contains(t, out, "lg()")
	assert.Contains(t, out, "treeman create")
	assert.Contains(t, out, "treeman review")
	assert.Contains(t, out, "treeman switch")
	assert.Contains(t, out, "treeman delete")
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

func TestInitCmd_UnsupportedShell(t *testing.T) {
	root := New("test", "", "")
	root.SetArgs([]string{"init", "fish"})
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
