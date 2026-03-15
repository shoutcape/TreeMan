package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shoutcape/TreeMan/internal/runtime"
)

func TestDoDeleteDoesNotCleanupWhenWorktreeRemoveFails(t *testing.T) {
	reset := stubDeleteDeps(t)
	defer reset()

	state := &runtime.RuntimeState{Repo: "repo", BranchSlug: "feature-test", Status: "running"}
	loadRuntimeStateFn = func(repo, branchSlug string) (*runtime.RuntimeState, error) {
		return state, nil
	}
	checkProcessStatusFn = func(*runtime.RuntimeState) string { return "running" }

	stopped := false
	stopProcessFn = func(got *runtime.RuntimeState) error {
		if got != state {
			t.Fatalf("stopProcess got unexpected state")
		}
		stopped = true
		return nil
	}

	cleaned := false
	cleanupRuntimeArtifactsFn = func(*runtime.RuntimeState) error {
		cleaned = true
		return nil
	}

	branchDeleted := false
	branchDeleteFn = func(string) error {
		branchDeleted = true
		return nil
	}

	worktreeRemoveFn = func(string) error { return fmt.Errorf("dirty worktree") }

	err := doDelete("/tmp/repo.feature-test", "feature/test", "/tmp/repo")
	if err == nil || !strings.Contains(err.Error(), "failed to remove worktree") {
		t.Fatalf("expected worktree remove error, got %v", err)
	}
	if !stopped {
		t.Fatal("expected runtime to be stopped before deletion")
	}
	if cleaned {
		t.Fatal("did not expect runtime artifacts to be cleaned when worktree removal fails")
	}
	if branchDeleted {
		t.Fatal("did not expect branch delete when worktree removal fails")
	}
}

func TestDoDeleteDoesNotCleanupWhenBranchDeleteFails(t *testing.T) {
	reset := stubDeleteDeps(t)
	defer reset()

	state := &runtime.RuntimeState{Repo: "repo", BranchSlug: "feature-test", Status: "stopped"}
	loadRuntimeStateFn = func(repo, branchSlug string) (*runtime.RuntimeState, error) {
		return state, nil
	}
	checkProcessStatusFn = func(*runtime.RuntimeState) string { return "stopped" }
	worktreeRemoveFn = func(string) error { return nil }
	branchDeleteFn = func(string) error { return fmt.Errorf("branch in use") }

	cleaned := false
	cleanupRuntimeArtifactsFn = func(*runtime.RuntimeState) error {
		cleaned = true
		return nil
	}

	err := doDelete("/tmp/repo.feature-test", "feature/test", "/tmp/repo")
	if err == nil || !strings.Contains(err.Error(), "could not be deleted") {
		t.Fatalf("expected branch delete error, got %v", err)
	}
	if cleaned {
		t.Fatal("did not expect runtime artifacts to be cleaned when branch deletion fails")
	}
}

func TestDoDeleteCleansRuntimeArtifactsAfterSuccessfulDelete(t *testing.T) {
	reset := stubDeleteDeps(t)
	defer reset()

	state := &runtime.RuntimeState{Repo: "repo", BranchSlug: "feature-test", Status: "running"}
	loadRuntimeStateFn = func(repo, branchSlug string) (*runtime.RuntimeState, error) {
		return state, nil
	}
	checkProcessStatusFn = func(*runtime.RuntimeState) string { return "running" }

	var calls []string
	stopProcessFn = func(*runtime.RuntimeState) error {
		calls = append(calls, "stop")
		return nil
	}
	worktreeRemoveFn = func(string) error {
		calls = append(calls, "remove")
		return nil
	}
	branchDeleteFn = func(string) error {
		calls = append(calls, "branch")
		return nil
	}
	cleanupRuntimeArtifactsFn = func(*runtime.RuntimeState) error {
		calls = append(calls, "cleanup")
		return nil
	}

	stdoutPath := filepath.Join(t.TempDir(), "stdout.txt")
	stdoutFile, err := os.Create(stdoutPath)
	if err != nil {
		t.Fatalf("create stdout capture: %v", err)
	}
	defer stdoutFile.Close()

	origStdout := os.Stdout
	os.Stdout = stdoutFile
	defer func() { os.Stdout = origStdout }()

	if err := doDelete("/tmp/repo.feature-test", "feature/test", "/tmp/repo"); err != nil {
		t.Fatalf("doDelete: %v", err)
	}

	got := strings.Join(calls, ",")
	if got != "stop,remove,branch,cleanup" {
		t.Fatalf("unexpected call order: %s", got)
	}
}

func stubDeleteDeps(t *testing.T) func() {
	t.Helper()

	origDefaultBranch := defaultBranchFn
	origLoadRuntimeState := loadRuntimeStateFn
	origCheckProcessStatus := checkProcessStatusFn
	origStopProcess := stopProcessFn
	origCleanupRuntimeArtifacts := cleanupRuntimeArtifactsFn
	origWorktreeRemove := worktreeRemoveFn
	origBranchDelete := branchDeleteFn

	defaultBranchFn = func() (string, error) { return "main", nil }
	loadRuntimeStateFn = func(string, string) (*runtime.RuntimeState, error) { return nil, nil }
	checkProcessStatusFn = func(*runtime.RuntimeState) string { return "stopped" }
	stopProcessFn = func(*runtime.RuntimeState) error { return nil }
	cleanupRuntimeArtifactsFn = func(*runtime.RuntimeState) error { return nil }
	worktreeRemoveFn = func(string) error { return nil }
	branchDeleteFn = func(string) error { return nil }

	return func() {
		defaultBranchFn = origDefaultBranch
		loadRuntimeStateFn = origLoadRuntimeState
		checkProcessStatusFn = origCheckProcessStatus
		stopProcessFn = origStopProcess
		cleanupRuntimeArtifactsFn = origCleanupRuntimeArtifacts
		worktreeRemoveFn = origWorktreeRemove
		branchDeleteFn = origBranchDelete
	}
}
