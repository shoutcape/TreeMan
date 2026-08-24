package forge

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunForgeCLI(t *testing.T) {
	t.Setenv("GO_WANT_FORGE_HELPER_PROCESS", "1")
	output, err := runForgeCLI(testHelperCommand(), []string{"-test.run=^TestForgeCLIHelperProcess$", "--", "success"}, []byte("request"), "test request")
	require.NoError(t, err)
	assert.Contains(t, string(output), "stdout:request")
	assert.NotContains(t, string(output), "diagnostic")

	output, err = runForgeCLI(testHelperCommand(), []string{"-test.run=^TestForgeCLIHelperProcess$", "--", "args", "one", "two"}, nil, "test args")
	require.NoError(t, err)
	assert.Contains(t, string(output), "args,one,two")

	_, err = runForgeCLI(testHelperCommand(), []string{"-test.run=^TestForgeCLIHelperProcess$", "--", "failure"}, nil, "test failure")
	require.Error(t, err)
	assert.EqualError(t, err, "test failure: failed request")

	_, err = runForgeCLI(testHelperCommand(), []string{"-test.run=^TestForgeCLIHelperProcess$", "--", "silent-failure"}, nil, "test silent failure")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "test silent failure: exit status 4")
}

func testHelperCommand() string {
	return os.Args[0]
}

func TestForgeCLIHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_FORGE_HELPER_PROCESS") != "1" {
		return
	}
	separator := 0
	for index, arg := range os.Args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator == 0 {
		return
	}
	if len(os.Args) == separator+1 {
		os.Exit(2)
	}
	args := os.Args[separator+1:]
	switch args[0] {
	case "success":
		input, err := io.ReadAll(os.Stdin)
		if err != nil {
			os.Exit(3)
		}
		_, _ = os.Stdout.WriteString("stdout:" + string(input))
		_, _ = os.Stderr.WriteString("diagnostic")
	case "args":
		_, _ = os.Stdout.WriteString(strings.Join(args, ","))
	case "failure":
		_, _ = os.Stderr.WriteString("  failed request\n")
		os.Exit(4)
	case "silent-failure":
		os.Exit(4)
	default:
		os.Exit(5)
	}
}
