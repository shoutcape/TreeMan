package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/shoutcape/treeman/internal/hooks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testScope(t *testing.T, n string) hooks.ApprovalScope {
	t.Helper()
	root := t.TempDir()
	scope, err := hooks.NewApprovalScope(root, filepath.Join(root, n+".toml"), hooks.PostCreatePhase, []string{"echo " + n})
	require.NoError(t, err)
	return scope
}

func TestHookApprovalStoreLifecycleAndPermissions(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	store, err := NewHookApprovalStore("")
	require.NoError(t, err)
	scope := testScope(t, "one")

	found, err := store.Lookup(scope)
	require.NoError(t, err)
	assert.False(t, found)
	require.NoError(t, store.Approve(scope))
	found, err = store.Lookup(scope)
	require.NoError(t, err)
	assert.True(t, found)
	list, err := store.List()
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, scope, list[0].Scope)
	assert.Equal(t, scope.ID(), list[0].ID)
	require.NoError(t, store.Revoke(list[0].ID))
	list, err = store.List()
	require.NoError(t, err)
	assert.Empty(t, list)
	assert.Equal(t, os.FileMode(0o700), fileMode(t, filepath.Join(stateHome, "treeman")))
	assert.Equal(t, os.FileMode(0o600), fileMode(t, filepath.Join(stateHome, "treeman", "hook-approvals.json")))
	assert.Equal(t, os.FileMode(0o600), fileMode(t, filepath.Join(stateHome, "treeman", "hook-approvals.lock")))
}

func TestHookApprovalStoreUsesCanonicalHomeFallback(t *testing.T) {
	homeTarget := t.TempDir()
	homeTarget, err := filepath.EvalSymlinks(homeTarget)
	require.NoError(t, err)
	homeLink := filepath.Join(t.TempDir(), "home")
	require.NoError(t, os.Symlink(homeTarget, homeLink))
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", homeLink)

	store, err := NewHookApprovalStore("")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(homeTarget, ".local", "state", "treeman"), store.dir)
	assert.DirExists(t, store.dir)
}

func TestHookApprovalStoreCanonicalizesAllowedStateParentSymlink(t *testing.T) {
	target := t.TempDir()
	target, err := filepath.EvalSymlinks(target)
	require.NoError(t, err)
	parent := filepath.Join(t.TempDir(), "state-parent")
	require.NoError(t, os.Symlink(target, parent))
	t.Setenv("XDG_STATE_HOME", parent)

	store, err := NewHookApprovalStore("")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(target, "treeman"), store.dir)
}

func TestHookApprovalStoreRejectsRepositoryStateThroughParentSymlinkWithoutSideEffects(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	stateParent := filepath.Join(t.TempDir(), "state-parent")
	require.NoError(t, os.Symlink(root, stateParent))
	before, err := os.Stat(root)
	require.NoError(t, err)
	t.Setenv("XDG_STATE_HOME", stateParent)

	_, err = NewHookApprovalStore(filepath.Join(root, ".git"))
	require.Error(t, err)
	assert.NoDirExists(t, filepath.Join(root, "treeman"))
	after, err := os.Stat(root)
	require.NoError(t, err)
	assert.Equal(t, before.Mode().Perm(), after.Mode().Perm())
}

// Approvals decide what a repository's own configuration may run, so they may
// not be stored anywhere that repository reaches: not its Git directory, not
// its tree, and not a worktree placed inside it.
func TestHookApprovalStoreRejectsStateInsideTheRepository(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	for _, inside := range []string{"state", filepath.Join(".git", "state"), filepath.Join(".worktrees", "feature", "state")} {
		t.Run(inside, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", filepath.Join(root, inside))
			store, err := NewHookApprovalStore(filepath.Join(root, ".git"))
			require.Error(t, err, "state inside the repository must be rejected")
			assert.Nil(t, store)
		})
	}
}

// Listing and revoking approvals has to work from anywhere, so a location
// outside every repository is simply used.
func TestHookApprovalStoreAcceptsStateOutsideAnyRepository(t *testing.T) {
	outside := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(outside, "state"))

	store, err := NewHookApprovalStore("")
	require.NoError(t, err)
	list, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestHookApprovalStoreRejectsRelativeStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "not-absolute")
	_, err := NewHookApprovalStore("")
	require.Error(t, err)
}

func TestHookApprovalStoreDoesNotBlockOnInterruptedTemporaryFile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	store, err := NewHookApprovalStore("")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(store.dir, ".hook-approvals-stale"), []byte("partial"), 0o600))
	require.NoError(t, store.Approve(testScope(t, "after-interruption")))
	assert.FileExists(t, filepath.Join(store.dir, ".hook-approvals-stale"))
}

