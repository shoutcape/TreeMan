package agentworktree

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func samePathForTest(a, b string) bool { return filepath.Clean(a) == filepath.Clean(b) }

func TestStorePathIsUnderTheGitCommonDirectory(t *testing.T) {
	common := t.TempDir()
	store := Open(common)
	assert.Equal(t, filepath.Join(common, "treeman", "agent-worktrees.json"), store.Path())
}

func TestListOfAnUnwrittenStoreIsEmptyAndCreatesNothing(t *testing.T) {
	common := t.TempDir()
	records, err := Open(common).List()
	require.NoError(t, err)
	assert.Empty(t, records)
	assert.NoDirExists(t, filepath.Join(common, "treeman"))
}

func TestPutWritesTheDocumentedFields(t *testing.T) {
	common := t.TempDir()
	store := Open(common)
	created := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.Put(Record{
		Agent: "claude", Name: "x", Branch: "feature/x", BranchCreated: true,
		CreatedSHA: "abc", CreatedAt: created, WorktreePath: "/w/x", SessionID: "s1",
	}))

	data, err := os.ReadFile(store.Path())
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc))
	records, ok := doc["records"].([]any)
	require.True(t, ok, "records must be a JSON array: %s", data)
	require.Len(t, records, 1)
	record := records[0].(map[string]any)
	for _, key := range []string{"agent", "name", "branch", "branch_created", "created_sha", "created_at", "worktree_path", "session_id"} {
		assert.Contains(t, record, key)
	}
	assert.Equal(t, "claude", record["agent"])
	assert.Equal(t, "feature/x", record["branch"])
	assert.Equal(t, true, record["branch_created"])
	assert.Equal(t, "/w/x", record["worktree_path"])
	assert.Equal(t, "s1", record["session_id"])

	listed, err := store.List()
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.True(t, listed[0].CreatedAt.Equal(created))
}

func TestPutReplacesTheRecordForTheSameAgentAndName(t *testing.T) {
	store := Open(t.TempDir())
	require.NoError(t, store.Put(Record{Agent: "claude", Name: "x", Branch: "feature/x", WorktreePath: "/a"}))
	require.NoError(t, store.Put(Record{Agent: "claude", Name: "y", Branch: "feature/y", WorktreePath: "/b"}))
	require.NoError(t, store.Put(Record{Agent: "other", Name: "x", Branch: "feature/x", WorktreePath: "/c"}))
	require.NoError(t, store.Put(Record{Agent: "claude", Name: "x", Branch: "feature/x", WorktreePath: "/d"}))

	records, err := store.List()
	require.NoError(t, err)
	require.Len(t, records, 3)
	got, ok := Find(records, "claude", "x")
	require.True(t, ok)
	assert.Equal(t, "/d", got.WorktreePath)
	got, ok = Find(records, "other", "x")
	require.True(t, ok)
	assert.Equal(t, "/c", got.WorktreePath)
}

func TestForgetBranchDropsEveryRecordForThatBranchOnly(t *testing.T) {
	store := Open(t.TempDir())
	require.NoError(t, store.Put(Record{Agent: "claude", Name: "x", Branch: "feature/x", BranchCreated: true}))
	require.NoError(t, store.Put(Record{Agent: "claude", Name: "feature-x", Branch: "feature/x"}))
	require.NoError(t, store.Put(Record{Agent: "claude", Name: "y", Branch: "feature/y"}))

	require.NoError(t, store.ForgetBranch("feature/x"))

	records, err := store.List()
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "feature/y", records[0].Branch)
}

