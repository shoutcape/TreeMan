package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/shoutcape/treeman/internal/fsutil"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
)

// claudeHookEvent is one Claude Code hook event TreeMan registers.
type claudeHookEvent struct {
	event   string
	matcher string
	command string
	timeout int
	// shared events take any number of hooks, so TreeMan adds its entry
	// beside the others. Claude Code uses one WorktreeCreate and one
	// WorktreeRemove answer, so TreeMan owns those events outright.
	shared bool
}

// claudeHookEvents are the entries `treeman claude install` writes. Creation
// may fetch and install dependencies, so it gets Claude Code's longest
// timeout; removal does no network work, and approval reads only local Git
// state.
var claudeHookEvents = []claudeHookEvent{
	{event: "WorktreeCreate", command: "treeman claude worktree-create", timeout: 600},
	{event: "WorktreeRemove", command: "treeman claude worktree-remove", timeout: 60},
	{event: "PermissionRequest", matcher: "EnterWorktree", command: "treeman claude permission-request", timeout: 30, shared: true},
}

type claudeInstallOptions struct {
	settingsPath string
	replace      bool
	dryRun       bool
}

func newClaudeInstallCmd() *cobra.Command {
	var options claudeInstallOptions
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Register TreeMan's worktree hooks in Claude Code settings",
		Long: `Add WorktreeCreate, WorktreeRemove, and PermissionRequest hooks that run
TreeMan to Claude Code's user settings, normally ~/.claude/settings.json
($CLAUDE_CONFIG_DIR/settings.json when CLAUDE_CONFIG_DIR is set).

Every other setting is kept, in its original order. Running it again changes
nothing. Before writing, the original file is copied next to it with a
.treeman-backup-<time> suffix.

A WorktreeCreate or WorktreeRemove hook that is not TreeMan's is left alone
and reported; pass --replace to replace it. TreeMan's PermissionRequest hook
is added beside any others.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.settingsPath == "" {
				path, err := claudeUserSettingsPath()
				if err != nil {
					return err
				}
				options.settingsPath = path
			}
			return runClaudeInstall(cmd, options)
		},
	}
	cmd.Flags().StringVar(&options.settingsPath, "settings", "", "Claude Code settings file to edit")
	cmd.Flags().BoolVar(&options.replace, "replace", false, "Replace existing WorktreeCreate and WorktreeRemove hooks")
	cmd.Flags().BoolVar(&options.dryRun, "dry-run", false, "Show what would change without writing")
	return cmd
}

func newClaudeUninstallCmd() *cobra.Command {
	var options claudeInstallOptions
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove TreeMan's hooks from Claude Code settings",
		Long: `Remove the hooks "treeman claude install" added from Claude Code's user
settings, normally ~/.claude/settings.json ($CLAUDE_CONFIG_DIR/settings.json
when CLAUDE_CONFIG_DIR is set).

Only TreeMan's own entries are removed; other hooks and settings are kept, in
their original order. Claude Code then creates worktrees itself again. Before
writing, the original file is copied next to it with a .treeman-backup-<time>
suffix. Running it again changes nothing.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.settingsPath == "" {
				path, err := claudeUserSettingsPath()
				if err != nil {
					return err
				}
				options.settingsPath = path
			}
			return runClaudeUninstall(cmd, options)
		},
	}
	cmd.Flags().StringVar(&options.settingsPath, "settings", "", "Claude Code settings file to edit")
	cmd.Flags().BoolVar(&options.dryRun, "dry-run", false, "Show what would change without writing")
	return cmd
}

func claudeUserSettingsPath() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "settings.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find the home directory: %w", err)
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

