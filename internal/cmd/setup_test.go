package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/envrc"
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

	assert.Equal(t, completedStatus("completed: installed with cargo"), status)
	assert.Contains(t, plainOutput, "Detected Cargo.toml, running cargo fetch...")
	assert.Contains(t, plainOutput, "Completed cargo fetch.")
}

func TestSetupDependenciesUsesCorepackForModernYarn(t *testing.T) {
	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, "package.json"), []byte(`{"packageManager":"yarn@4.9.2"}`), 0o644))

	binDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "corepack"), []byte("#!/bin/sh\n"), 0o755))
	t.Setenv("PATH", binDir)

	var output bytes.Buffer
	status := setupDependencies(&output, ui.NewRenderer(&output, terminal.Capabilities{}), project)
	plainOutput := ui.StripANSI(output.String())

	assert.Equal(t, completedStatus("completed: installed with corepack"), status)
	assert.Contains(t, plainOutput, "Detected package.json, running corepack yarn install...")
	assert.Contains(t, plainOutput, "Completed corepack yarn install.")
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
			for _, name := range []string{"skip-env", "skip-database", "skip-deps", "skip-hooks", "trust-hooks"} {
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

// Granting consent for hooks and disabling them state opposite intents, so the
// registry declares them mutually exclusive and every command that registers
// the setup flags rejects the pair before it runs anything.
func TestCreationCommands_RejectTrustingAndSkippingHooksTogether(t *testing.T) {
	for _, newCommand := range []struct {
		name string
		new  func() *cobra.Command
		args []string
	}{
		{"create", newCreateCmd, []string{"feature/test"}},
		{"branch", newBranchCmd, nil},
		{"review", newReviewCmd, nil},
		{"benchmark", newBenchmarkCmd, []string{"delete"}},
	} {
		t.Run(newCommand.name, func(t *testing.T) {
			cmd := newCommand.new()
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(append(append([]string(nil), newCommand.args...), "--trust-hooks", "--skip-hooks"))
			require.EqualError(t, cmd.Execute(),
				"if any flags in the group [skip-hooks trust-hooks] are set none of the others can be; [skip-hooks trust-hooks] were all set")
		})
	}
}

func TestSetupStatusAppearance(t *testing.T) {
	for _, test := range []struct {
		kind setupStatusKind
		want ui.Tone
	}{
		{kind: setupStatusCompleted, want: ui.ToneSuccess},
		{kind: setupStatusSkipped, want: ui.ToneMuted},
		{kind: setupStatusFailed, want: ui.ToneFailure},
	} {
		tone, _ := setupStatusAppearance(test.kind)
		assert.Equal(t, test.want, tone)
	}
}

func TestPrintSetupSummary_ReportsEnvrcToolStatus(t *testing.T) {
	var output bytes.Buffer
	printSetupSummary(&output, ui.NewRenderer(&output, terminal.Capabilities{}), setupSummary{
		environment: completedStatus("completed: copied 1 file(s)"),
		environmentTools: []envrc.ToolStatus{
			{Name: "direnv", Status: envrc.Available},
			{Name: "Nix", Status: envrc.ActiveInCurrentShell},
		},
		dependencies: skippedStatus("skipped"),
		database:     completedStatus("completed"),
		hooks:        skippedStatus("skipped"),
	})

	text := ui.StripANSI(output.String())
	assert.Contains(t, text, "direnv")
	assert.Contains(t, text, "available")
	assert.Contains(t, text, "Nix")
	assert.Contains(t, text, envrc.ActiveInCurrentShell)
}

func TestRunWorktreeSetup_ReportsSourceEnvrcWhenCopySkipped(t *testing.T) {
	mainRoot := t.TempDir()
	worktreePath := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(mainRoot, ".envrc"), []byte("use nix\n"), 0o644))

	var output bytes.Buffer
	summary := runWorktreeSetup(&output, ui.NewRenderer(&output, terminal.Capabilities{}), worktreeSetup{
		mainRoot:      mainRoot,
		worktreePath:  worktreePath,
		projectConfig: config.Config{},
		options: creationSetupOptions{
			skipEnv: true, skipDatabase: true, skipDeps: true, skipHooks: true,
		},
	})

	assert.Equal(t, skippedStatus("skipped (requested)"), summary.environment)
	assert.Equal(t, []string{"direnv", "Nix"}, []string{
		summary.environmentTools[0].Name,
		summary.environmentTools[1].Name,
	})
}

func TestReportNestedModules(t *testing.T) {
	repo, _ := createTestWorktree(t, "feature/nested-modules")
	module := filepath.Join(repo, "apps", "web", "package-lock.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(module), 0755))
	require.NoError(t, os.WriteFile(module, []byte(""), 0644))

	var output bytes.Buffer
	reportNestedModules(&output, ui.NewRenderer(&output, terminal.Capabilities{}), repo)

	assert.Equal(t, "○ Nested module apps/web (package-lock.json): skipped; not installed automatically.\n", ui.StripANSI(output.String()))
}

// Every setup step names the flag that turns it off, and a caller that reports
// a step tells the user to pass that exact flag, so the two lists must agree.
func TestSetupStepsNameARegisteredSkipFlag(t *testing.T) {
	cmd := &cobra.Command{}
	addCreationSetupFlags(cmd, &creationSetupOptions{})

	// A consent flag grants permission for a step rather than owning one, so
	// only the flags that turn a step off have to pair up with the steps.
	skipFlags := map[string]bool{}
	for _, flag := range creationSetupFlags(&creationSetupOptions{}) {
		if flag.conflictsWith == "" {
			skipFlags[flag.name] = true
			continue
		}
		assert.NotNilf(t, cmd.Flags().Lookup(flag.conflictsWith),
			"--%s conflicts with unregistered flag --%s", flag.name, flag.conflictsWith)
	}
	for _, step := range (setupSummary{}).steps() {
		assert.NotNilf(t, cmd.Flags().Lookup(step.skipFlag), "setup step %s names unregistered flag --%s", step.name, step.skipFlag)
		assert.Truef(t, skipFlags[step.skipFlag], "setup step %s names --%s, which turns no step off", step.name, step.skipFlag)
	}
	assert.Len(t, (setupSummary{}).steps(), len(skipFlags))
}