func TestHookApprovalStoreRejectsInvalidState(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	store, err := NewHookApprovalStore("")
	require.NoError(t, err)
	path := filepath.Join(stateHome, "treeman", "hook-approvals.json")

	for name, contents := range map[string]string{
		"malformed": "{",
		"version":   `{"version":99,"approvals":[]}`,
		"invalid":   `{"version":1,"approvals":[{"id":"wrong","scope":{},"approved_at":"2025-01-01T00:00:00Z"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
			_, err := store.List()
			assert.Error(t, err)
		})
	}
}

func TestHookApprovalStoreConcurrentApprovals(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	store, err := NewHookApprovalStore("")
	require.NoError(t, err)
	var scopes []hooks.ApprovalScope
	for i := 0; i < 12; i++ {
		scopes = append(scopes, testScope(t, string(rune('a'+i))))
	}
	start := make(chan struct{})
	errors := make(chan error, len(scopes))
	var wg sync.WaitGroup
	for _, scope := range scopes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errors <- store.Approve(scope)
		}()
	}
	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	list, err := store.List()
	require.NoError(t, err)
	assert.Len(t, list, 12)
}

func TestHookApprovalStoreConcurrentApprovalsAndRevocations(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	store, err := NewHookApprovalStore("")
	require.NoError(t, err)
	var initial, additions []hooks.ApprovalScope
	for i := 0; i < 8; i++ {
		initial = append(initial, testScope(t, "initial-"+string(rune('a'+i))))
		additions = append(additions, testScope(t, "addition-"+string(rune('a'+i))))
		require.NoError(t, store.Approve(initial[i]))
	}

	start := make(chan struct{})
	errors := make(chan error, len(initial)+len(additions))
	var wg sync.WaitGroup
	for i := range initial {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			<-start
			errors <- store.Revoke(initial[i].ID())
		}(i)
		go func(i int) {
			defer wg.Done()
			<-start
			errors <- store.Approve(additions[i])
		}(i)
	}
	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}

	list, err := store.List()
	require.NoError(t, err)
	wantIDs := make([]string, 0, len(additions))
	for _, scope := range additions {
		wantIDs = append(wantIDs, scope.ID())
	}
	gotIDs := make([]string, 0, len(list))
	for _, approval := range list {
		gotIDs = append(gotIDs, approval.ID)
	}
	assert.ElementsMatch(t, wantIDs, gotIDs)
}

func TestHookApprovalStoreRejectsStateSymlink(t *testing.T) {
	if filepath.Separator == '\\' {
		t.Skip("symlink permissions vary on Windows")
	}
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	require.NoError(t, os.MkdirAll(filepath.Join(stateHome, "treeman"), 0o700))
	target := filepath.Join(stateHome, "real.json")
	require.NoError(t, os.WriteFile(target, []byte(`{"version":1,"approvals":[]}`), 0o600))
	require.NoError(t, os.Symlink(target, filepath.Join(stateHome, "treeman", "hook-approvals.json")))
	_, err := NewHookApprovalStore("")
	assert.Error(t, err)
}

func TestHookApprovalStoreRejectsInvalidEntriesBeforeChmod(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("FIFO support is only required on Linux and Darwin")
	}
	for _, kind := range []string{"directory", "fifo", "symlink"} {
		for _, entry := range []string{"hook-approvals.json", "hook-approvals.lock"} {
			t.Run(kind+"/"+entry, func(t *testing.T) {
				stateHome := t.TempDir()
				dir := filepath.Join(stateHome, "treeman")
				require.NoError(t, os.MkdirAll(dir, 0o700))
				path := filepath.Join(dir, entry)
				var target string
				switch kind {
				case "directory":
					require.NoError(t, os.Mkdir(path, 0o644))
				case "fifo":
					require.NoError(t, syscall.Mkfifo(path, 0o644))
				case "symlink":
					target = filepath.Join(stateHome, "target")
					require.NoError(t, os.WriteFile(target, []byte("target"), 0o644))
					require.NoError(t, os.Symlink(target, path))
				}
				before, err := os.Lstat(path)
				require.NoError(t, err)
				t.Setenv("XDG_STATE_HOME", stateHome)
				_, err = NewHookApprovalStore("")
				require.Error(t, err)
				after, err := os.Lstat(path)
				require.NoError(t, err)
				assert.Equal(t, before.Mode(), after.Mode())
				if target != "" {
					assert.Equal(t, os.FileMode(0o644), fileMode(t, target))
				}
			})
		}
	}
}

func TestHookApprovalStoreRejectsDirectorySymlinkBeforeChmod(t *testing.T) {
	stateHome := t.TempDir()
	target := t.TempDir()
	dir := filepath.Join(stateHome, "treeman")
	require.NoError(t, os.Symlink(target, dir))
	before, err := os.Stat(target)
	require.NoError(t, err)
	t.Setenv("XDG_STATE_HOME", stateHome)

	_, err = NewHookApprovalStore("")
	require.Error(t, err)
	after, err := os.Stat(target)
	require.NoError(t, err)
	assert.Equal(t, before.Mode().Perm(), after.Mode().Perm())
}

func TestHookApprovalStoreOperationsRejectInvalidEntriesBeforeBlocking(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("FIFO support is only required on Linux and Darwin")
	}
	for _, kind := range []string{"directory", "fifo", "symlink"} {
		for _, entry := range []string{"hook-approvals.json", "hook-approvals.lock"} {
			t.Run(kind+"/"+entry, func(t *testing.T) {
				t.Setenv("XDG_STATE_HOME", t.TempDir())
				store, err := NewHookApprovalStore("")
				require.NoError(t, err)
				path := store.path
				if entry == "hook-approvals.lock" {
					path = store.lockPath
				}
				switch kind {
				case "directory":
					require.NoError(t, os.Mkdir(path, 0o700))
				case "fifo":
					require.NoError(t, syscall.Mkfifo(path, 0o600))
				case "symlink":
					target := filepath.Join(t.TempDir(), "target")
					require.NoError(t, os.WriteFile(target, []byte("target"), 0o600))
					require.NoError(t, os.Symlink(target, path))
				}
				scope := testScope(t, "invalid-entry")
				result := make(chan error, 1)
				go func() { result <- store.Approve(scope) }()
				select {
				case err := <-result:
					require.Error(t, err)
				case <-time.After(time.Second):
					t.Fatal("operation blocked on invalid state entry")
				}
			})
		}
	}
}

func TestHookApprovalStoreOperationsRejectCanonicalDirectorySymlink(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(*HookApprovalStore, hooks.ApprovalScope) error
	}{
		{name: "Lookup", run: func(store *HookApprovalStore, scope hooks.ApprovalScope) error {
			_, err := store.Lookup(scope)
			return err
		}},
		{name: "List", run: func(store *HookApprovalStore, _ hooks.ApprovalScope) error {
			_, err := store.List()
			return err
		}},
		{name: "Approve", run: (*HookApprovalStore).Approve},
		{name: "Revoke", run: func(store *HookApprovalStore, scope hooks.ApprovalScope) error {
			return store.Revoke(scope.ID())
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			commonDir := filepath.Join(root, ".git")
			require.NoError(t, os.Mkdir(commonDir, 0o700))
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			store, err := NewHookApprovalStore(commonDir)
			require.NoError(t, err)
			scope, err := hooks.NewApprovalScope(commonDir, filepath.Join(root, ".treeman.toml"), hooks.PostCreatePhase, []string{"echo test"})
			require.NoError(t, err)
			require.NoError(t, store.Approve(scope))
			original, err := os.ReadFile(store.path)
			require.NoError(t, err)

			target := filepath.Join(root, "state")
			require.NoError(t, os.Mkdir(target, 0o700))
			document := filepath.Join(target, filepath.Base(store.path))
			require.NoError(t, os.WriteFile(document, original, 0o600))
			require.NoError(t, os.Rename(store.dir, store.dir+"-backup"))
			require.NoError(t, os.Symlink(target, store.dir))

			assert.ErrorContains(t, test.run(store, scope), "approval state directory changed or is a symlink")
			after, err := os.ReadFile(document)
			require.NoError(t, err)
			assert.Equal(t, original, after)
			entries, err := os.ReadDir(target)
			require.NoError(t, err)
			require.Len(t, entries, 1, "rejected operation must not create lock or temporary files")
			assert.Equal(t, filepath.Base(document), entries[0].Name())
		})
	}
}

func TestHookApprovalStoreKeepsCanonicalPathAfterParentRetarget(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	parent := filepath.Join(t.TempDir(), "state-parent")
	require.NoError(t, os.Symlink(first, parent))
	t.Setenv("XDG_STATE_HOME", parent)
	store, err := NewHookApprovalStore("")
	require.NoError(t, err)
	require.NoError(t, os.Remove(parent))
	require.NoError(t, os.Symlink(second, parent))
	require.NoError(t, store.Approve(testScope(t, "stable-path")))
	assert.FileExists(t, filepath.Join(first, "treeman", "hook-approvals.json"))
	assert.NoFileExists(t, filepath.Join(second, "treeman", "hook-approvals.json"))
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.Mode().Perm()
}

func TestHookApprovalJSONIsInspectable(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	store, err := NewHookApprovalStore("")
	require.NoError(t, err)
	require.NoError(t, store.Approve(testScope(t, "json")))
	data, err := os.ReadFile(store.path)
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, json.Unmarshal(data, &document))
	assert.Equal(t, float64(hookApprovalVersion), document["version"])
}