func runClaudeInstall(cmd *cobra.Command, options claudeInstallOptions) error {
	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	path := options.settingsPath

	original, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if !exists || len(bytes.TrimSpace(original)) == 0 {
		original = []byte("{}")
	}

	updated, changes, conflicts, err := installClaudeHooks(original, options.replace)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if len(conflicts) > 0 {
		for _, conflict := range conflicts {
			fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", conflict))
		}
		return fmt.Errorf("%s already has other worktree hooks; rerun with --replace to replace them with TreeMan's", path)
	}
	if len(changes) == 0 {
		fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Claude Code worktree hooks are already installed in "+path))
		warnTreemanNotOnPath(out, render)
		return nil
	}
	for _, change := range changes {
		fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", change))
	}
	if options.dryRun {
		fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "Dry run: "+path+" was not changed."))
		return nil
	}
	if err := writeClaudeSettings(out, render, path, original, exists, updated); err != nil {
		return err
	}
	fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Installed Claude Code worktree hooks in "+path))
	fmt.Fprintln(out, render.Muted("  Claude Code reads hooks at startup; restart running sessions to use them."))
	warnTreemanNotOnPath(out, render)
	return nil
}

func runClaudeUninstall(cmd *cobra.Command, options claudeInstallOptions) error {
	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	path := options.settingsPath

	original, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "TreeMan's Claude Code hooks are not installed: "+path+" does not exist"))
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if len(bytes.TrimSpace(original)) == 0 {
		fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "TreeMan's Claude Code hooks are not installed in "+path))
		return nil
	}

	updated, changes, kept, err := uninstallClaudeHooks(original)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	for _, note := range kept {
		fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", note))
	}
	if len(changes) == 0 {
		fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "TreeMan's Claude Code hooks are not installed in "+path))
		return nil
	}
	for _, change := range changes {
		fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", change))
	}
	if options.dryRun {
		fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "Dry run: "+path+" was not changed."))
		return nil
	}
	if err := writeClaudeSettings(out, render, path, original, true, updated); err != nil {
		return err
	}
	fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Removed TreeMan's Claude Code hooks from "+path))
	fmt.Fprintln(out, render.Muted("  Claude Code reads hooks at startup; restart running sessions to stop using them."))
	return nil
}

// writeNewBackup writes data to path.treeman-backup-<time>, adding -2, -3, and
// so on when that name is taken, so a backup made in the same second as
// another never replaces it.
func writeNewBackup(path string, data []byte, mode os.FileMode) (string, error) {
	base := fmt.Sprintf("%s.treeman-backup-%s", path, time.Now().UTC().Format("20060102T150405Z"))
	for i := 1; ; i++ {
		backup := base
		if i > 1 {
			backup = fmt.Sprintf("%s-%d", base, i)
		}
		file, err := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = file.Write(data)
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		return backup, err
	}
}

