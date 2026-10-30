package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shoutcape/treeman/internal/agentworktree"
	"github.com/shoutcape/treeman/internal/hooks"
	"github.com/shoutcape/treeman/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- name mapping ------------------------------------------------------------

func TestClaudeBranchNameMapping(t *testing.T) {
	tests := map[string]string{
		"foo":                 "feature/foo",
		"feature-foo":         "feature/foo",
		"feature-feature-foo": "feature/feature-foo",
		"my-thing":            "feature/my-thing",
		"v1.2-Mixed":          "feature/v1.2-Mixed",
		"agent-a1b2c3":        "feature/agent-a1b2c3",
		"pr-":                 "feature/pr-",
		"pr-abc":              "feature/pr-abc",
	}
	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := claudeBranchName(name)
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func TestClaudeBranchNameRejectsNamesThatLeaveNoBranch(t *testing.T) {
	for _, name := range []string{"feature-", "bad name", "x~1"} {
		t.Run(name, func(t *testing.T) {
			_, err := claudeBranchName(name)
			assert.Error(t, err)
		})
	}
}

// --- fixtures ----------------------------------------------------------------

// isolateClaudeHookEnv keeps the commands away from the user's state and
// restores the variables the hooks set on the process.
func isolateClaudeHookEnv(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "TreeMan Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "TreeMan Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	for _, key := range []string{"GIT_TERMINAL_PROMPT", "GH_PROMPT_DISABLED", "GCM_INTERACTIVE", "GIT_SSH_COMMAND", "TREEMAN_CD_FILE"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
	wd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(wd) })
}

func canonicalTestDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

// claudeRepoWithOrigin is a clone of a local bare origin whose origin/HEAD
// names main.
func claudeRepoWithOrigin(t *testing.T) (repo, origin string) {
	t.Helper()
	isolateClaudeHookEnv(t)
	parent := canonicalTestDir(t)
	origin = filepath.Join(parent, "origin.git")
	seed := filepath.Join(parent, "seed")
	repo = filepath.Join(parent, "repo")
	gitTest(t, parent, "init", "--bare", "--initial-branch=main", origin)
	gitTest(t, parent, "init", "-b", "main", seed)
	require.NoError(t, os.WriteFile(filepath.Join(seed, "README.md"), []byte("initial\n"), 0o644))
	gitTest(t, seed, "add", "README.md")
	gitTest(t, seed, "commit", "-m", "initial")
	gitTest(t, seed, "remote", "add", "origin", origin)
	gitTest(t, seed, "push", "origin", "main")
	gitTest(t, parent, "clone", origin, repo)
	gitTest(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	return repo, origin
}

// claudeRepoWithoutOrigin has commits but no remote.
func claudeRepoWithoutOrigin(t *testing.T) string {
	t.Helper()
	isolateClaudeHookEnv(t)
	repo := filepath.Join(canonicalTestDir(t), "local")
	gitTest(t, filepath.Dir(repo), "init", "-b", "main", repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "README.md"), []byte("initial\n"), 0o644))
	gitTest(t, repo, "add", "README.md")
	gitTest(t, repo, "commit", "-m", "initial")
	return repo
}

type claudeHookResult struct {
	stdout string
	stderr string
	err    error
}

