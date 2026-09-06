package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/hooks"
	"github.com/shoutcape/treeman/internal/state"
	"github.com/shoutcape/treeman/internal/terminal"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/shoutcape/treeman/internal/worktree"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApproveCreationHooksInteractiveAcceptPersistsAndReuses(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	paths := approvalTestPaths(t, []string{"echo approved"})

	stderr := &bytes.Buffer{}
	first := interactiveApprovalInput("y\n", stderr)
	approved, err := approveCreationHooks(first, paths, creationSetupOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"echo approved"}, approved.hooks.commands)

	reused := interactiveApprovalInput("n\n", &bytes.Buffer{})
	_, err = approveCreationHooks(reused, paths, creationSetupOptions{})
	require.NoError(t, err)
	assert.NotContains(t, reused.ErrOrStderr().(*bytes.Buffer).String(), "Approve and save")
}

func TestApproveCreationHooksRefusesNoAndEOF(t *testing.T) {
	// Anything that is not an explicit yes is a refusal, so answering no and
	// answering nothing stop creation the same way.
	for _, test := range []struct {
		name  string
		input string
	}{
		{name: "no", input: "n\n"},
		{name: "eof", input: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			cmd := interactiveApprovalInput(test.input, &bytes.Buffer{})
			_, err := approveCreationHooks(cmd, approvalTestPaths(t, []string{"echo test"}), creationSetupOptions{})
			require.EqualError(t, err, "hook approval refused")
		})
	}
}

func TestApproveCreationHooksNonInteractiveDoesNotReadOrSave(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cmd := commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{})
	cmd.SetIn(strings.NewReader("y\n"))
	_, err := approveCreationHooks(cmd, approvalTestPaths(t, []string{"echo test"}), creationSetupOptions{})
	require.EqualError(t, err, "hook approval required; rerun with --trust-hooks to authorize this invocation or --skip-hooks to skip hooks")
	assert.NotContains(t, cmd.ErrOrStderr().(*bytes.Buffer).String(), "Approve and save")
}

func TestApproveCreationHooksRejectsStateInsideRepositoryBeforePrompt(t *testing.T) {
	paths := approvalTestPaths(t, []string{"echo test"})
	stateHome := filepath.Join(paths.mainRoot, "state")
	t.Setenv("XDG_STATE_HOME", stateHome)
	stderr := &bytes.Buffer{}
	cmd := interactiveApprovalInput("y\n", stderr)

	_, err := approveCreationHooks(cmd, paths, creationSetupOptions{})
	require.ErrorContains(t, err, "inside repository path")
	assert.Empty(t, stderr.String(), "containment must be rejected before prompting")
	assert.NoDirExists(t, filepath.Join(stateHome, "treeman"))
}

func TestApproveCreationHooksTrustAndSkipBypassState(t *testing.T) {
	for _, test := range []struct {
		name     string
		contents string
		opts     creationSetupOptions
		commands []string
	}{
		{name: "trust ignores malformed state", contents: "{", opts: creationSetupOptions{trustHooks: true}, commands: []string{"echo test"}},
		{name: "skip ignores relative state home", contents: "", opts: creationSetupOptions{skipHooks: true}, commands: []string{"echo test"}},
		{name: "no hooks ignores relative state home", contents: "", opts: creationSetupOptions{}, commands: nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			stateHome := t.TempDir()
			if test.contents != "" {
				require.NoError(t, os.MkdirAll(filepath.Join(stateHome, "treeman"), 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(stateHome, "treeman", "hook-approvals.json"), []byte(test.contents), 0o600))
			} else {
				t.Setenv("XDG_STATE_HOME", "relative-state")
			}
			if test.contents != "" {
				t.Setenv("XDG_STATE_HOME", stateHome)
			}
			got, err := approveCreationHooks(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), approvalTestPaths(t, test.commands), test.opts)
			require.NoError(t, err)
			if test.opts.skipHooks {
				assert.Nil(t, got.hooks.commands)
			} else {
				assert.Equal(t, test.commands, got.hooks.commands)
			}
			if test.contents != "" {
				data, readErr := os.ReadFile(filepath.Join(stateHome, "treeman", "hook-approvals.json"))
				require.NoError(t, readErr)
				assert.Equal(t, test.contents, string(data), "bypass must not rewrite state")
			} else {
				assert.NoDirExists(t, filepath.Join("relative-state", "treeman"), "bypass must not create relative state")
			}
		})
	}
}

