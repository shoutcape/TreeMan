package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteListHuman(t *testing.T) {
	buf := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(buf)

	writeListHuman(cmd, []listEntry{
		{Path: "/repo", Branch: "main", Main: true, Current: true},
		{Path: "/repo/.worktrees/feature", Branch: "feature", Dirty: true, Merged: true},
		{Path: "/repo/.worktrees/review", Detached: true},
	})

	assert.NotContains(t, buf.String(), "\x1b")
	assert.Equal(t, "\nWORKTREES\n\n    MARKERS  STATUS    MERGED  BRANCH                       PATH                     \n    ───────  ──────    ──────  ───────────────────────────  ─────────────────────────\n    M▶       CLEAN             main                         /repo\n             DIRTY     YES     feature                      /repo/.worktrees/feature\n             DETACHED          (detached)                   /repo/.worktrees/review\n", ui.StripANSI(buf.String()))
}

func TestWriteListJSON(t *testing.T) {
	buf := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(buf)
	entries := []listEntry{{Path: "/repo", Branch: "main", Main: true, Current: true}}

	require.NoError(t, writeListJSON(cmd, entries))
	assert.NotContains(t, buf.String(), "\x1b")
	var decoded []listEntry
	require.NoError(t, json.Unmarshal(buf.Bytes(), &decoded))
	assert.Equal(t, entries, decoded)
}

func TestListStatusUsesSemanticTones(t *testing.T) {
	tests := []struct {
		entry      listEntry
		wantStatus string
		wantTone   ui.Tone
	}{
		{entry: listEntry{}, wantStatus: "CLEAN", wantTone: ui.ToneSuccess},
		{entry: listEntry{Dirty: true}, wantStatus: "DIRTY", wantTone: ui.ToneWarning},
		{entry: listEntry{Detached: true}, wantStatus: "DETACHED", wantTone: ui.ToneMuted},
		{entry: listEntry{Detached: true, Dirty: true}, wantStatus: "DETACHED", wantTone: ui.ToneWarning},
	}

	for _, test := range tests {
		status, tone := listStatus(test.entry)
		assert.Equal(t, test.wantStatus, status)
		assert.Equal(t, test.wantTone, tone)
	}
}

func TestListCmd_HasWTLAlias(t *testing.T) {
	cmd := newListCmd()

	assert.Contains(t, cmd.Aliases, "wtl")
}

func TestRunListRefreshesDefaultBranchBeforeCheckingMergedState(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	runGit(t, "init", "--bare", "--initial-branch=main", origin)

	repo := filepath.Join(t.TempDir(), "repo")
	runGit(t, "clone", origin, repo)
	runGitInDir(t, repo, "config", "user.email", "test@example.com")
	runGitInDir(t, repo, "config", "user.name", "Test User")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "file"), []byte("base\n"), 0o644))
	runGitInDir(t, repo, "add", "file")
	runGitInDir(t, repo, "commit", "-m", "base")
	runGitInDir(t, repo, "push", "origin", "main")

	runGitInDir(t, repo, "checkout", "-b", "feature")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "file"), []byte("feature\n"), 0o644))
	runGitInDir(t, repo, "commit", "-am", "feature")
	runGitInDir(t, repo, "push", "-u", "origin", "feature")
	runGitInDir(t, repo, "checkout", "main")

	worktree := filepath.Join(t.TempDir(), "feature-worktree")
	runGitInDir(t, repo, "worktree", "add", worktree, "feature")

	// Advance origin/main without updating this clone's origin/main ref.
	updater := filepath.Join(t.TempDir(), "updater")
	runGit(t, "clone", origin, updater)
	runGitInDir(t, updater, "config", "user.email", "test@example.com")
	runGitInDir(t, updater, "config", "user.name", "Test User")
	runGitInDir(t, updater, "merge", "--ff-only", "origin/feature")
	runGitInDir(t, updater, "push", "origin", "main")
	runGitInDir(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

	previousDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repo))
	t.Cleanup(func() { require.NoError(t, os.Chdir(previousDir)) })
	commands := traceGitCommands(t, false)

	output := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(output)
	require.NoError(t, runList(cmd, true))

	var entries []listEntry
	require.NoError(t, json.Unmarshal(output.Bytes(), &entries))
	require.Len(t, entries, 2)
	assert.True(t, entries[1].Merged)
	assert.Equal(t, 1, countGitCommands(commands(), "ls-remote --heads origin refs/heads/main refs/heads/feature"))
	assert.Equal(t, 1, countGitCommands(commands(), "fetch origin refs/heads/main:refs/remotes/origin/main"))
}