func runClaudeHookCommand(t *testing.T, sub string, input any) claudeHookResult {
	t.Helper()
	data, err := json.Marshal(input)
	require.NoError(t, err)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	root := New("test", "test", "test")
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetIn(bytes.NewReader(data))
	root.SetArgs([]string{"claude", sub})
	err = root.Execute()
	return claudeHookResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func claudeCreate(t *testing.T, name, cwd string) claudeHookResult {
	t.Helper()
	return runClaudeHookCommand(t, "worktree-create", map[string]string{"name": name, "cwd": cwd, "session_id": "session-" + name})
}

// mustClaudeCreate requires success and that stdout holds only the path.
func mustClaudeCreate(t *testing.T, name, cwd string) string {
	t.Helper()
	result := claudeCreate(t, name, cwd)
	require.NoErrorf(t, result.err, "worktree-create %q failed: %s", name, result.stderr)
	path := strings.TrimSuffix(result.stdout, "\n")
	require.NotContains(t, path, "\n", "stdout must hold only the worktree path, got %q", result.stdout)
	require.True(t, filepath.IsAbs(path), "path must be absolute: %q", path)
	require.DirExists(t, path)
	return path
}

func claudeRemove(t *testing.T, path string) claudeHookResult {
	t.Helper()
	return runClaudeHookCommand(t, "worktree-remove", map[string]string{"worktree_path": path, "session_id": "s", "cwd": path})
}

func commonDirOf(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(gitTestOutput(t, dir, "rev-parse", "--path-format=absolute", "--git-common-dir"))
}

func claudeRecords(t *testing.T, dir string) []agentworktree.Record {
	t.Helper()
	records, err := agentworktree.Open(commonDirOf(t, dir)).List()
	require.NoError(t, err)
	return records
}

func recordsForBranch(t *testing.T, dir, branch string) []agentworktree.Record {
	t.Helper()
	var out []agentworktree.Record
	for _, record := range claudeRecords(t, dir) {
		if record.Branch == branch {
			out = append(out, record)
		}
	}
	return out
}

func hasBranch(dir, branch string) bool {
	return exec.Command("git", "-C", dir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil
}

func currentBranch(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(gitTestOutput(t, dir, "branch", "--show-current"))
}

func revParse(t *testing.T, dir, rev string) string {
	t.Helper()
	return strings.TrimSpace(gitTestOutput(t, dir, "rev-parse", rev))
}

func assertNoUpstream(t *testing.T, dir, branch string) {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "config", "--get", "branch."+branch+".merge").CombinedOutput()
	assert.Errorf(t, err, "branch %s must have no upstream, has %s", branch, out)
}

// userBranch creates a branch whose reflog is born clearly after anything
// the hook recorded, as a user re-creating a name later would.
func userBranch(t *testing.T, dir, branch string) {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "branch", branch)
	cmd.Env = append(os.Environ(), "GIT_COMMITTER_DATE=@"+strconv.FormatInt(time.Now().Unix()+60, 10)+" +0000")
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "%s", out)
}

func worktreeCount(t *testing.T, dir string) int {
	t.Helper()
	return strings.Count(gitTestOutput(t, dir, "worktree", "list", "--porcelain"), "worktree ")
}

// --- worktree-create ---------------------------------------------------------

func TestClaudeWorktreeCreatePrintsOnlyThePathAndRecordsOwnership(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)

	result := claudeCreate(t, "foo", repo)

	require.NoErrorf(t, result.err, "stderr: %s", result.stderr)
	want := filepath.Join(repo, ".worktrees", "feature-foo")
	assert.Equal(t, want+"\n", result.stdout, "stdout must be exactly the worktree path")
	assert.DirExists(t, want)
	assert.Equal(t, "feature/foo", currentBranch(t, want))
	assert.Equal(t, revParse(t, repo, "origin/main"), revParse(t, repo, "feature/foo"))
	assertNoUpstream(t, repo, "feature/foo")

	recordPath := filepath.Join(commonDirOf(t, repo), "treeman", "agent-worktrees.json")
	assert.FileExists(t, recordPath)
	records := claudeRecords(t, repo)
	require.Len(t, records, 1)
	record := records[0]
	assert.Equal(t, "claude", record.Agent)
	assert.Equal(t, "foo", record.Name)
	assert.Equal(t, "feature/foo", record.Branch)
	assert.True(t, record.BranchCreated)
	assert.Equal(t, revParse(t, repo, "feature/foo"), record.CreatedSHA)
	assert.False(t, record.CreatedAt.IsZero())
	assert.Equal(t, want, record.WorktreePath)
	assert.Equal(t, "session-foo", record.SessionID)
}

func TestClaudeWorktreeCreateStripsOneFeaturePrefix(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)

	path := mustClaudeCreate(t, "feature-bar", repo)

	assert.Equal(t, filepath.Join(repo, ".worktrees", "feature-bar"), path)
	assert.Equal(t, "feature/bar", currentBranch(t, path))
	assert.False(t, hasBranch(repo, "feature/feature-bar"))
}

func TestClaudeWorktreeCreateReturnsTheSameWorktreeForTheSameName(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	first := mustClaudeCreate(t, "again", repo)
	require.NoError(t, os.WriteFile(filepath.Join(first, "work.txt"), []byte("in progress"), 0o644))
	count := worktreeCount(t, repo)

	second := mustClaudeCreate(t, "again", repo)

	assert.Equal(t, first, second)
	assert.Equal(t, count, worktreeCount(t, repo))
	assert.FileExists(t, filepath.Join(second, "work.txt"))
	assert.Len(t, claudeRecords(t, repo), 1)
}

