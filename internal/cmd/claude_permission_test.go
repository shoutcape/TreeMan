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

const claudeAllowDecision = `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"}}}`

func enterWorktreeInput(cwd, path string) map[string]any {
	return map[string]any{
		"session_id":      "s",
		"cwd":             cwd,
		"hook_event_name": "PermissionRequest",
		"tool_name":       "EnterWorktree",
		"tool_input":      map[string]any{"path": path},
	}
}

func claudePermission(t *testing.T, input any) claudeHookResult {
	t.Helper()
	return runClaudeHookCommand(t, "permission-request", input)
}

// assertApproved requires exactly the allow decision on stdout.
func assertApproved(t *testing.T, result claudeHookResult) {
	t.Helper()
	require.NoErrorf(t, result.err, "stderr: %s", result.stderr)
	require.Equalf(t, claudeAllowDecision, strings.TrimSpace(result.stdout), "stderr: %s", result.stderr)
	var decision struct {
		HookSpecificOutput struct {
			HookEventName string `json:"hookEventName"`
			Decision      struct {
				Behavior string `json:"behavior"`
			} `json:"decision"`
		} `json:"hookSpecificOutput"`
	}
	require.NoError(t, json.Unmarshal([]byte(result.stdout), &decision))
	assert.Equal(t, "PermissionRequest", decision.HookSpecificOutput.HookEventName)
	assert.Equal(t, "allow", decision.HookSpecificOutput.Decision.Behavior)
}

// assertNoDecision requires exit 0 and nothing on stdout, so Claude Code asks
// as usual.
func assertNoDecision(t *testing.T, result claudeHookResult) {
	t.Helper()
	assert.NoError(t, result.err, "the hook never fails")
	assert.Empty(t, result.stdout, "no decision is printed")
}

func TestClaudePermissionApprovesATreemanWorktreeByAbsolutePath(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "perm", repo)

	assertApproved(t, claudePermission(t, enterWorktreeInput(repo, path)))
}

func TestClaudePermissionApprovesFromALinkedWorktreeOrSubdirectory(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "perm", repo)
	other := mustClaudeCreate(t, "other", repo)
	sub := filepath.Join(repo, "deep", "dir")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	assertApproved(t, claudePermission(t, enterWorktreeInput(sub, path)))
	assertApproved(t, claudePermission(t, enterWorktreeInput(other, path)))
}

func TestClaudePermissionApprovesARelativePathAgainstCwd(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	mustClaudeCreate(t, "rel", repo)
	sub := filepath.Join(repo, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	assertApproved(t, claudePermission(t, enterWorktreeInput(repo, ".worktrees/feature-rel")))
	assertApproved(t, claudePermission(t, enterWorktreeInput(sub, "../.worktrees/feature-rel")))
}

func TestClaudePermissionResolvesARelativePathAgainstCwdNotTheProjectDir(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	mustClaudeCreate(t, "relcwd", repo)
	sub := filepath.Join(repo, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	t.Setenv("CLAUDE_PROJECT_DIR", repo)

	assertApproved(t, claudePermission(t, enterWorktreeInput(sub, "../.worktrees/feature-relcwd")))
	// Resolved against cwd (repo/sub), this path is repo/sub/.worktrees/...,
	// which is no worktree. Against the project directory it would be the
	// managed worktree and be approved, so no decision shows cwd is used.
	assertNoDecision(t, claudePermission(t, enterWorktreeInput(sub, ".worktrees/feature-relcwd")))
}

func TestClaudePermissionApprovesAWorktreeTreemanDidNotCreateInsideItsDirectory(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := filepath.Join(repo, ".worktrees", "by-hand")
	gitTest(t, repo, "worktree", "add", "-b", "by-hand", path)

	assertApproved(t, claudePermission(t, enterWorktreeInput(repo, path)))
}

func TestClaudePermissionApprovesAWorktreeInTheConfiguredWorktreeDir(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	external := filepath.Join(canonicalTestDir(t), "trees")
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte("worktree_dir = \""+external+"\"\n"), 0o600))
	path := mustClaudeCreate(t, "configured", repo)
	require.Equal(t, filepath.Join(external, "feature-configured"), path)
	// .worktrees is not TreeMan's directory for this repository.
	stray := filepath.Join(repo, ".worktrees", "stray")
	gitTest(t, repo, "worktree", "add", "-b", "stray", stray)

	assertApproved(t, claudePermission(t, enterWorktreeInput(repo, path)))
	assertNoDecision(t, claudePermission(t, enterWorktreeInput(repo, stray)))
}