// writeClaudeSettings backs up an existing settings file, then replaces it
// with updated, keeping its permissions and any symlink to it.
func writeClaudeSettings(out io.Writer, render ui.Renderer, path string, original []byte, exists bool, updated []byte) error {
	mode := os.FileMode(0o600)
	if exists {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		mode = info.Mode().Perm()
		backup, err := writeNewBackup(path, original, mode)
		if err != nil {
			return fmt.Errorf("back up %s: %w", path, err)
		}
		fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "Backed up the original settings to "+backup))
	} else if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// Resolve a symlinked settings file so the link itself is kept.
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
	}
	if err := fsutil.AtomicWriteFile(target, updated, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func warnTreemanNotOnPath(out io.Writer, render ui.Renderer) {
	if _, err := exec.LookPath("treeman"); err != nil {
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", "treeman is not on PATH; Claude Code runs the hooks as `treeman ...` and needs it there"))
	}
}

// installClaudeHooks returns settings with TreeMan's hooks in place, what it
// changed, and any foreign hooks that stop it. Members keep their order and
// their values are copied through unchanged, so the only difference in the
// file is the hook entries and indentation.
func installClaudeHooks(settings []byte, replace bool) ([]byte, []string, []string, error) {
	root, err := parseOrderedObject(settings)
	if err != nil {
		return nil, nil, nil, err
	}
	hooks := orderedObject{}
	if raw, ok := root.get("hooks"); ok && string(bytes.TrimSpace(raw)) != "null" {
		if hooks, err = parseOrderedObject(raw); err != nil {
			return nil, nil, nil, fmt.Errorf("hooks: %w", err)
		}
	}

	var changes, conflicts []string
	for _, hook := range claudeHookEvents {
		want := claudeHookEntry(hook)
		raw, present := hooks.get(hook.event)
		if hook.shared {
			groups, err := parseHookGroups(raw, present)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("hooks.%s: %w", hook.event, err)
			}
			if containsTreemanGroup(groups, hook) {
				continue
			}
			group := treemanHookGroup(hook)
			groups = append(groups, group)
			list, err := marshalHookGroups(groups)
			if err != nil {
				return nil, nil, nil, err
			}
			changes = append(changes, fmt.Sprintf("Add to hooks.%s: %q for %s", hook.event, hook.command, hook.matcher))
			hooks.set(hook.event, list)
			continue
		}
		if present {
			same, err := jsonEqual(raw, want)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("hooks.%s: %w", hook.event, err)
			}
			if same {
				continue
			}
			if !replace {
				conflicts = append(conflicts, fmt.Sprintf("hooks.%s is already set to %s", hook.event, compactJSON(raw)))
				continue
			}
			changes = append(changes, fmt.Sprintf("Replace hooks.%s %s with %q", hook.event, compactJSON(raw), hook.command))
		} else {
			changes = append(changes, fmt.Sprintf("Add hooks.%s: %q", hook.event, hook.command))
		}
		hooks.set(hook.event, want)
	}
	if len(conflicts) > 0 || len(changes) == 0 {
		return settings, nil, conflicts, nil
	}
	hooksRaw, err := hooks.marshal()
	if err != nil {
		return nil, nil, nil, err
	}
	root.set("hooks", hooksRaw)
	compact, err := root.marshal()
	if err != nil {
		return nil, nil, nil, err
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, compact, "", "  "); err != nil {
		return nil, nil, nil, err
	}
	indented.WriteByte('\n')
	return indented.Bytes(), changes, nil, nil
}

// uninstallClaudeHooks returns settings without TreeMan's hooks, what it
// removed, and notes on hooks it left because they are not TreeMan's. An event
// left with no hooks is removed, and so is a hooks object left empty.
func uninstallClaudeHooks(settings []byte) ([]byte, []string, []string, error) {
	root, err := parseOrderedObject(settings)
	if err != nil {
		return nil, nil, nil, err
	}
	raw, ok := root.get("hooks")
	if !ok || string(bytes.TrimSpace(raw)) == "null" {
		return settings, nil, nil, nil
	}
	hooks, err := parseOrderedObject(raw)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("hooks: %w", err)
	}

	var changes, kept []string
	for _, hook := range claudeHookEvents {
		raw, present := hooks.get(hook.event)
		if !present {
			continue
		}
		groups, err := parseHookGroups(raw, true)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("hooks.%s: %w", hook.event, err)
		}
		remaining := groups[:0:0]
		for _, group := range groups {
			if !isTreemanGroup(group, hook) {
				remaining = append(remaining, group)
			}
		}
		if len(remaining) == len(groups) {
			if !hook.shared && len(groups) > 0 {
				kept = append(kept, fmt.Sprintf("Left hooks.%s as it is: it does not run TreeMan", hook.event))
			}
			continue
		}
		changes = append(changes, fmt.Sprintf("Remove %q from hooks.%s", hook.command, hook.event))
		if len(remaining) == 0 {
			hooks.remove(hook.event)
			continue
		}
		list, err := marshalHookGroups(remaining)
		if err != nil {
			return nil, nil, nil, err
		}
		hooks.set(hook.event, list)
	}
	if len(changes) == 0 {
		return settings, nil, kept, nil
	}
	if len(hooks) == 0 {
		root.remove("hooks")
	} else {
		hooksRaw, err := hooks.marshal()
		if err != nil {
			return nil, nil, nil, err
		}
		root.set("hooks", hooksRaw)
	}
	compact, err := root.marshal()
	if err != nil {
		return nil, nil, nil, err
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, compact, "", "  "); err != nil {
		return nil, nil, nil, err
	}
	indented.WriteByte('\n')
	return indented.Bytes(), changes, kept, nil
}