func TestClaudeWorktreeCreateFromASubdirectoryOrLinkedWorktreeUsesTheMainCheckout(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	sub := filepath.Join(repo, "deep", "dir")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	fromSub := mustClaudeCreate(t, "fromsub", sub)
	assert.Equal(t, filepath.Join(repo, ".worktrees", "feature-fromsub"), fromSub)

	fromLinked := mustClaudeCreate(t, "fromlinked", fromSub)
	assert.Equal(t, filepath.Join(repo, ".worktrees", "feature-fromlinked"), fromLinked)
}

func TestClaudeWorktreeCreateRefusesABranchInTheMainWorktree(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	gitTest(t, repo, "checkout", "-b", "feature/inmain")

	result := claudeCreate(t, "inmain", repo)

	require.Error(t, result.err)
	assert.Empty(t, result.stdout, "a failure prints no path")
	assert.NotEmpty(t, result.err.Error())
	assert.Equal(t, 1, worktreeCount(t, repo))
}

func TestClaudeWorktreeCreateRefusesAWorktreeItDidNotCreate(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	userWorktree := filepath.Join(canonicalTestDir(t), "user-wt")
	gitTest(t, repo, "worktree", "add", "-b", "feature/taken", userWorktree)

	result := claudeCreate(t, "taken", repo)

	require.Error(t, result.err)
	assert.Empty(t, result.stdout)
	assert.NotContains(t, result.stdout, userWorktree)
	assert.Empty(t, claudeRecords(t, repo))
}

func TestClaudeWorktreeCreateNeverReturnsTheMainWorktreeEvenWithARecord(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	gitTest(t, repo, "checkout", "-b", "feature/forged")
	require.NoError(t, agentworktree.Open(commonDirOf(t, repo)).Put(agentworktree.Record{
		Agent: "claude", Name: "forged", Branch: "feature/forged", BranchCreated: true,
		CreatedAt: time.Now().UTC(), WorktreePath: repo,
	}))

	result := claudeCreate(t, "forged", repo)

	require.Error(t, result.err)
	assert.Empty(t, result.stdout)
}

func TestClaudeWorktreeCreateReattachesAnExistingBranchWithoutClaimingIt(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	gitTest(t, repo, "branch", "feature/existing")
	sha := revParse(t, repo, "feature/existing")

	path := mustClaudeCreate(t, "existing", repo)

	assert.Equal(t, filepath.Join(repo, ".worktrees", "feature-existing"), path)
	assert.Equal(t, "feature/existing", currentBranch(t, path))
	assert.Equal(t, sha, revParse(t, repo, "feature/existing"))
	records := recordsForBranch(t, repo, "feature/existing")
	require.Len(t, records, 1)
	assert.False(t, records[0].BranchCreated, "an attached branch was not created by TreeMan")

	// The branch is merged (it is main's tip), but it is the user's.
	result := claudeRemove(t, path)
	require.NoErrorf(t, result.err, "stderr: %s", result.stderr)
	assert.NoDirExists(t, path)
	assert.True(t, hasBranch(repo, "feature/existing"), "a branch TreeMan did not create is never deleted")
}

func TestClaudeWorktreeCreateCopiesEnvFiles(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env"), []byte("SECRET=1\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env.local"), []byte("LOCAL=1\n"), 0o600))

	path := mustClaudeCreate(t, "envs", repo)

	data, err := os.ReadFile(filepath.Join(path, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "SECRET=1\n", string(data))
	assert.FileExists(t, filepath.Join(path, ".env.local"))
}

func TestClaudeWorktreeCreateHonorsWorktreeDir(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	external := filepath.Join(canonicalTestDir(t), "trees")
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte("worktree_dir = \""+external+"\"\n"), 0o600))

	path := mustClaudeCreate(t, "external", repo)

	assert.Equal(t, filepath.Join(external, "feature-external"), path)
}

func TestClaudeWorktreeCreateSuffixesACollidingSlug(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	plain := filepath.Join(repo, ".worktrees", "feature-coll")
	gitTest(t, repo, "worktree", "add", "-b", "feature-coll", plain)

	path := mustClaudeCreate(t, "coll", repo)

	assert.NotEqual(t, plain, path)
	assert.Equal(t, filepath.Join(repo, ".worktrees"), filepath.Dir(path))
	assert.True(t, strings.HasPrefix(filepath.Base(path), "feature-coll-"), "got %s", path)
	assert.Equal(t, "feature/coll", currentBranch(t, path))
	assert.Equal(t, "feature-coll", currentBranch(t, plain))
}

