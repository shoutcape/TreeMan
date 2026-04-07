package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_NoConfigFile(t *testing.T) {
	dir := t.TempDir()
	result := Load(dir)

	assert.Equal(t, Config{}, result.Config)
	assert.Equal(t, "", result.Path)
	assert.Equal(t, "", result.Warning)
}

func TestLoad_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	content := `[database]
env_key = "DATABASE_URI"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ConfigFileName), []byte(content), 0600))

	result := Load(dir)

	assert.Equal(t, "", result.Warning)
	assert.NotEmpty(t, result.Path)
	require.NotNil(t, result.Config.Database)
	assert.Equal(t, "DATABASE_URI", result.Config.Database.EnvKey)
}

func TestLoad_DatabaseURL(t *testing.T) {
	dir := t.TempDir()
	content := `[database]
env_key = "DATABASE_URL"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ConfigFileName), []byte(content), 0600))

	result := Load(dir)

	assert.Equal(t, "", result.Warning)
	require.NotNil(t, result.Config.Database)
	assert.Equal(t, "DATABASE_URL", result.Config.Database.EnvKey)
}

func TestLoad_NoDatabaseSection(t *testing.T) {
	dir := t.TempDir()
	// Config file exists but has no [database] section.
	content := `# Just a comment
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ConfigFileName), []byte(content), 0600))

	result := Load(dir)

	assert.Equal(t, "", result.Warning)
	assert.NotEmpty(t, result.Path)
	assert.Nil(t, result.Config.Database)
	assert.Equal(t, "", result.Config.DatabaseEnvKey())
}

func TestLoad_MissingEnvKey(t *testing.T) {
	dir := t.TempDir()
	// [database] section present but env_key missing -- should produce warning.
	content := `[database]
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ConfigFileName), []byte(content), 0600))

	result := Load(dir)

	assert.Contains(t, result.Warning, "env_key")
	assert.Equal(t, Config{}, result.Config)
}

func TestLoad_InvalidToml(t *testing.T) {
	dir := t.TempDir()
	content := `[database
invalid toml content
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ConfigFileName), []byte(content), 0600))

	result := Load(dir)

	assert.Contains(t, result.Warning, "could not parse")
	assert.Equal(t, Config{}, result.Config)
}

func TestLoad_WalksUpDirectoryTree(t *testing.T) {
	// Create a nested directory structure with .treeman.toml at the root.
	root := t.TempDir()
	nested := filepath.Join(root, "src", "deep", "nested")
	require.NoError(t, os.MkdirAll(nested, 0755))

	content := `[database]
env_key = "DB_URI"
`
	require.NoError(t, os.WriteFile(filepath.Join(root, ConfigFileName), []byte(content), 0600))

	result := Load(nested)

	assert.Equal(t, "", result.Warning)
	require.NotNil(t, result.Config.Database)
	assert.Equal(t, "DB_URI", result.Config.Database.EnvKey)
	assert.Equal(t, filepath.Join(root, ConfigFileName), result.Path)
}

func TestLoad_ClosestConfigWins(t *testing.T) {
	// If there are multiple .treeman.toml files in the tree, the closest one wins.
	root := t.TempDir()
	child := filepath.Join(root, "subdir")
	require.NoError(t, os.MkdirAll(child, 0755))

	rootContent := `[database]
env_key = "ROOT_DB"
`
	childContent := `[database]
env_key = "CHILD_DB"
`
	require.NoError(t, os.WriteFile(filepath.Join(root, ConfigFileName), []byte(rootContent), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(child, ConfigFileName), []byte(childContent), 0600))

	result := Load(child)

	assert.Equal(t, "", result.Warning)
	require.NotNil(t, result.Config.Database)
	assert.Equal(t, "CHILD_DB", result.Config.Database.EnvKey)
}

func TestDatabaseEnvKey_NilDatabase(t *testing.T) {
	cfg := Config{}
	assert.Equal(t, "", cfg.DatabaseEnvKey())
}

func TestDatabaseEnvKey_WithDatabase(t *testing.T) {
	cfg := Config{
		Database: &DatabaseConfig{EnvKey: "DATABASE_URI"},
	}
	assert.Equal(t, "DATABASE_URI", cfg.DatabaseEnvKey())
}

func TestFindConfig_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	result := findConfig(dir)
	assert.Equal(t, "", result)
}

func TestFindConfig_FileInDir(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ConfigFileName)
	require.NoError(t, os.WriteFile(configPath, []byte("[database]\n"), 0600))

	result := findConfig(dir)
	assert.Equal(t, configPath, result)
}

func TestLoad_HooksPostCreate(t *testing.T) {
	dir := t.TempDir()
	content := `[hooks]
post_create = ["pnpm db:migrate", "pnpm codegen"]
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ConfigFileName), []byte(content), 0600))

	result := Load(dir)

	assert.Equal(t, "", result.Warning)
	assert.NotEmpty(t, result.Path)
	require.NotNil(t, result.Config.Hooks)
	assert.Equal(t, []string{"pnpm db:migrate", "pnpm codegen"}, result.Config.Hooks.PostCreate)
	assert.Equal(t, []string{"pnpm db:migrate", "pnpm codegen"}, result.Config.PostCreateHooks())
}

