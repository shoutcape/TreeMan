package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/shoutcape/treeman/internal/terminal"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupDependenciesReportsSilentCargoSuccess(t *testing.T) {
	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, "Cargo.toml"), []byte("[package]\nname = \"example\""), 0o644))

	binDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "cargo"), []byte("#!/bin/sh\n"), 0o755))
	t.Setenv("PATH", binDir)

	var output bytes.Buffer
	status := setupDependencies(&output, ui.NewRenderer(&output, terminal.Capabilities{}), project)
	plainOutput := ui.StripANSI(output.String())

	assert.Equal(t, "completed: installed with cargo", status)
	assert.Contains(t, plainOutput, "Detected Cargo.toml, running cargo fetch...")
	assert.Contains(t, plainOutput, "Completed cargo fetch.")
}

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
	creationSetupOptions{skipEnv: true, skipDeps: true}.printSkipped(&output, ui.NewRenderer(&output, terminal.Capabilities{}))

	assert.Equal(t, "  ○  Skipped: environment file copy (requested)\n  ○  Skipped: dependency installation (requested)\n", ui.StripANSI(output.String()))
}

func TestSetupStatusAppearance(t *testing.T) {
	for _, test := range []struct {
		status string
		want   ui.Tone
	}{
		{status: "completed: copied 1 file", want: ui.ToneSuccess},
		{status: "skipped", want: ui.ToneMuted},
		{status: "completed: 1 failed", want: ui.ToneFailure},
	} {
		tone, _ := setupStatusAppearance(test.status)
		assert.Equal(t, test.want, tone)
	}
}

func TestCreationSetupOptions_DoesNotPrintUnrequestedSkips(t *testing.T) {
	var output bytes.Buffer
	creationSetupOptions{}.printSkipped(&output, ui.NewRenderer(&output, terminal.Capabilities{}))

	assert.Empty(t, output.String())
}