func TestClaudeWorktreeCreateStartsFromFreshlyFetchedOriginDefault(t *testing.T) {
	repo, origin := claudeRepoWithOrigin(t)
	other := filepath.Join(canonicalTestDir(t), "other")
	gitTest(t, filepath.Dir(other), "clone", origin, other)
	gitTest(t, other, "commit", "--allow-empty", "-m", "upstream work")
	gitTest(t, other, "push", "origin", "main")
	fresh := revParse(t, other, "HEAD")
	require.NotEqual(t, fresh, revParse(t, repo, "origin/main"))

	path := mustClaudeCreate(t, "fresh", repo)

	assert.Equal(t, fresh, revParse(t, path, "HEAD"))
	assertNoUpstream(t, repo, "feature/fresh")
}

func TestClaudeWorktreeCreateFallsBackToCachedOriginWhenFetchFails(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	cached := revParse(t, repo, "origin/main")
	// Local main moves ahead so HEAD and the cached origin differ.
	gitTest(t, repo, "commit", "--allow-empty", "-m", "local only")
	gitTest(t, repo, "remote", "set-url", "origin", filepath.Join(canonicalTestDir(t), "missing.git"))

	result := claudeCreate(t, "offline", repo)

	require.NoErrorf(t, result.err, "stderr: %s", result.stderr)
	path := strings.TrimSpace(result.stdout)
	assert.Equal(t, filepath.Join(repo, ".worktrees", "feature-offline"), path)
	assert.Equal(t, cached, revParse(t, path, "HEAD"), "starts from the cached origin/main")
	assert.Contains(t, strings.ToLower(result.stderr), "cached", "warns about the fallback")
	assertNoUpstream(t, repo, "feature/offline")
}

func TestClaudeWorktreeCreateWithoutOriginStartsFromHEAD(t *testing.T) {
	repo := claudeRepoWithoutOrigin(t)
	gitTest(t, repo, "commit", "--allow-empty", "-m", "head commit")
	head := revParse(t, repo, "HEAD")

	result := claudeCreate(t, "local", repo)

	require.NoErrorf(t, result.err, "stderr: %s", result.stderr)
	path := strings.TrimSpace(result.stdout)
	assert.Equal(t, filepath.Join(repo, ".worktrees", "feature-local"), path)
	assert.Equal(t, head, revParse(t, path, "HEAD"))
	assert.Contains(t, result.stderr, "HEAD", "warns that it starts from HEAD")
	assertNoUpstream(t, repo, "feature/local")
}

func TestClaudeWorktreeCreateSkipsUnapprovedHooksWithAWarning(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte("[hooks]\npost_create = [\"touch hook-ran\"]\n"), 0o600))

	result := claudeCreate(t, "nohooks", repo)

	require.NoErrorf(t, result.err, "creation never fails for missing consent: %s", result.stderr)
	path := strings.TrimSpace(result.stdout)
	assert.Equal(t, filepath.Join(repo, ".worktrees", "feature-nohooks"), path)
	assert.NoFileExists(t, filepath.Join(path, "hook-ran"))
	assert.Contains(t, result.stderr, "hook")
	assert.Contains(t, strings.ToLower(result.stderr), "approv")
}

func TestClaudeWorktreeCreateRunsHooksWithASavedApproval(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	configPath := filepath.Join(repo, ".treeman.toml")
	commands := []string{"touch hook-ran"}
	require.NoError(t, os.WriteFile(configPath, []byte("[hooks]\npost_create = [\"touch hook-ran\"]\n"), 0o600))
	commonDir := commonDirOf(t, repo)
	scope, err := hooks.NewApprovalScope(commonDir, configPath, hooks.PostCreatePhase, commands)
	require.NoError(t, err)
	store, err := state.NewHookApprovalStore(commonDir)
	require.NoError(t, err)
	require.NoError(t, store.Approve(scope))

	result := claudeCreate(t, "withhooks", repo)

	require.NoErrorf(t, result.err, "stderr: %s", result.stderr)
	path := strings.TrimSpace(result.stdout)
	assert.Equal(t, filepath.Join(repo, ".worktrees", "feature-withhooks"), path, "hook output must not reach stdout")
	assert.FileExists(t, filepath.Join(path, "hook-ran"))
}