func TestClaudePermissionApprovesAWorktreeOfABareLayout(t *testing.T) {
	isolateClaudeHookEnv(t)
	parent := canonicalTestDir(t)
	seed := filepath.Join(parent, "seed")
	gitTest(t, parent, "init", "-b", "main", seed)
	gitTest(t, seed, "commit", "--allow-empty", "-m", "init")
	project := filepath.Join(parent, "proj")
	require.NoError(t, os.MkdirAll(project, 0o755))
	gitTest(t, parent, "clone", "--bare", seed, filepath.Join(project, ".bare"))
	require.NoError(t, os.WriteFile(filepath.Join(project, ".git"), []byte("gitdir: ./.bare\n"), 0o644))
	path := mustClaudeCreate(t, "bare", project)

	assertApproved(t, claudePermission(t, enterWorktreeInput(project, path)))
}

func TestClaudePermissionApprovesAWorktreeOfAPlainBareRepository(t *testing.T) {
	isolateClaudeHookEnv(t)
	parent := canonicalTestDir(t)
	seed := filepath.Join(parent, "seed")
	gitTest(t, parent, "init", "-b", "main", seed)
	gitTest(t, seed, "commit", "--allow-empty", "-m", "init")
	bare := filepath.Join(parent, "repos", "foo.git")
	gitTest(t, parent, "clone", "--bare", seed, bare)
	path := mustClaudeCreate(t, "plainbare", bare)

	assertApproved(t, claudePermission(t, enterWorktreeInput(bare, path)))
}

func TestClaudePermissionGivesNoDecisionForTheMainWorktree(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	linked := mustClaudeCreate(t, "fromlinked", repo)

	assertNoDecision(t, claudePermission(t, enterWorktreeInput(repo, repo)))
	assertNoDecision(t, claudePermission(t, enterWorktreeInput(repo, ".")))
	assertNoDecision(t, claudePermission(t, enterWorktreeInput(linked, repo)))
}

func TestClaudePermissionGivesNoDecisionForAWorktreeOfAnotherRepository(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	mustClaudeCreate(t, "mine", repo)
	other := claudeRepoWithoutOrigin(t)
	foreign := mustClaudeCreate(t, "theirs", other)

	assertNoDecision(t, claudePermission(t, enterWorktreeInput(repo, foreign)))
}

func TestClaudePermissionGivesNoDecisionForAWorktreeOutsideTreemansDirectory(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	elsewhere := filepath.Join(canonicalTestDir(t), "elsewhere")
	gitTest(t, repo, "worktree", "add", "-b", "elsewhere", elsewhere)
	claudeDefault := filepath.Join(repo, ".claude", "worktrees", "plain")
	gitTest(t, repo, "worktree", "add", "-b", "plain", claudeDefault)

	assertNoDecision(t, claudePermission(t, enterWorktreeInput(repo, elsewhere)))
	assertNoDecision(t, claudePermission(t, enterWorktreeInput(repo, claudeDefault)))
}

func TestClaudePermissionGivesNoDecisionForAnUnregisteredDirectoryInsideTreemansDirectory(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	mustClaudeCreate(t, "real", repo)
	plain := filepath.Join(repo, ".worktrees", "just-a-dir")
	require.NoError(t, os.MkdirAll(plain, 0o755))

	assertNoDecision(t, claudePermission(t, enterWorktreeInput(repo, plain)))
	assertNoDecision(t, claudePermission(t, enterWorktreeInput(repo, filepath.Join(repo, ".worktrees"))))
}

