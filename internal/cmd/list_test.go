package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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

	assert.Equal(t, "\nWORKTREES\n\n    MARKERS  STATUS    MERGED  BRANCH                       PATH                     \n    ───────  ──────    ──────  ───────────────────────────  ─────────────────────────\n    M▶       CLEAN             main                         /repo\n             DIRTY     YES     feature                      /repo/.worktrees/feature\n             DETACHED          (detached)                   /repo/.worktrees/review\n", ui.StripANSI(buf.String()))
}

func TestWriteListJSON(t *testing.T) {
	buf := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(buf)
	entries := []listEntry{{Path: "/repo", Branch: "main", Main: true, Current: true}}

	require.NoError(t, writeListJSON(cmd, entries))
	var decoded []listEntry
	require.NoError(t, json.Unmarshal(buf.Bytes(), &decoded))
	assert.Equal(t, entries, decoded)
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

	previousDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repo))
	t.Cleanup(func() { require.NoError(t, os.Chdir(previousDir)) })

	output := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(output)
	require.NoError(t, runList(cmd, true))

	var entries []listEntry
	require.NoError(t, json.Unmarshal(output.Bytes(), &entries))
	require.Len(t, entries, 2)
	assert.True(t, entries[1].Merged)
}