func TestClaudeWorktreeCreateHookOutputStaysOffStdout(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	configPath := filepath.Join(repo, ".treeman.toml")
	commands := []string{"echo noisy-hook-output"}
	require.NoError(t, os.WriteFile(configPath, []byte("[hooks]\npost_create = [\"echo noisy-hook-output\"]\n"), 0o600))
	commonDir := commonDirOf(t, repo)
	scope, err := hooks.NewApprovalScope(commonDir, configPath, hooks.PostCreatePhase, commands)
	require.NoError(t, err)
	store, err := state.NewHookApprovalStore(commonDir)
	require.NoError(t, err)
	require.NoError(t, store.Approve(scope))

	result := claudeCreate(t, "noisy", repo)

	require.NoErrorf(t, result.err, "stderr: %s", result.stderr)
	assert.Equal(t, filepath.Join(repo, ".worktrees", "feature-noisy")+"\n", result.stdout)
	assert.Contains(t, result.stderr, "noisy-hook-output")
}

func TestClaudeWorktreeCreateRejectsBadInput(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	chdirForTest(t, repo)
	for name, input := range map[string]any{
		"missing name":    map[string]string{"cwd": repo},
		"empty name":      map[string]string{"name": "", "cwd": repo},
		"not a repo cwd":  map[string]string{"name": "x", "cwd": canonicalTestDir(t)},
		"name with space": map[string]string{"name": "bad name", "cwd": repo},
		"invalid ref":     map[string]string{"name": "a..b", "cwd": repo},
	} {
		t.Run(name, func(t *testing.T) {
			result := runClaudeHookCommand(t, "worktree-create", input)
			assert.Error(t, result.err)
			assert.Empty(t, result.stdout)
		})
	}
	t.Run("invalid JSON", func(t *testing.T) {
		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		root := New("test", "test", "test")
		root.SetOut(stdout)
		root.SetErr(stderr)
		root.SetIn(strings.NewReader("{not json"))
		root.SetArgs([]string{"claude", "worktree-create"})
		assert.Error(t, root.Execute())
		assert.Empty(t, stdout.String())
	})
	assert.Equal(t, 1, worktreeCount(t, repo))
}

func TestClaudeWorktreeCreateInABareLayoutStaysOutOfTheGitDirectory(t *testing.T) {
	isolateClaudeHookEnv(t)
	parent := canonicalTestDir(t)
	seed := filepath.Join(parent, "seed")
	gitTest(t, parent, "init", "-b", "main", seed)
	require.NoError(t, os.WriteFile(filepath.Join(seed, "f.txt"), []byte("hi\n"), 0o644))
	gitTest(t, seed, "add", "f.txt")
	gitTest(t, seed, "commit", "-m", "init")
	project := filepath.Join(parent, "proj")
	require.NoError(t, os.MkdirAll(project, 0o755))
	gitTest(t, parent, "clone", "--bare", seed, filepath.Join(project, ".bare"))
	require.NoError(t, os.WriteFile(filepath.Join(project, ".git"), []byte("gitdir: ./.bare\n"), 0o644))
	bare := filepath.Join(project, ".bare")

	for _, tc := range []struct{ name, cwd string }{
		{"barefromproj", project},
		{"barefromgitdir", bare},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := mustClaudeCreate(t, tc.name, tc.cwd)
			assert.False(t, strings.HasPrefix(path, bare+string(filepath.Separator)), "worktree %s is inside the Git directory", path)
			assert.Equal(t, "feature/"+tc.name, currentBranch(t, path))
		})
	}
	assert.NoDirExists(t, filepath.Join(bare, ".worktrees"))
}

func TestClaudeWorktreeCreateInAPlainBareRepositoryStaysOutOfTheGitDirectory(t *testing.T) {
	isolateClaudeHookEnv(t)
	parent := canonicalTestDir(t)
	seed := filepath.Join(parent, "seed")
	gitTest(t, parent, "init", "-b", "main", seed)
	gitTest(t, seed, "commit", "--allow-empty", "-m", "init")
	repos := filepath.Join(parent, "repos")
	require.NoError(t, os.MkdirAll(repos, 0o755))
	foo := filepath.Join(repos, "foo.git")
	bar := filepath.Join(repos, "bar.git")
	gitTest(t, parent, "clone", "--bare", seed, foo)
	gitTest(t, parent, "clone", "--bare", seed, bar)

	fooPath := mustClaudeCreate(t, "same", foo)
	barPath := mustClaudeCreate(t, "same", bar)

	assert.False(t, strings.HasPrefix(fooPath, foo+string(filepath.Separator)), "worktree %s is inside the Git directory", fooPath)
	assert.False(t, strings.HasPrefix(barPath, bar+string(filepath.Separator)), "worktree %s is inside the Git directory", barPath)
	assert.NotEqual(t, fooPath, barPath, "sibling bare repositories do not share worktree paths")
	assert.NoDirExists(t, filepath.Join(foo, ".worktrees"))
	assert.Equal(t, fooPath, mustClaudeCreate(t, "same", foo), "the same name is reused in a bare repository")
}