func TestLoad_HooksAndDatabase(t *testing.T) {
	dir := t.TempDir()
	content := `[database]
env_key = "DATABASE_URI"

[hooks]
post_create = ["pnpm db:migrate"]
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ConfigFileName), []byte(content), 0600))

	result := Load(dir)

	assert.Equal(t, "", result.Warning)
	require.NotNil(t, result.Config.Database)
	assert.Equal(t, "DATABASE_URI", result.Config.Database.EnvKey)
	require.NotNil(t, result.Config.Hooks)
	assert.Equal(t, []string{"pnpm db:migrate"}, result.Config.PostCreateHooks())
}

func TestLoad_NoHooksSection(t *testing.T) {
	dir := t.TempDir()
	content := `[database]
env_key = "DATABASE_URI"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ConfigFileName), []byte(content), 0600))

	result := Load(dir)

	assert.Nil(t, result.Config.Hooks)
	assert.Nil(t, result.Config.PostCreateHooks())
}

func TestLoad_EmptyHooksPostCreate(t *testing.T) {
	dir := t.TempDir()
	content := `[hooks]
post_create = []
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ConfigFileName), []byte(content), 0600))

	result := Load(dir)

	assert.Equal(t, "", result.Warning)
	require.NotNil(t, result.Config.Hooks)
	assert.Empty(t, result.Config.PostCreateHooks())
}

func TestPostCreateHooks_NilHooks(t *testing.T) {
	cfg := Config{}
	assert.Nil(t, cfg.PostCreateHooks())
}

func TestLoad_TerminalConfig(t *testing.T) {
	dir := t.TempDir()
	content := `[terminal]
app = "wezterm"

[terminal.layout]
[[terminal.layout.splits]]
direction = "right"
command = "pnpm dev"

[[terminal.layout.splits]]
direction = "bottom"
command = "pnpm test --watch"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ConfigFileName), []byte(content), 0600))

	result := Load(dir)

	assert.Equal(t, "", result.Warning)
	assert.NotEmpty(t, result.Path)
	require.NotNil(t, result.Config.Terminal)
	assert.Equal(t, "wezterm", result.Config.Terminal.App)
	require.NotNil(t, result.Config.Terminal.Layout)
	require.Len(t, result.Config.Terminal.Layout.Splits, 2)
	assert.Equal(t, "right", result.Config.Terminal.Layout.Splits[0].Direction)
	assert.Equal(t, "pnpm dev", result.Config.Terminal.Layout.Splits[0].Command)
	assert.Equal(t, "bottom", result.Config.Terminal.Layout.Splits[1].Direction)
	assert.Equal(t, "pnpm test --watch", result.Config.Terminal.Layout.Splits[1].Command)
}

func TestLoad_TerminalNoLayout(t *testing.T) {
	dir := t.TempDir()
	content := `[terminal]
app = "kitty"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ConfigFileName), []byte(content), 0600))

	result := Load(dir)

	assert.Equal(t, "", result.Warning)
	require.NotNil(t, result.Config.Terminal)
	assert.Equal(t, "kitty", result.Config.Terminal.App)
	assert.Nil(t, result.Config.Terminal.Layout)
}

func TestLoad_TerminalAndDatabase(t *testing.T) {
	dir := t.TempDir()
	content := `[database]
env_key = "DATABASE_URI"

[terminal]
app = "wezterm"

[terminal.layout]
[[terminal.layout.splits]]
direction = "right"
command = "pnpm dev"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ConfigFileName), []byte(content), 0600))

	result := Load(dir)

	assert.Equal(t, "", result.Warning)
	require.NotNil(t, result.Config.Database)
	assert.Equal(t, "DATABASE_URI", result.Config.Database.EnvKey)
	require.NotNil(t, result.Config.Terminal)
	assert.Equal(t, "wezterm", result.Config.Terminal.App)
	require.NotNil(t, result.Config.Terminal.Layout)
	require.Len(t, result.Config.Terminal.Layout.Splits, 1)
}

