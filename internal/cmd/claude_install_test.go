package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type installedHook struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

// treemanHookCommands returns the command hooks registered for event.
func treemanHookCommands(t *testing.T, settings []byte, event string) []installedHook {
	t.Helper()
	var doc struct {
		Hooks map[string][]struct {
			Matcher string          `json:"matcher"`
			Hooks   []installedHook `json:"hooks"`
		} `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal(settings, &doc), "settings must stay valid JSON: %s", settings)
	var out []installedHook
	for _, group := range doc.Hooks[event] {
		out = append(out, group.Hooks...)
	}
	return out
}

func assertTreemanHooksInstalled(t *testing.T, settings []byte) {
	t.Helper()
	create := treemanHookCommands(t, settings, "WorktreeCreate")
	require.Len(t, create, 1)
	assert.Equal(t, installedHook{Type: "command", Command: "treeman claude worktree-create", Timeout: 600}, create[0])
	remove := treemanHookCommands(t, settings, "WorktreeRemove")
	require.Len(t, remove, 1)
	assert.Equal(t, installedHook{Type: "command", Command: "treeman claude worktree-remove", Timeout: 60}, remove[0])
}

// topLevelKeys returns an object's keys in file order.
func topLevelKeys(t *testing.T, data []byte) []string {
	t.Helper()
	object, err := parseOrderedObjectForTest(data)
	require.NoError(t, err)
	return object
}

func parseOrderedObjectForTest(data []byte) ([]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	var keys []string
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		keys = append(keys, token.(string))
		var skip json.RawMessage
		if err := decoder.Decode(&skip); err != nil {
			return nil, err
		}
	}
	return keys, nil
}

func TestInstallClaudeHooksAddsBothEntriesToEmptySettings(t *testing.T) {
	updated, changes, conflicts, err := installClaudeHooks([]byte("{}"), false)
	require.NoError(t, err)
	assert.Empty(t, conflicts)
	assert.Len(t, changes, 2)
	assertTreemanHooksInstalled(t, updated)
}

func TestInstallClaudeHooksPreservesOtherSettingsAndOrder(t *testing.T) {
	original := []byte(`{
  "zeta": 1,
  "permissions": {"allow": ["Bash(ls:*)"], "deny": []},
  "hooks": {
    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo <pre> & done"}]}],
    "Stop": [{"hooks": [{"type": "command", "command": "notify"}]}]
  },
  "alpha": "a&b<c>",
  "model": "opus"
}`)
	updated, changes, conflicts, err := installClaudeHooks(original, false)
	require.NoError(t, err)
	assert.Empty(t, conflicts)
	assert.NotEmpty(t, changes)
	assertTreemanHooksInstalled(t, updated)

	assert.Equal(t, []string{"zeta", "permissions", "hooks", "alpha", "model"}, topLevelKeys(t, updated))

	var before, after map[string]any
	require.NoError(t, json.Unmarshal(original, &before))
	require.NoError(t, json.Unmarshal(updated, &after))
	for _, key := range []string{"zeta", "permissions", "alpha", "model"} {
		assert.Equal(t, before[key], after[key], key)
	}
	beforeHooks := before["hooks"].(map[string]any)
	afterHooks := after["hooks"].(map[string]any)
	assert.Equal(t, beforeHooks["PreToolUse"], afterHooks["PreToolUse"])
	assert.Equal(t, beforeHooks["Stop"], afterHooks["Stop"])

	var rawHooks struct {
		Hooks json.RawMessage `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal(updated, &rawHooks))
	hookKeys, err := parseOrderedObjectForTest(rawHooks.Hooks)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(hookKeys), 4)
	assert.Equal(t, []string{"PreToolUse", "Stop"}, hookKeys[:2], "existing hook events keep their order")

	// Characters are not HTML-escaped into & style escapes.
	assert.Contains(t, string(updated), "a&b<c>")
}