// --- worktree-remove ---------------------------------------------------------

func TestClaudeWorktreeRemoveOfAMissingPathSucceeds(t *testing.T) {
	isolateClaudeHookEnv(t)
	result := claudeRemove(t, filepath.Join(canonicalTestDir(t), "does-not-exist"))
	assert.NoError(t, result.err)
}

func TestClaudeWorktreeRemoveDeletesAnOwnedMergedBranchAndItsRecord(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "merged", repo)

	result := claudeRemove(t, path)

	require.NoErrorf(t, result.err, "stderr: %s", result.stderr)
	assert.NoDirExists(t, path)
	assert.False(t, hasBranch(repo, "feature/merged"), "an unchanged branch TreeMan created is merged and deleted")
	assert.Empty(t, recordsForBranch(t, repo, "feature/merged"))
}

func TestClaudeWorktreeRemoveDeletesAnOwnedBranchMergedIntoLocalDefault(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "localmerge", repo)
	gitTest(t, path, "commit", "--allow-empty", "-m", "work")
	gitTest(t, repo, "merge", "--ff-only", "feature/localmerge")

	result := claudeRemove(t, path)

	require.NoErrorf(t, result.err, "stderr: %s", result.stderr)
	assert.NoDirExists(t, path)
	assert.False(t, hasBranch(repo, "feature/localmerge"))
}

func TestClaudeWorktreeRemoveRemovesAnOwnedDirtyWorktreeAndKeepsAnUnmergedBranch(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "dirty", repo)
	require.NoError(t, os.WriteFile(filepath.Join(path, "tracked.txt"), []byte("x"), 0o644))
	gitTest(t, path, "add", "tracked.txt")
	gitTest(t, path, "commit", "-m", "unmerged work")
	require.NoError(t, os.WriteFile(filepath.Join(path, "untracked.txt"), []byte("y"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(path, "tracked.txt"), []byte("modified"), 0o644))
	tip := revParse(t, repo, "feature/dirty")

	result := claudeRemove(t, path)

	require.NoErrorf(t, result.err, "stderr: %s", result.stderr)
	assert.NoDirExists(t, path)
	require.True(t, hasBranch(repo, "feature/dirty"), "an unmerged branch is kept")
	assert.Equal(t, tip, revParse(t, repo, "feature/dirty"))
	records := recordsForBranch(t, repo, "feature/dirty")
	require.Len(t, records, 1, "the record of a kept owned branch stays")
	assert.Empty(t, records[0].WorktreePath)
	assert.True(t, records[0].BranchCreated)
}

func TestClaudeWorktreeRemoveDeletesAKeptOwnedBranchOnceItIsMerged(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "later", repo)
	gitTest(t, path, "commit", "--allow-empty", "-m", "work")
	require.NoError(t, claudeRemove(t, path).err)
	require.True(t, hasBranch(repo, "feature/later"))

	again := mustClaudeCreate(t, "later", repo)
	assert.Equal(t, "feature/later", currentBranch(t, again))
	records := recordsForBranch(t, repo, "feature/later")
	require.Len(t, records, 1)
	assert.True(t, records[0].BranchCreated, "re-attaching TreeMan's own branch keeps its ownership")

	gitTest(t, repo, "merge", "--ff-only", "feature/later")
	result := claudeRemove(t, again)

	require.NoErrorf(t, result.err, "stderr: %s", result.stderr)
	assert.NoDirExists(t, again)
	assert.False(t, hasBranch(repo, "feature/later"))
	assert.Empty(t, recordsForBranch(t, repo, "feature/later"))
}

func TestClaudeWorktreeRemoveRemovesACleanUnownedWorktreeAndKeepsItsBranch(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	userWorktree := filepath.Join(canonicalTestDir(t), "user-clean")
	gitTest(t, repo, "worktree", "add", "-b", "feature/user-clean", userWorktree)

	result := claudeRemove(t, userWorktree)

	require.NoErrorf(t, result.err, "stderr: %s", result.stderr)
	assert.NoDirExists(t, userWorktree)
	assert.True(t, hasBranch(repo, "feature/user-clean"), "a branch TreeMan did not create is kept even when merged")
}

