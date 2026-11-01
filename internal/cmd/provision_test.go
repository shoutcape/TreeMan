package cmd

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fixtures ----------------------------------------------------------------

// provisionRepo is a clone of a local bare origin whose origin/HEAD names main,
// with the process working directory inside the clone.
func provisionRepo(t *testing.T) (repo, origin string) {
	t.Helper()
	repo, origin = claudeRepoWithOrigin(t)
	chdirForTest(t, repo)
	return repo, origin
}

// noSetup provisions without any setup step doing work.
func noSetup() creationSetupOptions {
	return creationSetupOptions{skipEnv: true, skipDeps: true, skipDatabase: true, skipHooks: true}
}

func provisionCommand() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	return commandWithOutput(stdout, stderr), stdout, stderr
}

// pushFromOtherClone pushes a new commit on branch to origin from a separate
// clone, so the provisioning repository has not fetched it. It returns the SHA.
func pushFromOtherClone(t *testing.T, origin, branch, refspec string) string {
	t.Helper()
	other := filepath.Join(t.TempDir(), "other")
	gitTest(t, filepath.Dir(other), "clone", origin, other)
	if branch != "main" {
		gitTest(t, other, "checkout", "-b", branch)
	}
	require.NoError(t, os.WriteFile(filepath.Join(other, strings.ReplaceAll(branch, "/", "-")+".txt"), []byte(branch+"\n"), 0o644))
	gitTest(t, other, "add", ".")
	gitTest(t, other, "commit", "-m", "commit on "+branch)
	if refspec == "" {
		refspec = "HEAD:refs/heads/" + branch
	}
	gitTest(t, other, "push", "origin", refspec)
	return revParse(t, other, "HEAD")
}