func TestInstallClaudeHooksIsIdempotent(t *testing.T) {
	first, _, _, err := installClaudeHooks([]byte(`{"model":"opus"}`), false)
	require.NoError(t, err)
	second, changes, conflicts, err := installClaudeHooks(first, false)
	require.NoError(t, err)
	assert.Empty(t, changes)
	assert.Empty(t, conflicts)
	assert.Equal(t, string(first), string(second))
}

func TestInstallClaudeHooksRefusesForeignWorktreeHooksWithoutReplace(t *testing.T) {
	for _, event := range []string{"WorktreeCreate", "WorktreeRemove"} {
		t.Run(event, func(t *testing.T) {
			original := []byte(`{"hooks":{"` + event + `":[{"hooks":[{"type":"command","command":"bash ~/.claude/hooks/x.sh"}]}]}}`)
			_, changes, conflicts, err := installClaudeHooks(original, false)
			require.NoError(t, err)
			assert.NotEmpty(t, conflicts)
			assert.Empty(t, changes)
		})
	}
}

func TestInstallClaudeHooksReplacesForeignWorktreeHooksWithReplace(t *testing.T) {
	original := []byte(`{"hooks":{"WorktreeCreate":[{"hooks":[{"type":"command","command":"old-create"}]}],"WorktreeRemove":[{"hooks":[{"type":"command","command":"old-remove"}]}],"Stop":[]}}`)
	updated, changes, conflicts, err := installClaudeHooks(original, true)
	require.NoError(t, err)
	assert.Empty(t, conflicts)
	assert.Len(t, changes, 2)
	assertTreemanHooksInstalled(t, updated)
	assert.NotContains(t, string(updated), "old-create")
	assert.NotContains(t, string(updated), "old-remove")
	assert.Contains(t, string(updated), `"Stop"`)
}

func TestInstallClaudeHooksRejectsMalformedSettings(t *testing.T) {
	for name, data := range map[string]string{
		"invalid JSON":     `{"hooks":`,
		"top-level array":  `[]`,
		"non-object hooks": `{"hooks":[]}`,
		"string hooks":     `{"hooks":"x"}`,
		"trailing garbage": `{} {}`,
		"top-level string": `"x"`,
	} {
		t.Run(name, func(t *testing.T) {
			_, _, _, err := installClaudeHooks([]byte(data), false)
			assert.Error(t, err)
		})
	}
}

// --- command level -----------------------------------------------------------

func runClaudeInstallCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	root := New("test", "test", "test")
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(append([]string{"claude", "install"}, args...))
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func isolateClaudeConfig(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	os.Unsetenv("CLAUDE_CONFIG_DIR")
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	return home
}

func backupsOf(t *testing.T, path string) []string {
	t.Helper()
	matches, err := filepath.Glob(path + ".treeman-backup-*")
	require.NoError(t, err)
	return matches
}

func TestClaudeInstallWritesSettingsWithABackupAndIsIdempotent(t *testing.T) {
	isolateClaudeConfig(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	original := "{\n  \"model\": \"opus\",\n  \"env\": {\"A\": \"1\"}\n}\n"
	require.NoError(t, os.WriteFile(path, []byte(original), 0o640))

	stdout, _, err := runClaudeInstallCommand(t, "--settings", path)
	require.NoError(t, err)
	assert.Empty(t, stdout)
	updated, err := os.ReadFile(path)
	require.NoError(t, err)
	assertTreemanHooksInstalled(t, updated)
	assert.Equal(t, []string{"model", "env", "hooks"}, topLevelKeys(t, updated))

	backups := backupsOf(t, path)
	require.Len(t, backups, 1)
	backup, err := os.ReadFile(backups[0])
	require.NoError(t, err)
	assert.Equal(t, original, string(backup))

	_, _, err = runClaudeInstallCommand(t, "--settings", path)
	require.NoError(t, err)
	again, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(updated), string(again), "a second install changes nothing")
	assert.Len(t, backupsOf(t, path), 1, "a second install has nothing to back up")
}

