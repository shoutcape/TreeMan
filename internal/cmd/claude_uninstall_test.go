package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const foreignPermissionGroup = `{"matcher":"Bash","hooks":[{"type":"command","command":"guard-bash"}]}`

// --- install: PermissionRequest ---------------------------------------------

func TestInstallClaudeHooksAddsPermissionRequestBesideAForeignGroup(t *testing.T) {
	original := []byte(`{"hooks":{"PermissionRequest":[` + foreignPermissionGroup + `]}}`)

	updated, changes, conflicts, err := installClaudeHooks(original, false)

	require.NoError(t, err)
	assert.Empty(t, conflicts, "a foreign PermissionRequest hook is not a conflict")
	assert.NotEmpty(t, changes)
	groups := hookGroups(t, updated, "PermissionRequest")
	require.Len(t, groups, 2, "the foreign group is kept and TreeMan's is added")
	assert.Equal(t, "Bash", groups[0].Matcher, "the foreign group stays first")
	assert.Equal(t, "guard-bash", groups[0].Hooks[0].Command)
	assert.True(t, isTreemanPermissionGroup(groups[1]))
	assertTreemanHooksInstalled(t, updated)

	again, changes, conflicts, err := installClaudeHooks(updated, false)
	require.NoError(t, err)
	assert.Empty(t, changes, "a second install changes nothing")
	assert.Empty(t, conflicts)
	assert.Equal(t, string(updated), string(again))
}

func TestInstallClaudeHooksAddsPermissionRequestBesideAForeignEnterWorktreeGroup(t *testing.T) {
	foreign := `{"matcher":"EnterWorktree","hooks":[{"type":"command","command":"other-approver"}]}`
	original := []byte(`{"hooks":{"PermissionRequest":[` + foreign + `]}}`)

	updated, _, conflicts, err := installClaudeHooks(original, false)

	require.NoError(t, err)
	assert.Empty(t, conflicts)
	groups := hookGroups(t, updated, "PermissionRequest")
	require.Len(t, groups, 2)
	assert.Equal(t, "other-approver", groups[0].Hooks[0].Command)
	assertTreemanPermissionGroup(t, updated)
}

func TestInstallClaudeHooksAddsOnlyPermissionRequestWhenWorktreeHooksAreInstalled(t *testing.T) {
	// Settings from an install that predates the PermissionRequest hook.
	original := []byte(`{"hooks":{` +
		`"WorktreeCreate":[{"hooks":[{"type":"command","command":"treeman claude worktree-create","timeout":600}]}],` +
		`"WorktreeRemove":[{"hooks":[{"type":"command","command":"treeman claude worktree-remove","timeout":60}]}]}}`)

	updated, changes, conflicts, err := installClaudeHooks(original, false)

	require.NoError(t, err)
	assert.Empty(t, conflicts)
	assert.Len(t, changes, 1)
	assertTreemanHooksInstalled(t, updated)
	assertTreemanPermissionGroup(t, updated)
}

func TestClaudeInstallCommandAddsPermissionRequestAndIsIdempotent(t *testing.T) {
	isolateClaudeConfig(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"hooks":{"PermissionRequest":[`+foreignPermissionGroup+`]}}`), 0o600))

	_, _, err := runClaudeInstallCommand(t, "--settings", path)
	require.NoError(t, err)
	first, err := os.ReadFile(path)
	require.NoError(t, err)
	assertTreemanPermissionGroup(t, first)
	require.Len(t, hookGroups(t, first, "PermissionRequest"), 2)

	_, _, err = runClaudeInstallCommand(t, "--settings", path)
	require.NoError(t, err)
	second, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(first), string(second))
	assert.Len(t, backupsOf(t, path), 1)
}

// --- uninstall: settings transformation -------------------------------------

func installedSettings(t *testing.T, original string) []byte {
	t.Helper()
	updated, _, conflicts, err := installClaudeHooks([]byte(original), false)
	require.NoError(t, err)
	require.Empty(t, conflicts)
	return updated
}

func hookEventKeys(t *testing.T, settings []byte) []string {
	t.Helper()
	var doc struct {
		Hooks json.RawMessage `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal(settings, &doc))
	if doc.Hooks == nil {
		return nil
	}
	keys, err := parseOrderedObjectForTest(doc.Hooks)
	require.NoError(t, err)
	return keys
}

