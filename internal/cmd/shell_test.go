package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShellInstallAddsManagedBlockAndIsIdempotent(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".zshrc")
	cfg := shellConfig{name: "zsh", path: configPath}

	require.NoError(t, installShellIntegration(cfg, "/opt/treeman/bin"))
	first, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(first), shellBlockStart)
	assert.Contains(t, string(first), `export PATH="/opt/treeman/bin:$PATH"`)
	assert.Contains(t, string(first), `eval "$(treeman shell init zsh)"`)
	assert.Contains(t, string(first), shellBlockEnd)

	require.NoError(t, installShellIntegration(cfg, "/opt/treeman/bin"))
	second, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, first, second)
}

func TestShellInstallUpdatesManagedBlock(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".bashrc")
	cfg := shellConfig{name: "bash", path: configPath}
	require.NoError(t, installShellIntegration(cfg, "/old/bin"))
	require.NoError(t, installShellIntegration(cfg, "/new/bin"))

	contents, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.NotContains(t, string(contents), "/old/bin")
	assert.Contains(t, string(contents), "/new/bin")
}

func TestShellInstallPreservesManagedPathWhenPathIsOmitted(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".bashrc")
	cfg := shellConfig{name: "bash", path: configPath}
	require.NoError(t, installShellIntegration(cfg, "/opt/treeman/bin"))
	require.NoError(t, installShellIntegration(cfg, ""))

	contents, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(contents), "/opt/treeman/bin")
}

func TestShellInstallRejectsMalformedBlock(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".zshrc")
	require.NoError(t, os.WriteFile(configPath, []byte(shellBlockStart+"\n"), 0o600))

	err := installShellIntegration(shellConfig{name: "zsh", path: configPath}, "")
	assert.ErrorContains(t, err, "malformed")
}

func TestShellUninstallRemovesOnlyManagedBlock(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".zshrc")
	contents := "export EDITOR=vim\n\n" + managedShellBlock("zsh", "") + "alias ll='ls -l'\n"
	require.NoError(t, os.WriteFile(configPath, []byte(contents), 0o600))

	removed, err := uninstallShellIntegration(configPath)
	require.NoError(t, err)
	assert.True(t, removed)
	updated, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.NotContains(t, string(updated), shellBlockStart)
	assert.Contains(t, string(updated), "export EDITOR=vim")
	assert.Contains(t, string(updated), "alias ll='ls -l'")
}

func TestShellStatusRecognizesManagedAndLegacyIntegration(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".zshrc")
	require.NoError(t, os.WriteFile(configPath, []byte(managedShellBlock("zsh", "")), 0o600))
	state, err := shellIntegrationState(configPath, "zsh")
	require.NoError(t, err)
	assert.Equal(t, "installed", state)

	require.NoError(t, os.WriteFile(configPath, []byte(`eval "$(treeman init zsh)"`), 0o600))
	state, err = shellIntegrationState(configPath, "zsh")
	require.NoError(t, err)
	assert.Equal(t, "legacy", state)
}

func TestResolveShellConfigUsesShellConventions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))

	bash, err := resolveShellConfig("bash", "")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".bashrc"), bash.path)
	zsh, err := resolveShellConfig("zsh", "")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".zshrc"), zsh.path)
	fish, err := resolveShellConfig("fish", "")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "xdg", "fish", "config.fish"), fish.path)
}

func TestShellUninstallAllRemovesManagedBlocks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	paths := []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, "xdg", "fish", "config.fish"),
	}
	for _, path := range paths {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(managedShellBlock("bash", "")), 0o600))
	}

	removed, err := uninstallAllShellIntegrations()
	require.NoError(t, err)
	assert.Equal(t, len(paths), removed)
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.NotContains(t, string(contents), shellBlockStart)
	}
}

func TestShellInstallCommandSupportsExplicitConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.fish")
	root := New("test", "", "")
	output := &bytes.Buffer{}
	root.SetOut(output)
	root.SetArgs([]string{"shell", "install", "--shell", "fish", "--config", configPath})

	require.NoError(t, root.Execute())
	assert.Contains(t, output.String(), "installed")
	contents, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(contents), "treeman shell init fish | source")
}

func TestBashIntegrationChangesDirectoryAndPreservesHelp(t *testing.T) {
	binDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "worktree with spaces")
	require.NoError(t, os.Mkdir(target, 0o755))
	fakeBinary := filepath.Join(binDir, "treeman")
	require.NoError(t, os.WriteFile(fakeBinary, []byte(`#!/usr/bin/env bash
case "$1" in
  create)
    if [[ "${2:-}" == "--help" ]]; then
      printf 'create help\n'
    else
      printf '%s\n' "$TREEMAN_TEST_DEST" > "$TREEMAN_CD_FILE"
    fi
    ;;
  tms) printf '%s\n' "$TREEMAN_TEST_DEST" > "$TREEMAN_CD_FILE" ;;
  version) printf 'test version\n' ;;
esac
`), 0o755))
	integrationPath := filepath.Join(t.TempDir(), "integration.sh")
	integration := renderShellInit("bash")
	require.NoError(t, os.WriteFile(integrationPath, []byte(integration), 0o600))

	command := exec.Command("bash", "-c", `source "$1"; tm feature/test; printf '%s' "$PWD"`, "bash", integrationPath)
	command.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"), "TREEMAN_TEST_DEST="+target)
	output, err := command.Output()
	require.NoError(t, err)
	assert.Equal(t, target, string(output))

	// A later user binding must not change what the compatibility shortcut runs.
	command = exec.Command("bash", "-c", `source "$1"; tm() { printf 'hijacked'; }; wt feature/test; printf '%s' "$PWD"`, "bash", integrationPath)
	command.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"), "TREEMAN_TEST_DEST="+target)
	output, err = command.Output()
	require.NoError(t, err)
	assert.Equal(t, target, string(output))

	// The wt* names predate tm* and reach the same destination through shims.
	command = exec.Command("bash", "-c", `source "$1"; wt feature/test; printf '%s' "$PWD"`, "bash", integrationPath)
	command.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"), "TREEMAN_TEST_DEST="+target)
	output, err = command.Output()
	require.NoError(t, err)
	assert.Equal(t, target, string(output))

	command = exec.Command("bash", "-c", `source "$1"; treeman tms; printf '%s' "$PWD"`, "bash", integrationPath)
	command.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"), "TREEMAN_TEST_DEST="+target)
	output, err = command.Output()
	require.NoError(t, err)
	assert.Equal(t, target, string(output))

	// Output that is not a destination reaches the terminal directly, because
	// the wrapper no longer captures stdout to find the path.
	command = exec.Command("bash", "-c", `source "$1"; treeman create --help; printf '%s' "$PWD"`, "bash", integrationPath)
	command.Dir = binDir
	command.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"))
	output, err = command.Output()
	require.NoError(t, err)
	assert.Equal(t, "create help\n"+binDir, string(output), "no destination means no directory change")
}