func TestClaudeInstallCreatesAMissingSettingsFile(t *testing.T) {
	isolateClaudeConfig(t)
	path := filepath.Join(t.TempDir(), "nested", "settings.json")

	_, _, err := runClaudeInstallCommand(t, "--settings", path)

	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assertTreemanHooksInstalled(t, data)
}

func TestClaudeInstallTreatsAnEmptyFileAsEmptySettings(t *testing.T) {
	isolateClaudeConfig(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, os.WriteFile(path, []byte("  \n"), 0o600))

	_, _, err := runClaudeInstallCommand(t, "--settings", path)

	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assertTreemanHooksInstalled(t, data)
}

func TestClaudeInstallRefusesForeignHooksAndLeavesTheFileUnchanged(t *testing.T) {
	isolateClaudeConfig(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	original := `{"hooks":{"WorktreeCreate":[{"hooks":[{"type":"command","command":"bash ~/.claude/hooks/worktree-create.sh"}]}]}}`
	require.NoError(t, os.WriteFile(path, []byte(original), 0o600))

	_, stderr, err := runClaudeInstallCommand(t, "--settings", path)

	require.Error(t, err)
	assert.Contains(t, stderr, "worktree-create.sh", "the conflicting hook is reported")
	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, original, string(data))
	assert.Empty(t, backupsOf(t, path))
}

func TestClaudeInstallReplaceOverwritesForeignHooks(t *testing.T) {
	isolateClaudeConfig(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	original := `{"hooks":{"WorktreeCreate":[{"hooks":[{"type":"command","command":"old"}]}]},"model":"opus"}`
	require.NoError(t, os.WriteFile(path, []byte(original), 0o600))

	_, _, err := runClaudeInstallCommand(t, "--settings", path, "--replace")

	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assertTreemanHooksInstalled(t, data)
	assert.Equal(t, []string{"hooks", "model"}, topLevelKeys(t, data))
	require.Len(t, backupsOf(t, path), 1)
}

func TestClaudeInstallRejectsInvalidJSONWithoutWriting(t *testing.T) {
	isolateClaudeConfig(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	original := `{"model": "opus",`
	require.NoError(t, os.WriteFile(path, []byte(original), 0o600))

	_, _, err := runClaudeInstallCommand(t, "--settings", path)

	require.Error(t, err)
	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, original, string(data))
}

func TestClaudeInstallDryRunWritesNothing(t *testing.T) {
	isolateClaudeConfig(t)
	dir := t.TempDir()
	existing := filepath.Join(dir, "settings.json")
	original := `{"model":"opus"}`
	require.NoError(t, os.WriteFile(existing, []byte(original), 0o600))
	missing := filepath.Join(dir, "absent", "settings.json")

	_, stderr, err := runClaudeInstallCommand(t, "--settings", existing, "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, stderr, "WorktreeCreate", "a dry run says what it would change")
	data, err := os.ReadFile(existing)
	require.NoError(t, err)
	assert.Equal(t, original, string(data))
	assert.Empty(t, backupsOf(t, existing))

	_, _, err = runClaudeInstallCommand(t, "--settings", missing, "--dry-run")
	require.NoError(t, err)
	assert.NoFileExists(t, missing)
	assert.NoDirExists(t, filepath.Dir(missing))
}

func TestClaudeInstallDefaultsToClaudeConfigDir(t *testing.T) {
	home := isolateClaudeConfig(t)
	configDir := filepath.Join(t.TempDir(), "claude-config")
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	_, _, err := runClaudeInstallCommand(t)

	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(configDir, "settings.json"))
	require.NoError(t, err)
	assertTreemanHooksInstalled(t, data)
	assert.NoFileExists(t, filepath.Join(home, ".claude", "settings.json"))
}

func TestClaudeInstallDefaultsToHomeClaudeSettings(t *testing.T) {
	home := isolateClaudeConfig(t)

	_, _, err := runClaudeInstallCommand(t)

	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	require.NoError(t, err)
	assertTreemanHooksInstalled(t, data)
}
