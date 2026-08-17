package cmd

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreationCommands_HaveOptionalSetupFlagsDisabledByDefault(t *testing.T) {
	for _, newCommand := range []struct {
		name string
		new  func() *cobra.Command
	}{
		{"create", newCreateCmd},
		{"branch", newBranchCmd},
		{"review", newReviewCmd},
	} {
		t.Run(newCommand.name, func(t *testing.T) {
			cmd := newCommand.new()
			for _, name := range []string{"skip-env", "skip-database", "skip-deps", "skip-hooks"} {
				flag := cmd.Flags().Lookup(name)
				require.NotNilf(t, flag, "missing --%s", name)
				assert.Equal(t, "false", flag.DefValue)
			}

			require.NoError(t, cmd.ParseFlags([]string{"--skip-env", "--skip-database", "--skip-deps", "--skip-hooks"}))
			for _, name := range []string{"skip-env", "skip-database", "skip-deps", "skip-hooks"} {
				value, err := cmd.Flags().GetBool(name)
				require.NoError(t, err)
				assert.Truef(t, value, "--%s was not parsed", name)
			}
		})
	}
}

func TestCreationSetupOptions_PrintSkipped(t *testing.T) {
	var output bytes.Buffer
	creationSetupOptions{skipEnv: true, skipDeps: true}.printSkipped(&output)

	assert.Equal(t, "  Skipped: environment file copy (requested)\n  Skipped: dependency installation (requested)\n", output.String())
}

func TestCreationSetupOptions_DoesNotPrintUnrequestedSkips(t *testing.T) {
	var output bytes.Buffer
	creationSetupOptions{}.printSkipped(&output)

	assert.Empty(t, output.String())
}