func TestClaudePermissionGivesNoDecisionForAMissingDirectory(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "gone", repo)
	require.NoError(t, os.RemoveAll(path))

	assertNoDecision(t, claudePermission(t, enterWorktreeInput(repo, path)))
	assertNoDecision(t, claudePermission(t, enterWorktreeInput(repo, filepath.Join(repo, ".worktrees", "never-existed"))))
}

func TestClaudePermissionGivesNoDecisionForOtherTools(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "tools", repo)

	for _, tool := range []string{"Bash", "Write", "ExitWorktree", "enterworktree", ""} {
		t.Run(tool, func(t *testing.T) {
			input := enterWorktreeInput(repo, path)
			input["tool_name"] = tool
			assertNoDecision(t, claudePermission(t, input))
		})
	}
}

func TestClaudePermissionGivesNoDecisionForBadInput(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	mustClaudeCreate(t, "bad", repo)
	chdirForTest(t, repo)

	for name, input := range map[string]any{
		"empty path":           enterWorktreeInput(repo, ""),
		"no tool_input":        map[string]any{"cwd": repo, "tool_name": "EnterWorktree"},
		"tool_input not obj":   map[string]any{"cwd": repo, "tool_name": "EnterWorktree", "tool_input": "x"},
		"path not a string":    map[string]any{"cwd": repo, "tool_name": "EnterWorktree", "tool_input": map[string]any{"path": 7}},
		"cwd not a repo":       enterWorktreeInput(canonicalTestDir(t), filepath.Join(repo, ".worktrees", "feature-bad")),
		"cwd does not exist":   enterWorktreeInput(filepath.Join(canonicalTestDir(t), "nope"), filepath.Join(repo, ".worktrees", "feature-bad")),
		"empty object":         map[string]any{},
		"JSON array":           []any{"EnterWorktree"},
		"JSON string":          "EnterWorktree",
		"JSON null":            nil,
		"cwd relative and odd": enterWorktreeInput("no/such/dir", ".worktrees/feature-bad"),
	} {
		t.Run(name, func(t *testing.T) {
			assertNoDecision(t, claudePermission(t, input))
		})
	}

	for name, raw := range map[string]string{
		"malformed JSON": `{"tool_name": "EnterWorktree", "tool_input": {`,
		"empty stdin":    ``,
		"not JSON":       `EnterWorktree please`,
	} {
		t.Run(name, func(t *testing.T) {
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			root := New("test", "test", "test")
			root.SetOut(stdout)
			root.SetErr(stderr)
			root.SetIn(strings.NewReader(raw))
			root.SetArgs([]string{"claude", "permission-request"})
			assert.NoError(t, root.Execute(), "the hook never fails")
			assert.Empty(t, stdout.String())
		})
	}
}

// --- CLAUDE_PROJECT_DIR --------------------------------------------------------

func TestClaudePermissionUsesClaudeProjectDirOverCwd(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	mine := mustClaudeCreate(t, "mine", repo)
	other := claudeRepoWithoutOrigin(t)
	theirs := mustClaudeCreate(t, "theirs", other)

	// The session started in repo, then cd'd into the other repository.
	t.Setenv("CLAUDE_PROJECT_DIR", repo)

	assertApproved(t, claudePermission(t, enterWorktreeInput(other, mine)))
	assertNoDecision(t, claudePermission(t, enterWorktreeInput(other, theirs)))
}

func TestClaudePermissionFallsBackToTheCurrentDirectory(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "nocwd", repo)
	chdirForTest(t, repo)

	assertApproved(t, claudePermission(t, map[string]any{"tool_name": "EnterWorktree", "tool_input": map[string]any{"path": path}}))
}

func TestClaudeWorktreeCreateUsesClaudeProjectDirOverCwd(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	other := claudeRepoWithoutOrigin(t)
	t.Setenv("CLAUDE_PROJECT_DIR", repo)

	path := mustClaudeCreate(t, "projdir", other)

	assert.Equal(t, filepath.Join(repo, ".worktrees", "feature-projdir"), path)
	assert.True(t, hasBranch(repo, "feature/projdir"))
	assert.False(t, hasBranch(other, "feature/projdir"), "the cwd's repository is not used")
	assert.NoDirExists(t, filepath.Join(other, ".worktrees"))
	assert.Len(t, recordsForBranch(t, repo, "feature/projdir"), 1)
}