func TestForgetWorktreeKeepsCreatedBranchRecordsWithoutAPath(t *testing.T) {
	store := Open(t.TempDir())
	require.NoError(t, store.Put(Record{Agent: "claude", Name: "owned", Branch: "feature/owned", BranchCreated: true, CreatedSHA: "a", WorktreePath: "/w/owned"}))
	require.NoError(t, store.Put(Record{Agent: "claude", Name: "attached", Branch: "feature/attached", WorktreePath: "/w/attached"}))
	require.NoError(t, store.Put(Record{Agent: "claude", Name: "other", Branch: "feature/other", BranchCreated: true, WorktreePath: "/w/other"}))

	require.NoError(t, store.ForgetWorktree("/w/owned/", samePathForTest))
	require.NoError(t, store.ForgetWorktree("/w/attached", samePathForTest))

	records, err := store.List()
	require.NoError(t, err)
	owned, ok := Find(records, "claude", "owned")
	require.True(t, ok, "a record for a branch TreeMan created must survive worktree removal")
	assert.Empty(t, owned.WorktreePath)
	assert.True(t, owned.BranchCreated)
	assert.Equal(t, "a", owned.CreatedSHA)
	// A record that never owned its branch may be kept or dropped, but it
	// must neither point at the removed worktree nor claim the branch.
	if attached, ok := Find(records, "claude", "attached"); ok {
		assert.Empty(t, attached.WorktreePath)
		assert.False(t, attached.BranchCreated)
	}
	other, ok := Find(records, "claude", "other")
	require.True(t, ok)
	assert.Equal(t, "/w/other", other.WorktreePath)
}

func TestUpdateErrorLeavesRecordsUnchanged(t *testing.T) {
	store := Open(t.TempDir())
	require.NoError(t, store.Put(Record{Agent: "claude", Name: "x", Branch: "feature/x"}))
	before, err := os.ReadFile(store.Path())
	require.NoError(t, err)

	boom := errors.New("boom")
	err = store.Update(func([]Record) ([]Record, error) { return nil, boom })
	require.ErrorIs(t, err, boom)

	after, err := os.ReadFile(store.Path())
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestUpdateToNothingLeavesAReadableEmptyStore(t *testing.T) {
	store := Open(t.TempDir())
	require.NoError(t, store.Put(Record{Agent: "claude", Name: "x", Branch: "feature/x"}))
	require.NoError(t, store.ForgetBranch("feature/x"))
	records, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestConcurrentPutsAreNotLost(t *testing.T) {
	common := t.TempDir()
	const writers = 16
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Separate Store values, as separate processes would have.
			errs <- Open(common).Put(Record{Agent: "claude", Name: string(rune('a' + i)), Branch: "feature/" + string(rune('a'+i))})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	records, err := Open(common).List()
	require.NoError(t, err)
	assert.Len(t, records, writers)
}

func TestListRejectsCorruptAndUnknownVersionDocuments(t *testing.T) {
	for name, data := range map[string]string{
		"invalid JSON":    "{not json",
		"unknown version": `{"version":99,"records":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			store := Open(t.TempDir())
			require.NoError(t, os.MkdirAll(filepath.Dir(store.Path()), 0o700))
			require.NoError(t, os.WriteFile(store.Path(), []byte(data), 0o600))
			_, err := store.List()
			assert.Error(t, err)
			// An update must fail closed rather than overwrite what it cannot read.
			err = store.Put(Record{Agent: "claude", Name: "x", Branch: "feature/x"})
			assert.Error(t, err)
			got, readErr := os.ReadFile(store.Path())
			require.NoError(t, readErr)
			assert.Equal(t, data, string(got))
		})
	}
}

func TestFindByPathIgnoresRecordsWithoutAWorktree(t *testing.T) {
	records := []Record{
		{Agent: "claude", Name: "gone", Branch: "feature/gone"},
		{Agent: "claude", Name: "here", Branch: "feature/here", WorktreePath: "/w/here"},
	}
	_, ok := FindByPath(records, "", samePathForTest)
	assert.False(t, ok)
	got, ok := FindByPath(records, "/w/here", samePathForTest)
	require.True(t, ok)
	assert.Equal(t, "here", got.Name)
	_, ok = FindByPath(records, "/w/elsewhere", samePathForTest)
	assert.False(t, ok)
}