func TestUninstallClaudeHooksRestoresSettingsWithoutHooks(t *testing.T) {
	installed := installedSettings(t, `{"model":"opus","env":{"A":"1"}}`)

	updated, changes, kept, err := uninstallClaudeHooks(installed)

	require.NoError(t, err)
	assert.Len(t, changes, 3, "all three TreeMan entries are removed")
	assert.Empty(t, kept)
	assert.Equal(t, []string{"model", "env"}, topLevelKeys(t, updated), "an empty hooks object is removed")
	var got, want any
	require.NoError(t, json.Unmarshal(updated, &got))
	require.NoError(t, json.Unmarshal([]byte(`{"model":"opus","env":{"A":"1"}}`), &want))
	assert.Equal(t, want, got)
}

func TestUninstallClaudeHooksKeepsForeignHooksAndOrder(t *testing.T) {
	treemanPermission := `{"matcher":"EnterWorktree","hooks":[{"type":"command","command":"treeman claude permission-request","timeout":30}]}`
	laterPermission := `{"matcher":"Write","hooks":[{"type":"command","command":"guard-write"}]}`
	original := []byte(`{
  "zeta": 1,
  "hooks": {
    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo <pre> & done"}]}],
    "WorktreeCreate": [{"hooks": [{"type": "command", "command": "treeman claude worktree-create", "timeout": 600}]}],
    "PermissionRequest": [` + foreignPermissionGroup + `, ` + treemanPermission + `, ` + laterPermission + `],
    "WorktreeRemove": [{"hooks": [{"type": "command", "command": "treeman claude worktree-remove", "timeout": 60}]}],
    "Stop": [{"hooks": [{"type": "command", "command": "notify"}]}]
  },
  "alpha": "a&b<c>"
}`)

	updated, changes, kept, err := uninstallClaudeHooks(original)

	require.NoError(t, err)
	assert.Len(t, changes, 3)
	assert.Empty(t, kept)
	assert.Equal(t, []string{"zeta", "hooks", "alpha"}, topLevelKeys(t, updated))
	assert.Equal(t, []string{"PreToolUse", "PermissionRequest", "Stop"}, hookEventKeys(t, updated),
		"emptied events are removed and the others keep their order")

	var before, after struct {
		Hooks map[string]any `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal(original, &before))
	require.NoError(t, json.Unmarshal(updated, &after))
	assert.Equal(t, before.Hooks["PreToolUse"], after.Hooks["PreToolUse"])
	assert.Equal(t, before.Hooks["Stop"], after.Hooks["Stop"])

	groups := hookGroups(t, updated, "PermissionRequest")
	require.Len(t, groups, 2)
	assert.Equal(t, "guard-bash", groups[0].Hooks[0].Command)
	assert.Equal(t, "guard-write", groups[1].Hooks[0].Command)
	assert.NotContains(t, string(updated), "treeman claude")
	assert.Contains(t, string(updated), "a&b<c>", "characters are not HTML-escaped")
}

func TestUninstallClaudeHooksRemovesAnEmptiedPermissionRequestButKeepsOtherHooks(t *testing.T) {
	installed := installedSettings(t, `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"notify"}]}]}}`)

	updated, _, _, err := uninstallClaudeHooks(installed)

	require.NoError(t, err)
	assert.Equal(t, []string{"Stop"}, hookEventKeys(t, updated))
}

func TestUninstallClaudeHooksLeavesForeignWorktreeHooksAndReportsThem(t *testing.T) {
	for _, event := range []string{"WorktreeCreate", "WorktreeRemove"} {
		t.Run(event, func(t *testing.T) {
			// TreeMan installed everything, then the user replaced one
			// worktree hook with their own.
			installed := installedSettings(t, `{}`)
			var doc map[string]map[string]any
			require.NoError(t, json.Unmarshal(installed, &doc))
			doc["hooks"][event] = []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "my-own-hook"}}}}
			original, err := json.Marshal(doc)
			require.NoError(t, err)

			updated, changes, kept, err := uninstallClaudeHooks(original)

			require.NoError(t, err)
			assert.Len(t, changes, 2, "the other two TreeMan entries are removed")
			require.NotEmpty(t, kept, "the foreign hook is reported")
			assert.Contains(t, strings.Join(kept, "\n"), event)
			commands := treemanHookCommands(t, updated, event)
			require.Len(t, commands, 1)
			assert.Equal(t, "my-own-hook", commands[0].Command)
			assert.NotContains(t, string(updated), "treeman claude")
		})
	}
}

func TestUninstallClaudeHooksWithNothingInstalledChangesNothing(t *testing.T) {
	for name, settings := range map[string]string{
		"no hooks":      `{"model":"opus"}`,
		"null hooks":    `{"hooks":null}`,
		"foreign hooks": `{"hooks":{"PermissionRequest":[` + foreignPermissionGroup + `],"Stop":[]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			updated, changes, _, err := uninstallClaudeHooks([]byte(settings))
			require.NoError(t, err)
			assert.Empty(t, changes)
			assert.Equal(t, settings, string(updated))
		})
	}
}