func TestRunListSkipsFetchWhenDefaultBranchIsCurrent(t *testing.T) {
	repo, _ := createMergedCleanWorktree(t)
	runGitInDir(t, repo, "fetch", "origin", "refs/heads/main:refs/remotes/origin/main")
	changeToDir(t, repo)
	commands := traceGitCommands(t, false)

	output := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(output)
	require.NoError(t, runList(cmd, true))

	var entries []listEntry
	require.NoError(t, json.Unmarshal(output.Bytes(), &entries))
	require.Len(t, entries, 2)
	assert.True(t, entries[1].Merged)
	assert.Equal(t, 1, countGitCommands(commands(), "ls-remote --heads origin refs/heads/main refs/heads/feature"))
	assert.Zero(t, countGitCommands(commands(), "fetch origin refs/heads/main:refs/remotes/origin/main"))
}

func TestRunListContinuesWithoutMergeMarkersWhenRemoteStateFails(t *testing.T) {
	repo, _ := createMergedCleanWorktree(t)
	changeToDir(t, repo)
	traceGitCommands(t, true)

	output := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(output)
	require.NoError(t, runList(cmd, true))

	var entries []listEntry
	require.NoError(t, json.Unmarshal(output.Bytes(), &entries))
	require.Len(t, entries, 2)
	assert.False(t, entries[1].Merged)
}

func TestRunListUsesCompleteGitHubSnapshotWithoutGitOrRESTFallback(t *testing.T) {
	repo, _ := createMergedCleanWorktree(t)
	mainSHA := gitRevParse(t, repo, "refs/remotes/origin/main")
	featureSHA := gitRevParse(t, repo, "refs/heads/feature")
	commit := fmt.Sprintf(`{"associatedPullRequests":{"nodes":[],"pageInfo":{"hasNextPage":false}}}`)
	githubCommands := traceGitHubAPI(t, githubSnapshotResponse(mainSHA, featureSHA, commit, true), `[]`)
	t.Setenv("_TREEMAN_REMOTE_URL", "https://github.com/owner/repo.git")
	changeToDir(t, repo)
	commands := traceGitCommands(t, false)

	output := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(output)
	require.NoError(t, runList(cmd, true))

	var entries []listEntry
	require.NoError(t, json.Unmarshal(output.Bytes(), &entries))
	require.Len(t, entries, 2)
	assert.True(t, entries[1].Merged, "literal merge must use the refreshed local tracking ref")
	assert.Equal(t, 1, countGitHubCommands(githubCommands(), "api graphql"))
	assert.Zero(t, countGitHubCommands(githubCommands(), "api repos/"))
	assert.Zero(t, countGitCommands(commands(), "ls-remote"))
	assert.Zero(t, countGitCommands(commands(), "fetch origin refs/heads/main:refs/remotes/origin/main"))
}

func TestRunListFetchesChangedDefaultBranchFromGitHubSnapshot(t *testing.T) {
	repo, _ := createMergedCleanWorktree(t)
	originURL, err := exec.Command("git", "-C", repo, "remote", "get-url", "origin").Output()
	require.NoError(t, err)
	updater := filepath.Join(t.TempDir(), "updater")
	runGit(t, "clone", strings.TrimSpace(string(originURL)), updater)
	runGitInDir(t, updater, "config", "user.email", "test@example.com")
	runGitInDir(t, updater, "config", "user.name", "Test User")
	require.NoError(t, os.WriteFile(filepath.Join(updater, "next"), []byte("next\n"), 0o644))
	runGitInDir(t, updater, "add", "next")
	runGitInDir(t, updater, "commit", "-m", "advance main")
	runGitInDir(t, updater, "push", "origin", "main")

	mainSHA := originBranchSHA(t, repo, "main")
	featureSHA := gitRevParse(t, repo, "refs/heads/feature")
	githubCommands := traceGitHubAPI(t, githubSnapshotResponse(mainSHA, featureSHA, `{"associatedPullRequests":{"nodes":[],"pageInfo":{"hasNextPage":false}}}`, true), `[]`)
	t.Setenv("_TREEMAN_REMOTE_URL", "https://github.com/owner/repo.git")
	changeToDir(t, repo)
	commands := traceGitCommands(t, false)

	output := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(output)
	require.NoError(t, runList(cmd, true))

	var entries []listEntry
	require.NoError(t, json.Unmarshal(output.Bytes(), &entries))
	require.Len(t, entries, 2)
	assert.True(t, entries[1].Merged)
	assert.Equal(t, 1, countGitHubCommands(githubCommands(), "api graphql"))
	assert.Equal(t, 1, countGitCommands(commands(), "fetch origin refs/heads/main:refs/remotes/origin/main"))
	assert.Zero(t, countGitCommands(commands(), "ls-remote"))
}