func TestApproveCreationHooksRejectsMalformedAndRelativeState(t *testing.T) {
	for _, test := range []struct {
		name string
		data string
	}{
		{name: "malformed", data: "{"},
		{name: "relative scope", data: `{"version":1,"approvals":[{"id":"bad","scope":{"Repository":"relative","ConfigPath":"relative","Phase":"post_create","Commands":["echo test"]},"ApprovedAt":"2025-01-01T00:00:00Z"}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			stateHome := t.TempDir()
			t.Setenv("XDG_STATE_HOME", stateHome)
			_, err := state.NewHookApprovalStore("")
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(stateHome, "treeman", "hook-approvals.json"), []byte(test.data), 0o600))
			_, err = approveCreationHooks(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), approvalTestPaths(t, []string{"echo test"}), creationSetupOptions{trustHooks: false})
			assert.Error(t, err)
		})
	}
}

func TestApproveCreationHooksSnapshotsCommandsAndEscapesToStderr(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	commands := []string{"printf '\033[31mred'"}
	paths := approvalTestPaths(t, commands)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	cmd := interactiveApprovalInput("y\n", stderr)
	cmd.SetOut(stdout)
	approved, err := approveCreationHooks(cmd, paths, creationSetupOptions{})
	require.NoError(t, err)
	commands[0] = "changed"
	assert.Equal(t, "printf '\033[31mred'", approved.hooks.commands[0])
	assert.Contains(t, stderr.String(), `\x1b[31mred`)
	assert.Contains(t, stderr.String(), "Hooks run in the new worktree under ")
	assert.Contains(t, stderr.String(), paths.parentDir)
	assert.NotContains(t, stderr.String(), "Execution directory:")
	assert.Empty(t, stdout.String())
}

func TestApproveCreationHooksReportsSaveFailure(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	statePath := filepath.Join(stateHome, "treeman", "hook-approvals.json")
	cmd := commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{})
	cmd.SetIn(&approvalStateMutatingReader{path: statePath, input: []byte("y\n")})
	cmd.SetContext(context.WithValue(context.Background(), terminalSessionKey{}, terminalSession{
		errorOutput: terminal.Capabilities{Interactive: true},
		standardOut: terminal.Capabilities{Interactive: true},
	}))
	_, err := approveCreationHooks(cmd, approvalTestPaths(t, []string{"echo test"}), creationSetupOptions{})
	assert.ErrorContains(t, err, "save hook approval")
}

func TestApprovedHooksUseSnapshotDuringSetup(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	commands := []string{"printf original > hook-output"}
	paths := approvalTestPaths(t, commands)
	approved, err := approveCreationHooks(interactiveApprovalInput("y\n", &bytes.Buffer{}), paths, creationSetupOptions{skipEnv: true, skipDeps: true, skipDatabase: true})
	require.NoError(t, err)
	paths.config.Hooks.PostCreate[0] = "printf changed > hook-output"
	worktree := t.TempDir()
	var output bytes.Buffer
	summary := setupCreatedWorktree(&output, ui.NewRenderer(&output, terminal.Capabilities{}), approved, git.CreatedWorktree{Path: worktree, Branch: "snapshot"})
	require.Equal(t, completedStatus("completed: 1 succeeded"), summary.hooks)
	data, err := os.ReadFile(filepath.Join(worktree, "hook-output"))
	require.NoError(t, err)
	assert.Equal(t, "original", string(data))
}

func TestHooksApprovalCLIListAndRevoke(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	chdirForTest(t, t.TempDir())
	paths := approvalTestPaths(t, []string{"echo test"})
	scope, err := hooks.NewApprovalScope(paths.protected.CommonDir, paths.configPath, hooks.PostCreatePhase, paths.config.PostCreateHooks())
	require.NoError(t, err)
	store, err := state.NewHookApprovalStore("")
	require.NoError(t, err)
	require.NoError(t, store.Approve(scope))

	out := &bytes.Buffer{}
	list := newHooksCmd()
	list.SetOut(out)
	list.SetArgs([]string{"approvals", "list"})
	require.NoError(t, list.Execute())
	assert.Contains(t, out.String(), scope.ID())
	assert.Contains(t, out.String(), "echo test")

	out.Reset()
	revoke := newHooksCmd()
	revoke.SetOut(out)
	revoke.SetArgs([]string{"approvals", "revoke", scope.ID()})
	require.NoError(t, revoke.Execute())
	assert.Contains(t, out.String(), "Revoked hook approval "+scope.ID())
	found, err := store.Lookup(scope)
	require.NoError(t, err)
	assert.False(t, found)
	unknown := newHooksCmd()
	unknown.SetOut(&bytes.Buffer{})
	unknown.SetArgs([]string{"approvals", "revoke", "unknown-id"})
	assert.EqualError(t, unknown.Execute(), `approval "unknown-id" not found`)
}

func TestCreationPlanAllowsDestinationRaceAfterApproval(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	paths := approvalTestPaths(t, []string{"printf approved > hook-output"})
	paths.path = filepath.Join(paths.parentDir, "feature-test")
	approved, err := approveCreationHooks(interactiveApprovalInput("y\n", &bytes.Buffer{}), paths, creationSetupOptions{skipEnv: true, skipDeps: true, skipDatabase: true})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(paths.path, 0o700))
	selected, err := approved.plan("feature/test")([]git.WorktreeEntry{{Path: paths.path, Branch: "feature/other"}})
	require.NoError(t, err)
	assert.NotEqual(t, paths.path, selected)
	require.NoError(t, os.MkdirAll(selected, 0o700))
	var output bytes.Buffer
	summary := setupCreatedWorktree(&output, ui.NewRenderer(&output, terminal.Capabilities{}), approved, git.CreatedWorktree{Path: selected, Branch: "feature/test"})
	assert.Equal(t, completedStatus("completed: 1 succeeded"), summary.hooks)
	assert.FileExists(t, filepath.Join(selected, "hook-output"))
	assert.NoFileExists(t, filepath.Join(paths.path, "hook-output"))
}

func approvalTestPaths(t *testing.T, commands []string) creationPaths {
	t.Helper()
	root := t.TempDir()
	configPath := filepath.Join(root, ".treeman.toml")
	require.NoError(t, os.WriteFile(configPath, []byte(""), 0o600))
	return creationPaths{
		mainRoot: root, path: filepath.Join(root, ".worktrees", "test"), configPath: configPath,
		parentDir: filepath.Join(root, ".worktrees"),
		protected: structProtected(root), config: config.Config{Hooks: &config.HooksConfig{PostCreate: commands}},
	}
}

func structProtected(root string) (p worktree.Protected) {
	return worktree.Protected{MainRoot: root, CommonDir: filepath.Join(root, ".git")}
}

func commandWithApprovalInput(input string, stderr *bytes.Buffer) *cobra.Command {
	cmd := commandWithOutput(&bytes.Buffer{}, stderr)
	cmd.SetIn(strings.NewReader(input))
	return cmd
}

func interactiveApprovalInput(input string, stderr *bytes.Buffer) *cobra.Command {
	cmd := commandWithApprovalInput(input, stderr)
	interactive := terminal.Capabilities{InputTTY: true, OutputTTY: true, Interactive: true, Width: 120}
	cmd.SetContext(context.WithValue(context.Background(), terminalSessionKey{}, terminalSession{
		errorOutput: interactive,
		standardOut: interactive,
	}))
	return cmd
}

type approvalStateMutatingReader struct {
	path  string
	input []byte
	done  bool
}

func (r *approvalStateMutatingReader) Read(p []byte) (int, error) {
	if !r.done {
		r.done = true
		if err := os.MkdirAll(r.path, 0o700); err != nil {
			return 0, err
		}
	}
	if len(r.input) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.input)
	r.input = r.input[n:]
	return n, nil
}