func TestClaudeWorktreeCreateUsesClaudeProjectDirInASubdirectory(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	sub := filepath.Join(repo, "pkg")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	t.Setenv("CLAUDE_PROJECT_DIR", sub)

	path := mustClaudeCreate(t, "projsub", canonicalTestDir(t))

	assert.Equal(t, filepath.Join(repo, ".worktrees", "feature-projsub"), path)
}

func TestClaudeWorktreeCreateWithoutProjectDirOrCwdUsesTheCurrentDirectory(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	chdirForTest(t, repo)

	result := runClaudeHookCommand(t, "worktree-create", map[string]string{"name": "here", "session_id": "s"})

	require.NoErrorf(t, result.err, "stderr: %s", result.stderr)
	assert.Equal(t, filepath.Join(repo, ".worktrees", "feature-here")+"\n", result.stdout)
}

func TestClaudeWorktreeRemoveIgnoresClaudeProjectDir(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "removeme", repo)
	other := claudeRepoWithoutOrigin(t)
	t.Setenv("CLAUDE_PROJECT_DIR", other)

	result := claudeRemove(t, path)

	require.NoErrorf(t, result.err, "stderr: %s", result.stderr)
	assert.NoDirExists(t, path)
	assert.False(t, hasBranch(repo, "feature/removeme"), "the owned merged branch of the worktree's repository is deleted")
	assert.Empty(t, recordsForBranch(t, repo, "feature/removeme"))
}

func TestClaudePermissionApprovesAManagedWorktreeThroughASymlink(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "linked", repo)
	link := filepath.Join(canonicalTestDir(t), "link-to-worktree")
	require.NoError(t, os.Symlink(path, link))

	assertApproved(t, claudePermission(t, enterWorktreeInput(repo, link)))
}

func TestClaudePermissionResolvesDotDotSegments(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "dots", repo)
	mustClaudeCreate(t, "sibling", repo)
	elsewhere := filepath.Join(canonicalTestDir(t), "elsewhere")
	gitTest(t, repo, "worktree", "add", "-b", "elsewhere", elsewhere)
	escape, err := filepath.Rel(path, elsewhere)
	require.NoError(t, err)

	// Both resolve to managed worktrees.
	assertApproved(t, claudePermission(t, enterWorktreeInput(repo, path+"/../feature-dots")))
	assertApproved(t, claudePermission(t, enterWorktreeInput(repo, path+"/../feature-sibling")))
	assertApproved(t, claudePermission(t, enterWorktreeInput(repo, "sub/../.worktrees/feature-dots")))
	// These escape TreeMan's directory: to an unmanaged worktree and to the
	// main worktree.
	assertNoDecision(t, claudePermission(t, enterWorktreeInput(repo, path+"/"+escape)))
	assertNoDecision(t, claudePermission(t, enterWorktreeInput(repo, ".worktrees/feature-dots/../..")))
}

func TestClaudePermissionWithWorktreeDirAtTheRepositoryParent(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	parent := filepath.Dir(repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte("worktree_dir = \""+parent+"\"\n"), 0o600))
	path := mustClaudeCreate(t, "atparent", repo)
	require.Equal(t, parent, filepath.Dir(path))
	byHand := filepath.Join(parent, "by-hand")
	gitTest(t, repo, "worktree", "add", "-b", "by-hand", byHand)
	// Another repository living in the same parent directory.
	seed := filepath.Join(parent, "seed")
	foreign := filepath.Join(parent, "seed-wt")
	gitTest(t, seed, "worktree", "add", "-b", "foreign", foreign)

	assertApproved(t, claudePermission(t, enterWorktreeInput(repo, path)))
	assertApproved(t, claudePermission(t, enterWorktreeInput(repo, byHand)))
	assertNoDecision(t, claudePermission(t, enterWorktreeInput(repo, foreign)))
	assertNoDecision(t, claudePermission(t, enterWorktreeInput(repo, seed)))
	assertNoDecision(t, claudePermission(t, enterWorktreeInput(repo, repo)))
}