func TestClaudeWorktreeRemoveRefusesADirtyUnownedWorktree(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	userWorktree := filepath.Join(canonicalTestDir(t), "user-dirty")
	gitTest(t, repo, "worktree", "add", "-b", "feature/user-dirty", userWorktree)
	require.NoError(t, os.WriteFile(filepath.Join(userWorktree, "precious.txt"), []byte("keep"), 0o644))

	result := claudeRemove(t, userWorktree)

	require.Error(t, result.err)
	assert.FileExists(t, filepath.Join(userWorktree, "precious.txt"))
	assert.Contains(t, gitTestOutput(t, repo, "worktree", "list", "--porcelain"), userWorktree)
	assert.True(t, hasBranch(repo, "feature/user-dirty"))
}

func TestClaudeWorktreeRemoveRefusesTheMainWorktree(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	require.NoError(t, agentworktree.Open(commonDirOf(t, repo)).Put(agentworktree.Record{
		Agent: "claude", Name: "main", Branch: "main", BranchCreated: true, CreatedAt: time.Now().UTC(), WorktreePath: repo,
	}))

	result := claudeRemove(t, repo)

	require.Error(t, result.err)
	assert.DirExists(t, repo)
	assert.FileExists(t, filepath.Join(repo, "README.md"))
	assert.True(t, hasBranch(repo, "main"))
}

func TestClaudeWorktreeRemoveRemovesAnOwnedDetachedWorktree(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "detach", repo)
	gitTest(t, path, "checkout", "--detach")

	result := claudeRemove(t, path)

	require.NoErrorf(t, result.err, "stderr: %s", result.stderr)
	assert.NoDirExists(t, path)
}

func TestClaudeWorktreeRemoveNeverDeletesARecreatedBranch(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "recreated", repo)
	gitTest(t, repo, "worktree", "remove", "--force", path)
	gitTest(t, repo, "branch", "-D", "feature/recreated")
	userBranch(t, repo, "feature/recreated")

	again := mustClaudeCreate(t, "recreated", repo)
	assert.Equal(t, path, again)
	result := claudeRemove(t, again)

	require.NoErrorf(t, result.err, "stderr: %s", result.stderr)
	assert.NoDirExists(t, again)
	assert.True(t, hasBranch(repo, "feature/recreated"), "the user's re-created branch is kept although it is merged")
}

func TestClaudeWorktreeRemoveNeverDeletesARecreatedBranchWithAStaleRecord(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "stale", repo)
	// The branch is deleted and re-created outside TreeMan with no create in
	// between, and a worktree is added by hand at the recorded path.
	gitTest(t, repo, "worktree", "remove", "--force", path)
	gitTest(t, repo, "branch", "-D", "feature/stale")
	userBranch(t, repo, "feature/stale")
	gitTest(t, repo, "worktree", "add", path, "feature/stale")

	result := claudeRemove(t, path)

	require.NoErrorf(t, result.err, "stderr: %s", result.stderr)
	assert.True(t, hasBranch(repo, "feature/stale"), "a branch that merely shares a name is never deleted")
}

// --- delete and clean --------------------------------------------------------

func TestDeleteDropsRecordsForTheBranchItDeletes(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "deleted", repo)
	require.Len(t, recordsForBranch(t, repo, "feature/deleted"), 1)
	chdirForTest(t, repo)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	root := New("test", "test", "test")
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"delete", "--path", path, "--branch", "feature/deleted", "--yes"})
	require.NoErrorf(t, root.Execute(), "stderr: %s", stderr.String())
	require.False(t, hasBranch(repo, "feature/deleted"))

	assert.Empty(t, recordsForBranch(t, repo, "feature/deleted"))

	// A later branch of the same name is the user's, and is never deleted.
	userBranch(t, repo, "feature/deleted")
	again := mustClaudeCreate(t, "deleted", repo)
	require.NoError(t, claudeRemove(t, again).err)
	assert.True(t, hasBranch(repo, "feature/deleted"))
}

func TestCleanDropsRecordsForWhatItRemoves(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "cleaned", repo)
	require.NoError(t, os.WriteFile(filepath.Join(path, "feature.txt"), []byte("done\n"), 0o644))
	gitTest(t, path, "add", "feature.txt")
	gitTest(t, path, "commit", "-m", "feature")
	gitTest(t, path, "push", "-u", "origin", "feature/cleaned")
	gitTest(t, repo, "merge", "--ff-only", "feature/cleaned")
	gitTest(t, repo, "push", "origin", "main")
	gitTest(t, repo, "fetch", "origin")
	chdirForTest(t, repo)

	cmd := commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{})
	require.NoError(t, runClean(cmd, false, true))
	require.NoDirExists(t, path, "clean must remove the merged worktree for this test to apply")
	require.False(t, hasBranch(repo, "feature/cleaned"))

	assert.Empty(t, recordsForBranch(t, repo, "feature/cleaned"))
}