func breakOrigin(t *testing.T, repo string) {
	t.Helper()
	gitTest(t, repo, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
}

func refExists(dir, ref string) bool {
	return exec.Command("git", "-C", dir, "show-ref", "--verify", "--quiet", ref).Run() == nil
}

func upstreamOf(t *testing.T, dir, branch string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", branch+"@{upstream}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func plain(buf *bytes.Buffer) string { return ui.StripANSI(buf.String()) }

// assertNoFetchLine checks that no "Fetching" progress line was printed.
// Temporary paths contain the test name, so only line starts are matched.
func assertNoFetchLine(t *testing.T, stderr *bytes.Buffer, msgAndArgs ...any) {
	t.Helper()
	for _, line := range strings.Split(plain(stderr), "\n") {
		assert.False(t, strings.HasPrefix(strings.TrimLeft(line, "→ "), "Fetching"), msgAndArgs...)
	}
}

// --- new branch from default -------------------------------------------------

func TestProvisionNewBranchFromDefaultStartsFromLatestOrigin(t *testing.T) {
	repo, origin := provisionRepo(t)
	latest := pushFromOtherClone(t, origin, "main", "")
	cmd, _, _ := provisionCommand()

	got, err := provisionWorktree(cmd, provisionRequest{
		mainRoot: repo,
		source:   newBranchFromDefault("feature/fresh", fetchStrict),
		options:  noSetup(),
	})
	require.NoError(t, err)

	want := filepath.Join(repo, ".worktrees", "feature-fresh")
	assert.Equal(t, want, got.worktree.Path)
	assert.Equal(t, "feature/fresh", got.worktree.Branch)
	assert.False(t, got.setupSkipped)
	assert.Equal(t, latest, revParse(t, want, "HEAD"), "a new branch starts from the freshly fetched default branch")
	assert.Equal(t, "feature/fresh", currentBranch(t, want))
}

func TestProvisionStrictFetchFailureCreatesNothing(t *testing.T) {
	repo, _ := provisionRepo(t)
	breakOrigin(t, repo)
	cmd, _, _ := provisionCommand()

	_, err := provisionWorktree(cmd, provisionRequest{
		mainRoot: repo,
		source:   newBranchFromDefault("feature/strict", fetchStrict),
		options:  noSetup(),
	})

	require.Error(t, err)
	assert.False(t, hasBranch(repo, "feature/strict"))
	assert.NoDirExists(t, filepath.Join(repo, ".worktrees", "feature-strict"))
	assert.Equal(t, 1, worktreeCount(t, repo))
}

func TestProvisionFallbackFetchFailureStartsFromCachedOrigin(t *testing.T) {
	repo, _ := provisionRepo(t)
	cached := revParse(t, repo, "origin/main")
	// Move local HEAD away from origin/main so the two start points differ.
	require.NoError(t, os.WriteFile(filepath.Join(repo, "local.txt"), []byte("local\n"), 0o644))
	gitTest(t, repo, "add", "local.txt")
	gitTest(t, repo, "commit", "-m", "local only")
	require.NotEqual(t, cached, revParse(t, repo, "HEAD"))
	breakOrigin(t, repo)
	cmd, _, stderr := provisionCommand()

	got, err := provisionWorktree(cmd, provisionRequest{
		mainRoot: repo,
		source:   newBranchFromDefault("feature/cached", fetchFallBackToCached),
		options:  noSetup(),
	})
	require.NoError(t, err)

	assert.Equal(t, cached, revParse(t, got.worktree.Path, "HEAD"))
	assert.Contains(t, plain(stderr), "!", "a failed fetch is reported as a warning")
	assert.Contains(t, plain(stderr), "cached")
}

func TestProvisionFallbackWithoutOriginStartsFromHEAD(t *testing.T) {
	repo := claudeRepoWithoutOrigin(t)
	chdirForTest(t, repo)
	head := revParse(t, repo, "HEAD")
	cmd, _, stderr := provisionCommand()

	got, err := provisionWorktree(cmd, provisionRequest{
		mainRoot: repo,
		source:   newBranchFromDefault("feature/from-head", fetchFallBackToCached),
		options:  noSetup(),
	})
	require.NoError(t, err)

	assert.Equal(t, head, revParse(t, got.worktree.Path, "HEAD"))
	assert.Contains(t, plain(stderr), "HEAD", "starting from HEAD is warned about")
}

func TestProvisionFallbackWithNoCommitsFails(t *testing.T) {
	isolateClaudeHookEnv(t)
	repo := filepath.Join(canonicalTestDir(t), "empty")
	gitTest(t, filepath.Dir(repo), "init", "-b", "main", repo)
	chdirForTest(t, repo)
	cmd, _, _ := provisionCommand()

	_, err := provisionWorktree(cmd, provisionRequest{
		mainRoot: repo,
		source:   newBranchFromDefault("feature/nothing", fetchFallBackToCached),
		options:  noSetup(),
	})

	require.Error(t, err)
	assert.False(t, hasBranch(repo, "feature/nothing"))
	assert.NoDirExists(t, filepath.Join(repo, ".worktrees", "feature-nothing"))
}

// --- guard: existing local branch --------------------------------------------

func TestProvisionRefusesExistingLocalBranchBeforeConsentAndFetch(t *testing.T) {
	sources := map[string]func(branch string) worktreeSource{
		"newBranchFromDefault/strict":   func(b string) worktreeSource { return newBranchFromDefault(b, fetchStrict) },
		"newBranchFromDefault/fallback": func(b string) worktreeSource { return newBranchFromDefault(b, fetchFallBackToCached) },
		"trackRemoteBranch":             trackRemoteBranch,
		"pullRequestHead":               func(b string) worktreeSource { return pullRequestHead(b, 7, "refs/pull/7/head") },
	}
	for name, source := range sources {
		t.Run(name, func(t *testing.T) {
			repo, origin := provisionRepo(t)
			const branch = "feature/taken"
			pushFromOtherClone(t, origin, branch, "")
			pushFromOtherClone(t, origin, branch, "HEAD:refs/pull/7/head")
			gitTest(t, repo, "branch", branch)
			before := revParse(t, repo, branch)
			// An unapproved hook would make a non-interactive command fail
			// with an approval error if consent were asked first.
			configureHookApprovalRepo(t, repo, "touch hook-ran")
			options := noSetup()
			options.skipHooks = false
			cmd, _, stderr := provisionCommand()

			_, err := provisionWorktree(cmd, provisionRequest{mainRoot: repo, source: source(branch), options: options})

			require.Error(t, err)
			assert.Contains(t, err.Error(), branch)
			assert.Contains(t, err.Error(), "already exists")
			assert.NotContains(t, err.Error(), "approval", "the guard runs before hook consent")
			assertNoFetchLine(t, stderr, "the guard runs before any fetch")
			assert.False(t, refExists(repo, "refs/remotes/origin/"+branch), "origin's branch must not be fetched")
			assert.False(t, refExists(repo, "refs/pull/7/head"))
			assert.Equal(t, before, revParse(t, repo, branch), "the existing branch is untouched")
			assert.Equal(t, 1, worktreeCount(t, repo))
		})
	}
}

func TestProvisionRefusalNamesTheExistingWorktree(t *testing.T) {
	sources := map[string]func(branch string) worktreeSource{
		"newBranchFromDefault/strict":   func(b string) worktreeSource { return newBranchFromDefault(b, fetchStrict) },
		"newBranchFromDefault/fallback": func(b string) worktreeSource { return newBranchFromDefault(b, fetchFallBackToCached) },
		"trackRemoteBranch":             trackRemoteBranch,
		"pullRequestHead":               func(b string) worktreeSource { return pullRequestHead(b, 7, "refs/pull/7/head") },
	}
	for name, source := range sources {
		t.Run(name, func(t *testing.T) {
			repo, origin := provisionRepo(t)
			const branch = "feature/busy"
			pushFromOtherClone(t, origin, branch, "")
			existing := filepath.Join(canonicalTestDir(t), "busy")
			gitTest(t, repo, "worktree", "add", "-b", branch, existing)
			cmd, _, stderr := provisionCommand()

			_, err := provisionWorktree(cmd, provisionRequest{mainRoot: repo, source: source(branch), options: noSetup()})

			require.Error(t, err)
			assert.Contains(t, err.Error(), existing, "the refusal names the branch's worktree")
			assertNoFetchLine(t, stderr)
			assert.Equal(t, 2, worktreeCount(t, repo))
		})
	}
}

func TestRunBranchRefusesExistingLocalBranchBeforeFetching(t *testing.T) {
	repo, origin := provisionRepo(t)
	const branch = "feature/local-already"
	pushFromOtherClone(t, origin, branch, "")
	gitTest(t, repo, "branch", branch)
	pathWithOnlyGit(t)
	cmd, stdout, stderr := provisionCommand()

	err := runBranch(cmd, branch, noSetup())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
	assertNoFetchLine(t, stderr)
	assert.False(t, refExists(repo, "refs/remotes/origin/"+branch), "branch must not fetch before refusing")
	assert.Empty(t, stdout.String())
	assert.Equal(t, 1, worktreeCount(t, repo))
}

// --- consent and destination before fetch -----------------------------------

func TestProvisionUnusableDestinationFetchesNothing(t *testing.T) {
	repo, origin := provisionRepo(t)
	const branch = "feature/remote-only"
	pushFromOtherClone(t, origin, branch, "")
	notADir := filepath.Join(canonicalTestDir(t), "file")
	require.NoError(t, os.WriteFile(notADir, []byte("x"), 0o644))
	cmd, _, stderr := provisionCommand()

	_, err := provisionWorktree(cmd, provisionRequest{
		mainRoot:  repo,
		parentDir: notADir,
		source:    trackRemoteBranch(branch),
		options:   noSetup(),
	})

	require.Error(t, err)
	assertNoFetchLine(t, stderr)
	assert.False(t, refExists(repo, "refs/remotes/origin/"+branch), "an unusable destination costs no fetch")
	assert.False(t, hasBranch(repo, branch))
}

func TestProvisionRefusedConsentFetchesNothing(t *testing.T) {
	repo, origin := provisionRepo(t)
	const branch = "feature/refused"
	pushFromOtherClone(t, origin, branch, "")
	configureHookApprovalRepo(t, repo, "touch hook-ran")
	options := noSetup()
	options.skipHooks = false

	_, err := provisionWorktree(interactiveApprovalCommand("n\n"), provisionRequest{
		mainRoot: repo, source: trackRemoteBranch(branch), options: options,
	})

	require.ErrorContains(t, err, "hook approval refused")
	assert.False(t, refExists(repo, "refs/remotes/origin/"+branch))
	assert.False(t, hasBranch(repo, branch))
}

// --- other sources -----------------------------------------------------------

func TestProvisionTrackRemoteBranchSetsUpstream(t *testing.T) {
	repo, origin := provisionRepo(t)
	const branch = "feature/remote"
	sha := pushFromOtherClone(t, origin, branch, "")
	cmd, _, _ := provisionCommand()

	got, err := provisionWorktree(cmd, provisionRequest{mainRoot: repo, source: trackRemoteBranch(branch), options: noSetup()})
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(repo, ".worktrees", "feature-remote"), got.worktree.Path)
	assert.Equal(t, sha, revParse(t, got.worktree.Path, "HEAD"))
	assert.Equal(t, branch, currentBranch(t, got.worktree.Path))
	assert.Equal(t, "origin/"+branch, upstreamOf(t, repo, branch))
}

func TestProvisionPullRequestHeadTracksOriginBranch(t *testing.T) {
	repo, origin := provisionRepo(t)
	const branch = "feature/pr-branch"
	pushFromOtherClone(t, origin, branch, "")
	// The PR head differs from origin's branch tip, so the start point is
	// observably the PR ref.
	prHead := pushFromOtherClone(t, origin, "pr-head", "HEAD:refs/pull/7/head")
	cmd, _, _ := provisionCommand()

	got, err := provisionWorktree(cmd, provisionRequest{
		mainRoot: repo, source: pullRequestHead(branch, 7, "refs/pull/7/head"), options: noSetup(),
	})
	require.NoError(t, err)

	assert.Equal(t, prHead, revParse(t, got.worktree.Path, "HEAD"), "the branch is created at the PR head")
	assert.Equal(t, branch, currentBranch(t, got.worktree.Path))
	assert.True(t, refExists(repo, "refs/remotes/origin/"+branch), "origin's branch is fetched by name")
	assert.Equal(t, "origin/"+branch, upstreamOf(t, repo, branch))
}

func TestProvisionPullRequestHeadFromForkWarnsWithoutUpstream(t *testing.T) {
	repo, origin := provisionRepo(t)
	const branch = "fork-branch"
	prHead := pushFromOtherClone(t, origin, "pr-head", "HEAD:refs/pull/9/head")
	cmd, _, stderr := provisionCommand()

	got, err := provisionWorktree(cmd, provisionRequest{
		mainRoot: repo, source: pullRequestHead(branch, 9, "refs/pull/9/head"), options: noSetup(),
	})
	require.NoError(t, err, "a fork PR whose branch is not on origin still gets a worktree")

	assert.Equal(t, prHead, revParse(t, got.worktree.Path, "HEAD"))
	assertNoUpstream(t, repo, branch)
	assert.Contains(t, plain(stderr), "!")
	assert.Contains(t, plain(stderr), branch)
}

func TestProvisionPullRequestHeadFetchFailureCreatesNothing(t *testing.T) {
	repo, _ := provisionRepo(t)
	cmd, _, _ := provisionCommand()

	_, err := provisionWorktree(cmd, provisionRequest{
		mainRoot: repo, source: pullRequestHead("feature/no-pr", 404, "refs/pull/404/head"), options: noSetup(),
	})

	require.Error(t, err)
	assert.False(t, hasBranch(repo, "feature/no-pr"))
	assert.Equal(t, 1, worktreeCount(t, repo))
}

func TestProvisionExistingBranchAttachesWithoutFetching(t *testing.T) {
	repo, _ := provisionRepo(t)
	const branch = "feature/local"
	gitTest(t, repo, "branch", branch)
	sha := revParse(t, repo, branch)
	breakOrigin(t, repo) // any fetch would fail
	cmd, _, stderr := provisionCommand()

	got, err := provisionWorktree(cmd, provisionRequest{mainRoot: repo, source: existingBranch(branch), options: noSetup()})
	require.NoError(t, err)

	assert.Equal(t, sha, revParse(t, got.worktree.Path, "HEAD"))
	assert.Equal(t, branch, currentBranch(t, got.worktree.Path))
	assertNoFetchLine(t, stderr)
}

// --- afterAdd ----------------------------------------------------------------

func TestProvisionAfterAddRunsAfterAddAndBeforeSetup(t *testing.T) {
	repo, _ := provisionRepo(t)
	configureHookApprovalRepo(t, repo, "touch hook-ran")
	options := noSetup()
	options.skipHooks = false
	options.trustHooks = true
	cmd, _, _ := provisionCommand()

	var seen git.CreatedWorktree
	var dirExisted, hookHadRun bool
	got, err := provisionWorktree(cmd, provisionRequest{
		mainRoot: repo,
		source:   newBranchFromDefault("feature/after-add", fetchStrict),
		options:  options,
		afterAdd: func(created git.CreatedWorktree) error {
			seen = created
			_, statErr := os.Stat(created.Path)
			dirExisted = statErr == nil
			_, hookErr := os.Stat(filepath.Join(created.Path, "hook-ran"))
			hookHadRun = hookErr == nil
			return nil
		},
	})
	require.NoError(t, err)

	assert.Equal(t, got.worktree, seen, "afterAdd receives the created worktree")
	assert.True(t, dirExisted, "afterAdd runs once the worktree exists")
	assert.False(t, hookHadRun, "afterAdd runs before setup")
	assert.FileExists(t, filepath.Join(got.worktree.Path, "hook-ran"), "setup still runs")
}

func TestProvisionAfterAddErrorIsAWarning(t *testing.T) {
	repo, _ := provisionRepo(t)
	configureHookApprovalRepo(t, repo, "touch hook-ran")
	options := noSetup()
	options.skipHooks = false
	options.trustHooks = true
	cmd, _, stderr := provisionCommand()

	got, err := provisionWorktree(cmd, provisionRequest{
		mainRoot: repo,
		source:   newBranchFromDefault("feature/after-add-fails", fetchStrict),
		options:  options,
		afterAdd: func(git.CreatedWorktree) error { return errors.New("could not record ownership") },
	})
	require.NoError(t, err)

	assert.DirExists(t, got.worktree.Path)
	assert.Contains(t, plain(stderr), "could not record ownership")
	assert.False(t, got.setupSkipped)
	assert.FileExists(t, filepath.Join(got.worktree.Path, "hook-ran"), "provisioning continues to setup")
}

// --- setup lock --------------------------------------------------------------

func TestProvisionSkipsSetupWhenTheLockIsHeld(t *testing.T) {
	repo, _ := provisionRepo(t)
	configureHookApprovalRepo(t, repo, "touch hook-ran")
	options := noSetup()
	options.skipHooks = false
	options.trustHooks = true
	const branch = "feature/held"
	// Git names a linked worktree's administration after its directory, so
	// the lock for the worktree about to be created can be held up front.
	path := filepath.Join(repo, ".worktrees", "feature-held")
	held := setupTarget{
		WorktreeEntry: git.WorktreeEntry{Path: path, Branch: branch},
		commonDir:     commonDirOf(t, repo),
		worktreeID:    "feature-held",
	}
	cmd, _, stderr := provisionCommand()

	var got provisionedWorktree
	var provisionErr error
	require.NoError(t, withSetupLock(held, func() error {
		got, provisionErr = provisionWorktree(cmd, provisionRequest{
			mainRoot: repo, source: newBranchFromDefault(branch, fetchStrict), options: options,
		})
		return nil
	}))

	require.NoError(t, provisionErr, "a held lock does not fail creation")
	require.Equal(t, path, got.worktree.Path)
	created, err := resolveSetupTarget(path)
	require.NoError(t, err)
	require.Equal(t, held.worktreeID, created.worktreeID, "the test must hold the created worktree's lock")
	assert.True(t, got.setupSkipped)
	assert.DirExists(t, path, "the worktree is still delivered")
	assert.NoFileExists(t, filepath.Join(path, "hook-ran"), "setup did not run")
	assert.Contains(t, plain(stderr), "!")
	assert.Contains(t, plain(stderr), "already running")
}

func TestProvisionReleasesTheSetupLock(t *testing.T) {
	repo, _ := provisionRepo(t)
	cmd, _, _ := provisionCommand()

	got, err := provisionWorktree(cmd, provisionRequest{
		mainRoot: repo, source: newBranchFromDefault("feature/released", fetchStrict), options: noSetup(),
	})
	require.NoError(t, err)
	require.False(t, got.setupSkipped)

	target, err := resolveSetupTarget(got.worktree.Path)
	require.NoError(t, err)
	assert.NoError(t, withSetupLock(target, func() error { return nil }), "setup releases the lock when it finishes")
	// And `treeman setup` can run afterwards.
	assert.NoError(t, reprovisionWorktree(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), target, quietSetup()))
}