func TestUninstallClaudeHooksIsIdempotent(t *testing.T) {
	installed := installedSettings(t, `{"model":"opus","hooks":{"PermissionRequest":[`+foreignPermissionGroup+`]}}`)
	first, _, _, err := uninstallClaudeHooks(installed)
	require.NoError(t, err)

	second, changes, _, err := uninstallClaudeHooks(first)

	require.NoError(t, err)
	assert.Empty(t, changes)
	assert.Equal(t, string(first), string(second))
}

func TestUninstallClaudeHooksRejectsMalformedSettings(t *testing.T) {
	for name, data := range map[string]string{
		"invalid JSON":    `{"hooks":`,
		"top-level array": `[]`,
		"array hooks":     `{"hooks":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, _, _, err := uninstallClaudeHooks([]byte(data))
			assert.Error(t, err)
		})
	}
}

// --- uninstall: command level -----------------------------------------------

func runClaudeUninstallCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	root := New("test", "test", "test")
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(append([]string{"claude", "uninstall"}, args...))
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func writeInstalledSettings(t *testing.T, original string) (path string, installed []byte) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "settings.json")
	installed = installedSettings(t, original)
	require.NoError(t, os.WriteFile(path, installed, 0o640))
	return path, installed
}

func TestClaudeUninstallRemovesTreemanHooksWithABackupAndIsIdempotent(t *testing.T) {
	isolateClaudeConfig(t)
	path, installed := writeInstalledSettings(t, `{"model":"opus","hooks":{"Stop":[{"hooks":[{"type":"command","command":"notify"}]}]}}`)

	stdout, _, err := runClaudeUninstallCommand(t, "--settings", path)

	require.NoError(t, err)
	assert.Empty(t, stdout)
	updated, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(updated), "treeman claude")
	assert.Equal(t, []string{"Stop"}, hookEventKeys(t, updated))
	assert.Equal(t, []string{"model", "hooks"}, topLevelKeys(t, updated))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm(), "the file keeps its permissions")

	backups := backupsOf(t, path)
	require.Len(t, backups, 1)
	backup, err := os.ReadFile(backups[0])
	require.NoError(t, err)
	assert.Equal(t, string(installed), string(backup))

	_, _, err = runClaudeUninstallCommand(t, "--settings", path)
	require.NoError(t, err)
	again, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(updated), string(again), "a second uninstall changes nothing")
	assert.Len(t, backupsOf(t, path), 1, "a second uninstall has nothing to back up")
}

func TestClaudeUninstallLeavesAForeignWorktreeCreateAndReportsIt(t *testing.T) {
	isolateClaudeConfig(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	original := `{"hooks":{` +
		`"WorktreeCreate":[{"hooks":[{"type":"command","command":"bash ~/.claude/hooks/worktree-create.sh"}]}],` +
		`"WorktreeRemove":[{"hooks":[{"type":"command","command":"treeman claude worktree-remove","timeout":60}]}]}}`
	require.NoError(t, os.WriteFile(path, []byte(original), 0o600))

	_, stderr, err := runClaudeUninstallCommand(t, "--settings", path)

	require.NoError(t, err)
	assert.Contains(t, stderr, "WorktreeCreate", "the foreign hook is reported")
	updated, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"WorktreeCreate"}, hookEventKeys(t, updated))
	create := treemanHookCommands(t, updated, "WorktreeCreate")
	require.Len(t, create, 1)
	assert.Equal(t, "bash ~/.claude/hooks/worktree-create.sh", create[0].Command)
}

func TestClaudeUninstallReportsAForeignWorktreeCreateWhenNothingElseIsInstalled(t *testing.T) {
	isolateClaudeConfig(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	original := `{"hooks":{"WorktreeCreate":[{"hooks":[{"type":"command","command":"my-create"}]}]}}`
	require.NoError(t, os.WriteFile(path, []byte(original), 0o600))

	_, stderr, err := runClaudeUninstallCommand(t, "--settings", path)

	require.NoError(t, err)
	assert.Contains(t, stderr, "WorktreeCreate")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, original, string(data))
	assert.Empty(t, backupsOf(t, path))
}

func TestClaudeUninstallDryRunWritesNothing(t *testing.T) {
	isolateClaudeConfig(t)
	path, installed := writeInstalledSettings(t, `{"model":"opus"}`)

	_, stderr, err := runClaudeUninstallCommand(t, "--settings", path, "--dry-run")

	require.NoError(t, err)
	assert.Contains(t, stderr, "PermissionRequest", "a dry run says what it would change")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(installed), string(data))
	assert.Empty(t, backupsOf(t, path))
}

func TestClaudeUninstallOfAMissingFileIsNotAnError(t *testing.T) {
	isolateClaudeConfig(t)
	path := filepath.Join(t.TempDir(), "absent", "settings.json")

	_, _, err := runClaudeUninstallCommand(t, "--settings", path)

	require.NoError(t, err)
	assert.NoFileExists(t, path)
	assert.NoDirExists(t, filepath.Dir(path))
}

func TestClaudeUninstallRejectsInvalidJSONWithoutWriting(t *testing.T) {
	isolateClaudeConfig(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	original := `{"hooks": {`
	require.NoError(t, os.WriteFile(path, []byte(original), 0o600))

	_, _, err := runClaudeUninstallCommand(t, "--settings", path)

	require.Error(t, err)
	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, original, string(data))
	assert.Empty(t, backupsOf(t, path))
}

func TestClaudeUninstallDefaultsToClaudeConfigDir(t *testing.T) {
	home := isolateClaudeConfig(t)
	configDir := filepath.Join(t.TempDir(), "claude-config")
	require.NoError(t, os.MkdirAll(configDir, 0o700))
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	path := filepath.Join(configDir, "settings.json")
	require.NoError(t, os.WriteFile(path, installedSettings(t, `{"model":"opus"}`), 0o600))
	homeSettings := filepath.Join(home, ".claude", "settings.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(homeSettings), 0o700))
	homeInstalled := installedSettings(t, `{}`)
	require.NoError(t, os.WriteFile(homeSettings, homeInstalled, 0o600))

	_, _, err := runClaudeUninstallCommand(t)

	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "treeman claude")
	untouched, err := os.ReadFile(homeSettings)
	require.NoError(t, err)
	assert.Equal(t, string(homeInstalled), string(untouched))
}

// --- backups -----------------------------------------------------------------

func TestBackupsWithinTheSameSecondNeverOverwriteEachOther(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	first, err := writeNewBackup(path, []byte("first"), 0o600)
	require.NoError(t, err)
	second, err := writeNewBackup(path, []byte("second"), 0o600)
	require.NoError(t, err)
	third, err := writeNewBackup(path, []byte("third"), 0o600)
	require.NoError(t, err)

	assert.NotEqual(t, first, second)
	assert.NotEqual(t, second, third)
	assert.NotEqual(t, first, third)
	for want, backup := range map[string]string{"first": first, "second": second, "third": third} {
		data, err := os.ReadFile(backup)
		require.NoError(t, err)
		assert.Equal(t, want, string(data))
		assert.True(t, strings.HasPrefix(backup, path+".treeman-backup-"), backup)
	}
}

func TestClaudeUninstallNeverOverwritesAnEarlierBackup(t *testing.T) {
	isolateClaudeConfig(t)
	path, installed := writeInstalledSettings(t, `{"model":"opus"}`)
	// Occupy the backup names of this second and the next, so the command
	// runs into an existing backup however the clock ticks.
	now := time.Now().UTC()
	earlier := map[string]string{}
	for _, at := range []time.Time{now, now.Add(time.Second)} {
		name := path + ".treeman-backup-" + at.Format("20060102T150405Z")
		earlier[name] = "earlier backup " + name
		require.NoError(t, os.WriteFile(name, []byte(earlier[name]), 0o600))
	}

	_, _, err := runClaudeUninstallCommand(t, "--settings", path)

	require.NoError(t, err)
	for name, content := range earlier {
		data, err := os.ReadFile(name)
		require.NoError(t, err)
		assert.Equal(t, content, string(data), "an earlier backup is never overwritten")
	}
	backups := backupsOf(t, path)
	require.Len(t, backups, 3, "a new backup is written beside the earlier ones")
	found := false
	for _, backup := range backups {
		if _, isEarlier := earlier[backup]; isEarlier {
			continue
		}
		data, err := os.ReadFile(backup)
		require.NoError(t, err)
		assert.Equal(t, string(installed), string(data))
		found = true
	}
	assert.True(t, found)
}

func TestClaudeInstallAndUninstallInTheSameSecondKeepEveryBackup(t *testing.T) {
	isolateClaudeConfig(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	versions := []string{`{"model":"opus"}`}
	require.NoError(t, os.WriteFile(path, []byte(versions[0]), 0o600))

	for i := 0; i < 2; i++ {
		_, _, err := runClaudeInstallCommand(t, "--settings", path)
		require.NoError(t, err)
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		versions = append(versions, string(data))
		_, _, err = runClaudeUninstallCommand(t, "--settings", path)
		require.NoError(t, err)
		data, err = os.ReadFile(path)
		require.NoError(t, err)
		versions = append(versions, string(data))
	}

	backups := backupsOf(t, path)
	require.Len(t, backups, 4, "every write keeps its own backup")
	var contents []string
	for _, backup := range backups {
		data, err := os.ReadFile(backup)
		require.NoError(t, err)
		contents = append(contents, string(data))
	}
	// Each version before the last was backed up once.
	assert.ElementsMatch(t, versions[:4], contents)
}

// --- empty and malformed events ----------------------------------------------

func TestUninstallClaudeHooksLeavesAnEmptyWorktreeCreateSilently(t *testing.T) {
	for name, value := range map[string]string{"null": `null`, "empty list": `[]`} {
		t.Run(name, func(t *testing.T) {
			alone := `{"hooks":{"WorktreeCreate":` + value + `}}`
			updated, changes, kept, err := uninstallClaudeHooks([]byte(alone))
			require.NoError(t, err)
			assert.Empty(t, changes)
			assert.Empty(t, kept, "an empty event is not a foreign hook")
			assert.Equal(t, alone, string(updated))

			withTreeman := `{"hooks":{"WorktreeCreate":` + value + `,` +
				`"WorktreeRemove":[{"hooks":[{"type":"command","command":"treeman claude worktree-remove","timeout":60}]}]}}`
			updated, changes, kept, err = uninstallClaudeHooks([]byte(withTreeman))
			require.NoError(t, err)
			assert.Len(t, changes, 1)
			assert.Empty(t, kept)
			assert.Equal(t, []string{"WorktreeCreate"}, hookEventKeys(t, updated), "the empty event is left as it is")
			var doc struct {
				Hooks map[string]json.RawMessage `json:"hooks"`
			}
			require.NoError(t, json.Unmarshal(updated, &doc))
			assert.JSONEq(t, value, string(doc.Hooks["WorktreeCreate"]))
		})
	}
}

func TestClaudeUninstallWithAnEmptyWorktreeCreateWritesNothing(t *testing.T) {
	for name, value := range map[string]string{"null": `null`, "empty list": `[]`} {
		t.Run(name, func(t *testing.T) {
			isolateClaudeConfig(t)
			path := filepath.Join(t.TempDir(), "settings.json")
			original := `{"hooks":{"WorktreeCreate":` + value + `}}`
			require.NoError(t, os.WriteFile(path, []byte(original), 0o600))

			_, stderr, err := runClaudeUninstallCommand(t, "--settings", path)

			require.NoError(t, err)
			assert.NotContains(t, stderr, "Left hooks")
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, original, string(data))
			assert.Empty(t, backupsOf(t, path))
		})
	}
}

func TestClaudeInstallAndUninstallRejectANonListEventWithoutWriting(t *testing.T) {
	cases := map[string]struct {
		run      func(*testing.T, ...string) (string, string, error)
		settings string
	}{
		"install PermissionRequest object":   {runClaudeInstallCommand, `{"hooks":{"PermissionRequest":{}}}`},
		"install PermissionRequest string":   {runClaudeInstallCommand, `{"hooks":{"PermissionRequest":"x"}}`},
		"uninstall PermissionRequest object": {runClaudeUninstallCommand, `{"hooks":{"PermissionRequest":{},"WorktreeRemove":[{"hooks":[{"type":"command","command":"treeman claude worktree-remove"}]}]}}`},
		"uninstall WorktreeCreate object":    {runClaudeUninstallCommand, `{"hooks":{"WorktreeCreate":{},"WorktreeRemove":[{"hooks":[{"type":"command","command":"treeman claude worktree-remove"}]}]}}`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			isolateClaudeConfig(t)
			path := filepath.Join(t.TempDir(), "settings.json")
			require.NoError(t, os.WriteFile(path, []byte(tc.settings), 0o600))

			_, _, err := tc.run(t, "--settings", path)

			require.Error(t, err)
			data, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			assert.Equal(t, tc.settings, string(data))
			assert.Empty(t, backupsOf(t, path))
		})
	}
}