func TestClaudeWorktreeCreateRecreatesAWorktreeWhoseDirectoryWasDeleted(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	path := mustClaudeCreate(t, "vanished", repo)
	gitTest(t, path, "commit", "--allow-empty", "-m", "work")
	tip := revParse(t, repo, "feature/vanished")
	require.NoError(t, os.RemoveAll(path))

	again := mustClaudeCreate(t, "vanished", repo)

	assert.Equal(t, path, again)
	assert.Equal(t, "feature/vanished", currentBranch(t, again))
	assert.Equal(t, tip, revParse(t, again, "HEAD"), "the existing branch is re-attached, not re-created")
}

func TestClaudeWorktreeCreateDisablesPromptsButKeepsTheConfiguredSSHCommand(t *testing.T) {
	repo, _ := claudeRepoWithOrigin(t)
	gitTest(t, repo, "config", "core.sshCommand", "ssh -i /keys/project")

	mustClaudeCreate(t, "prompts", repo)

	assert.Equal(t, "0", os.Getenv("GIT_TERMINAL_PROMPT"))
	sshCommand := os.Getenv("GIT_SSH_COMMAND")
	assert.Contains(t, sshCommand, "-i /keys/project")
	assert.Contains(t, sshCommand, "BatchMode=yes")
}

// TestClaudeWorktreeCreateBinaryPrintsOnlyThePath runs the real binary the way
// Claude Code does: JSON on stdin, then a closed stdin, and stdout read as
// the path.
func TestClaudeWorktreeCreateBinaryPrintsOnlyThePath(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the treeman binary")
	}
	wd, err := os.Getwd()
	require.NoError(t, err)
	moduleRoot := filepath.Clean(filepath.Join(wd, "..", ".."))
	binary := filepath.Join(t.TempDir(), "treeman")
	build := exec.Command("go", "build", "-o", binary, "./cmd/treeman")
	build.Dir = moduleRoot
	out, err := build.CombinedOutput()
	require.NoErrorf(t, err, "go build: %s", out)

	repo, _ := claudeRepoWithOrigin(t)
	configPath := filepath.Join(repo, ".treeman.toml")
	hook := "echo hook-stdout; echo hook-devstdout > /dev/stdout"
	require.NoError(t, os.WriteFile(configPath, []byte("[hooks]\npost_create = [\""+hook+"\"]\n"), 0o600))
	commonDir := commonDirOf(t, repo)
	scope, err := hooks.NewApprovalScope(commonDir, configPath, hooks.PostCreatePhase, []string{hook})
	require.NoError(t, err)
	store, err := state.NewHookApprovalStore(commonDir)
	require.NoError(t, err)
	require.NoError(t, store.Approve(scope))

	run := exec.Command(binary, "claude", "worktree-create")
	run.Dir = canonicalTestDir(t)
	run.Stdin = strings.NewReader(`{"name":"binary","cwd":"` + repo + `","session_id":"s"}`)
	var stdout, stderr bytes.Buffer
	run.Stdout = &stdout
	run.Stderr = &stderr
	require.NoErrorf(t, run.Run(), "stderr: %s", stderr.String())

	want := filepath.Join(repo, ".worktrees", "feature-binary")
	assert.Equal(t, want+"\n", stdout.String())
	assert.Contains(t, stderr.String(), "hook-stdout")
	assert.Contains(t, stderr.String(), "hook-devstdout")

	// A failure exits non-zero with a message and no path.
	fail := exec.Command(binary, "claude", "worktree-create")
	fail.Stdin = strings.NewReader(`{"name":"x","cwd":"` + canonicalTestDir(t) + `"}`)
	stdout.Reset()
	stderr.Reset()
	fail.Stdout = &stdout
	fail.Stderr = &stderr
	assert.Error(t, fail.Run())
	assert.Empty(t, stdout.String())
	assert.NotEmpty(t, stderr.String())

	// worktree-remove of a missing path exits 0.
	missing := exec.Command(binary, "claude", "worktree-remove")
	missing.Stdin = strings.NewReader(`{"worktree_path":"` + filepath.Join(canonicalTestDir(t), "gone") + `"}`)
	assert.NoError(t, missing.Run())
}