// claudeHookGroup is one matcher group of a hook event: an optional matcher
// and the commands it runs.
type claudeHookGroup struct {
	Matcher string             `json:"matcher,omitempty"`
	Hooks   []claudeHookAction `json:"hooks"`
}

type claudeHookAction struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

func treemanHookGroup(hook claudeHookEvent) json.RawMessage {
	group, _ := marshalNoEscape(claudeHookGroup{
		Matcher: hook.matcher,
		Hooks:   []claudeHookAction{{Type: "command", Command: hook.command, Timeout: hook.timeout}},
	})
	return group
}

func claudeHookEntry(hook claudeHookEvent) json.RawMessage {
	entry, _ := marshalHookGroups([]json.RawMessage{treemanHookGroup(hook)})
	return entry
}

// parseHookGroups reads an event's list of matcher groups, keeping each one
// as written. An absent or null event has none.
func parseHookGroups(raw json.RawMessage, present bool) ([]json.RawMessage, error) {
	if !present || string(bytes.TrimSpace(raw)) == "null" {
		return nil, nil
	}
	var groups []json.RawMessage
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, fmt.Errorf("expected a list of hook groups: %w", err)
	}
	return groups, nil
}

func marshalHookGroups(groups []json.RawMessage) (json.RawMessage, error) {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, group := range groups {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(group)
	}
	buf.WriteByte(']')
	return buf.Bytes(), nil
}

func containsTreemanGroup(groups []json.RawMessage, hook claudeHookEvent) bool {
	for _, group := range groups {
		if isTreemanGroup(group, hook) {
			return true
		}
	}
	return false
}

// isTreemanGroup reports whether a matcher group is the one TreeMan installs
// for hook: its matcher, and TreeMan's command as its only hook. The timeout
// may have been edited and still counts.
func isTreemanGroup(raw json.RawMessage, hook claudeHookEvent) bool {
	var group claudeHookGroup
	if err := json.Unmarshal(raw, &group); err != nil {
		return false
	}
	return group.Matcher == hook.matcher && len(group.Hooks) == 1 &&
		group.Hooks[0].Type == "command" && group.Hooks[0].Command == hook.command
}

func jsonEqual(a, b json.RawMessage) (bool, error) {
	var left, right any
	if err := json.Unmarshal(a, &left); err != nil {
		return false, err
	}
	if err := json.Unmarshal(b, &right); err != nil {
		return false, err
	}
	l, _ := json.Marshal(left)
	r, _ := json.Marshal(right)
	return bytes.Equal(l, r), nil
}

func compactJSON(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}

func marshalNoEscape(value any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// orderedObject is a JSON object whose member order is kept and whose values
// are carried as written.
type orderedObject []orderedMember

type orderedMember struct {
	key   string
	value json.RawMessage
}

func parseOrderedObject(data []byte) (orderedObject, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("expected a JSON object")
	}
	object := orderedObject{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		key, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("invalid JSON object key")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		object = append(object, orderedMember{key: key, value: value})
	}
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("invalid JSON: unexpected data after the top-level object")
	}
	return object, nil
}

func (o orderedObject) get(key string) (json.RawMessage, bool) {
	for i := len(o) - 1; i >= 0; i-- {
		if o[i].key == key {
			return o[i].value, true
		}
	}
	return nil, false
}

// set replaces the last member named key, as JSON readers use the last of
// duplicate keys, or appends a new one.
func (o *orderedObject) set(key string, value json.RawMessage) {
	for i := len(*o) - 1; i >= 0; i-- {
		if (*o)[i].key == key {
			(*o)[i].value = value
			return
		}
	}
	*o = append(*o, orderedMember{key: key, value: value})
}

// remove deletes every member named key.
func (o *orderedObject) remove(key string) {
	kept := (*o)[:0]
	for _, member := range *o {
		if member.key != key {
			kept = append(kept, member)
		}
	}
	*o = kept
}

func (o orderedObject) marshal() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, member := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := marshalNoEscape(member.key)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(member.value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