func TestReprovisionRefusesWhileTheLockIsHeld(t *testing.T) {
	repo, _ := provisionRepo(t)
	cmd, _, _ := provisionCommand()
	got, err := provisionWorktree(cmd, provisionRequest{
		mainRoot: repo, source: newBranchFromDefault("feature/rerun", fetchStrict), options: noSetup(),
	})
	require.NoError(t, err)
	target, err := resolveSetupTarget(got.worktree.Path)
	require.NoError(t, err)

	var inner error
	require.NoError(t, withSetupLock(target, func() error {
		inner = reprovisionWorktree(commandWithOutput(&bytes.Buffer{}, &bytes.Buffer{}), target, quietSetup())
		return nil
	}))

	assert.True(t, errors.Is(inner, errSetupRunning), "got %v", inner)
}

// --- warm-start offer --------------------------------------------------------

func TestProvisionOffersWarmStartsOnlyWhenPromptsCanBeAnswered(t *testing.T) {
	for _, agent := range []bool{false, true} {
		name := "interactive"
		if agent {
			name = "savedApprovalOnly"
		}
		t.Run(name, func(t *testing.T) {
			repo, _ := provisionRepo(t)
			require.NoError(t, os.WriteFile(filepath.Join(repo, "package.json"), []byte("{}\n"), 0o644))
			require.NoError(t, os.Mkdir(filepath.Join(repo, "node_modules"), 0o755))
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			pathWithOnlyGit(t)
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			cmd := warmStartCommand("n\n", stdout, stderr, false)
			options := creationSetupOptions{skipEnv: true, skipDatabase: true, skipHooks: true, savedApprovalOnly: agent}

			_, err := provisionWorktree(cmd, provisionRequest{
				mainRoot: repo, source: newBranchFromDefault("feature/warm", fetchFallBackToCached), options: options,
			})
			require.NoError(t, err)

			if agent {
				assert.NotContains(t, stderr.String(), "WARM STARTS")
			} else {
				assert.Contains(t, stderr.String(), "WARM STARTS")
				assert.Less(t, strings.Index(stderr.String(), "SETUP"), strings.Index(stderr.String(), "WARM STARTS"),
					"the offer comes after setup")
			}
		})
	}
}

// --- add-only stage ----------------------------------------------------------

func TestAddWorktreeDoesNotRunSetup(t *testing.T) {
	repo, _ := provisionRepo(t)
	configureHookApprovalRepo(t, repo, "touch hook-ran")
	options := noSetup()
	options.skipHooks = false
	options.trustHooks = true
	cmd, _, stderr := provisionCommand()

	added, err := addWorktree(cmd, provisionRequest{
		mainRoot: repo, source: newBranchFromDefault("feature/add-only", fetchStrict), options: options,
	})
	require.NoError(t, err)

	assert.DirExists(t, added.worktree.Path)
	assert.Equal(t, "feature/add-only", currentBranch(t, added.worktree.Path))
	assert.NoFileExists(t, filepath.Join(added.worktree.Path, "hook-ran"))
	assert.NotContains(t, plain(stderr), "SETUP")
}