func TestLoadGlobal_NoFile(t *testing.T) {
	dir := t.TempDir()
	result := LoadGlobal(dir)

	assert.Equal(t, Config{}, result.Config)
	assert.Equal(t, "", result.Path)
	assert.Equal(t, "", result.Warning)
}

func TestLoadGlobal_ValidTerminal(t *testing.T) {
	dir := t.TempDir()
	content := `[terminal]
app = "wezterm"

[terminal.layout]
[[terminal.layout.splits]]
direction = "right"
command = "pnpm dev"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, GlobalConfigFileName), []byte(content), 0600))

	result := LoadGlobal(dir)

	assert.Equal(t, "", result.Warning)
	assert.Equal(t, filepath.Join(dir, GlobalConfigFileName), result.Path)
	require.NotNil(t, result.Config.Terminal)
	assert.Equal(t, "wezterm", result.Config.Terminal.App)
	require.NotNil(t, result.Config.Terminal.Layout)
	require.Len(t, result.Config.Terminal.Layout.Splits, 1)
	assert.Equal(t, "right", result.Config.Terminal.Layout.Splits[0].Direction)
	assert.Equal(t, "pnpm dev", result.Config.Terminal.Layout.Splits[0].Command)
}

func TestLoadGlobal_InvalidToml(t *testing.T) {
	dir := t.TempDir()
	content := `[terminal
broken toml
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, GlobalConfigFileName), []byte(content), 0600))

	result := LoadGlobal(dir)

	assert.Contains(t, result.Warning, "could not parse")
	assert.Equal(t, filepath.Join(dir, GlobalConfigFileName), result.Path)
	assert.Equal(t, Config{}, result.Config)
}

func TestMergeTerminalConfig_BothNil(t *testing.T) {
	result := MergeTerminalConfig(nil, nil)
	assert.Nil(t, result)
}

func TestMergeTerminalConfig_GlobalOnly(t *testing.T) {
	global := &TerminalConfig{App: "wezterm"}
	result := MergeTerminalConfig(global, nil)
	assert.Equal(t, global, result)
}

func TestMergeTerminalConfig_ProjectOnly(t *testing.T) {
	project := &TerminalConfig{App: "kitty"}
	result := MergeTerminalConfig(nil, project)
	assert.Equal(t, project, result)
}

func TestMergeTerminalConfig_ProjectOverridesGlobal(t *testing.T) {
	global := &TerminalConfig{App: "wezterm"}
	project := &TerminalConfig{App: "kitty"}
	result := MergeTerminalConfig(global, project)
	assert.Equal(t, "kitty", result.App)
}

func TestMergeTerminalConfig_ProjectLayoutWithGlobalApp(t *testing.T) {
	global := &TerminalConfig{App: "ghostty"}
	project := &TerminalConfig{Layout: &LayoutConfig{
		Splits: []SplitConfig{{Direction: "right", Command: "pdev"}},
	}}
	result := MergeTerminalConfig(global, project)
	assert.Equal(t, "ghostty", result.App)
	require.NotNil(t, result.Layout)
	assert.Equal(t, "right", result.Layout.Splits[0].Direction)
}

func TestMergeTerminalConfig_GlobalLayoutOverriddenByProject(t *testing.T) {
	global := &TerminalConfig{App: "ghostty", Layout: &LayoutConfig{
		Splits: []SplitConfig{{Direction: "right", Command: ""}},
	}}
	project := &TerminalConfig{Layout: &LayoutConfig{
		Splits: []SplitConfig{{Direction: "down", Command: "pdev"}},
	}}
	result := MergeTerminalConfig(global, project)
	assert.Equal(t, "ghostty", result.App)
	assert.Equal(t, "down", result.Layout.Splits[0].Direction)
	assert.Equal(t, "pdev", result.Layout.Splits[0].Command)
}
