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
		status setupStatus
		want   ui.Tone
	}{
		{status: setupStatus{description: "completed: copied 1 file"}, want: ui.ToneSuccess},
		{status: setupStatus{description: "skipped"}, want: ui.ToneMuted},
		{status: setupStatus{description: "not bootstrapped: Cargo.toml", warning: true}, want: ui.ToneWarning},
		{status: setupStatus{description: "completed: 1 failed"}, want: ui.ToneFailure},
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

func TestSetupDependencies_ReportsUnsupportedManifestsAfterInstallerFailure(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"example\"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte("{}\n"), 0o644))
	t.Setenv("PATH", "")

	var output bytes.Buffer
	result := setupDependencies(&output, ui.NewRenderer(&output, terminal.Capabilities{}), dir, false)

	assert.True(t, result.incomplete)
	assert.True(t, result.status.warning)
	assert.Contains(t, result.status.description, "failed: package-lock.json found but npm is not installed, skipping")
	assert.Contains(t, result.status.description, "not bootstrapped: Cargo.toml")
	assert.Contains(t, ui.StripANSI(output.String()), "Unsupported dependency manifests were not bootstrapped: Cargo.toml")
}

func TestPrintWorktreeReadiness(t *testing.T) {
	var output bytes.Buffer
	render := ui.NewRenderer(&output, terminal.Capabilities{})

	printWorktreeReadiness(&output, render, true, "Review worktree")
	printWorktreeReadiness(&output, render, false, "Worktree")

	assert.Equal(t, "! Review worktree setup incomplete:\n✓ Worktree ready:\n", ui.StripANSI(output.String()))
}