func TestRunListUsesGitHubSnapshotForDeletedSquashMerge(t *testing.T) {
	repo, _ := createSquashMergedCleanWorktree(t)
	runGitInDir(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	mainSHA := originBranchSHA(t, repo, "main")
	featureSHA := gitRevParse(t, repo, "refs/heads/feature")
	commit := fmt.Sprintf(`{"associatedPullRequests":{"nodes":[{"merged":true,"baseRefName":"main","headRefName":%q,"headRefOid":%q}],"pageInfo":{"hasNextPage":false}}}`, "feature", featureSHA)
	githubCommands := traceGitHubAPI(t, githubSnapshotResponse(mainSHA, featureSHA, commit, false), `[]`)
	t.Setenv("_TREEMAN_REMOTE_URL", "https://github.com/owner/repo.git")
	changeToDir(t, repo)
	commands := traceGitCommands(t, false)

	output := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(output)
	require.NoError(t, runList(cmd, true))

	var entries []listEntry
	require.NoError(t, json.Unmarshal(output.Bytes(), &entries))
	require.Len(t, entries, 2)
	assert.True(t, entries[1].Merged)
	assert.Equal(t, 1, countGitHubCommands(githubCommands(), "api graphql"))
	assert.Zero(t, countGitHubCommands(githubCommands(), "api repos/"))
	assert.Zero(t, countGitCommands(commands(), "ls-remote"))
}

func TestRunListFallsBackToRESTForIncompleteGitHubSnapshot(t *testing.T) {
	repo, _ := createSquashMergedCleanWorktree(t)
	runGitInDir(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	mainSHA := originBranchSHA(t, repo, "main")
	featureSHA := gitRevParse(t, repo, "refs/heads/feature")
	rest := fmt.Sprintf(`[{"merged_at":"2026-08-01T10:00:00Z","base":{"ref":"main"},"head":{"ref":"feature","sha":%q,"repo":{"owner":{"login":"contributor"}}}}]`, featureSHA)
	githubCommands := traceGitHubAPI(t, githubSnapshotResponse(mainSHA, featureSHA, `null`, false), rest)
	t.Setenv("_TREEMAN_REMOTE_URL", "https://github.com/owner/repo.git")
	changeToDir(t, repo)
	commands := traceGitCommands(t, false)

	output := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(output)
	require.NoError(t, runList(cmd, true))

	var entries []listEntry
	require.NoError(t, json.Unmarshal(output.Bytes(), &entries))
	require.Len(t, entries, 2)
	assert.True(t, entries[1].Merged, "fork PR verification must retain the REST fallback")
	assert.Equal(t, 1, countGitHubCommands(githubCommands(), "api graphql"))
	assert.Equal(t, 1, countGitHubCommands(githubCommands(), "api repos/owner/repo/commits/"))
	assert.Zero(t, countGitCommands(commands(), "ls-remote"))
}

func TestRunListFallsBackToGitRemoteStateWhenGitHubSnapshotFails(t *testing.T) {
	repo, _ := createMergedCleanWorktree(t)
	githubCommands := traceGitHubAPI(t, `not-json`, `[]`)
	t.Setenv("_TREEMAN_REMOTE_URL", "https://github.com/owner/repo.git")
	changeToDir(t, repo)
	commands := traceGitCommands(t, false)

	output := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(output)
	require.NoError(t, runList(cmd, true))

	var entries []listEntry
	require.NoError(t, json.Unmarshal(output.Bytes(), &entries))
	require.Len(t, entries, 2)
	assert.True(t, entries[1].Merged)
	assert.Equal(t, 1, countGitHubCommands(githubCommands(), "api graphql"))
	assert.Equal(t, 1, countGitCommands(commands(), "ls-remote --heads origin refs/heads/main refs/heads/feature"))
}

func TestRunListDetectsSquashMergedBranch(t *testing.T) {
	// Simulates GitLab's squash-merge workflow:
	//   1. Feature branch is pushed.
	//   2. A squash commit (new SHA, not an ancestor of feature tip) lands on main.
	//   3. The remote feature branch is deleted (GitLab default).
	// The branch tip is NOT an ancestor of origin/main, so git branch --merged
	// won't catch it. treeman should still mark it merged once the forge
	// confirms a merged PR/MR whose head SHA equals the local branch tip.
	repo, _ := createSquashMergedCleanWorktree(t)
	stubForgeVerifier(t, []string{gitRevParse(t, repo, "refs/heads/feature")}, nil)

	changeToDir(t, repo)

	output := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(output)
	require.NoError(t, runList(cmd, true))

	var entries []listEntry
	require.NoError(t, json.Unmarshal(output.Bytes(), &entries))
	require.Len(t, entries, 2)
	assert.True(t, entries[1].Merged, "forge-verified squash merge should be marked merged")
}

func TestRunListRemoteGoneWithoutConfirmedMergeNotMarked(t *testing.T) {
	// A deleted remote branch alone no longer proves a merge. Without a
	// merged head SHA matching the local tip the branch must stay unmarked
	// (and clean will retain it).
	repo, _ := createSquashMergedCleanWorktree(t)
	stubForgeVerifier(t, []string{"not-the-local-tip"}, nil)

	changeToDir(t, repo)

	output := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(output)
	require.NoError(t, runList(cmd, true))

	var entries []listEntry
	require.NoError(t, json.Unmarshal(output.Bytes(), &entries))
	require.Len(t, entries, 2)
	assert.False(t, entries[1].Merged, "remote-gone branch without confirmed merge should not be marked merged")
}
