package deps

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestValidateReusePaths(t *testing.T) {
	for _, paths := range [][]string{
		{""}, {"."}, {"../deps"}, {"foo/../bar"}, {".git/../cache"}, {"/deps"}, {".git/config"}, {".GIT/x"}, {"foo/.Git"}, {"foo/.gIt/bar"}, {"a\\.GIT\\b"}, {"deps", "deps/./"}, {"deps", "deps/nested"},
	} {
		assert.Error(t, ValidateReusePaths(paths), paths)
	}
	assert.NoError(t, ValidateReusePaths([]string{"packages/api/node_modules", "node_modules"}))
}

func TestReuseCopiesIndependently(t *testing.T) {
	source := t.TempDir()
	destination := t.TempDir()
	selected := filepath.Join(source, "deps")
	require.NoError(t, os.MkdirAll(filepath.Join(selected, "empty"), 0o751))
	require.NoError(t, os.WriteFile(filepath.Join(selected, "file"), []byte("source"), 0o640))
	require.NoError(t, os.Chmod(filepath.Join(selected, "file"), 0o640))
	require.NoError(t, os.Symlink("file", filepath.Join(selected, "link")))
	require.NoError(t, os.Symlink("missing", filepath.Join(selected, "dangling")))

	status, err := Reuse(source, destination, "deps")
	require.NoError(t, err)
	assert.Equal(t, ReuseCopied, status)
	copied := filepath.Join(destination, "deps")
	contents, err := os.ReadFile(filepath.Join(copied, "file"))
	require.NoError(t, err)
	assert.Equal(t, "source", string(contents))
	info, err := os.Stat(filepath.Join(copied, "empty"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	mode, err := os.Stat(filepath.Join(copied, "file"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), mode.Mode().Perm())
	link, err := os.Readlink(filepath.Join(copied, "link"))
	require.NoError(t, err)
	assert.Equal(t, "file", link)
	_, err = os.Lstat(filepath.Join(copied, "dangling"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(copied, "file"), []byte("changed"), 0o600))
	original, err := os.ReadFile(filepath.Join(selected, "file"))
	require.NoError(t, err)
	assert.Equal(t, "source", string(original))
}

func TestReuseStatusesAndValidation(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	status, err := Reuse(source, destination, "missing")
	require.NoError(t, err)
	assert.Equal(t, ReuseSourceMissing, status)
	require.NoError(t, os.Mkdir(filepath.Join(source, "deps"), 0o755))
	require.NoError(t, os.Mkdir(filepath.Join(destination, "deps"), 0o755))
	status, err = Reuse(source, destination, "deps")
	require.NoError(t, err)
	assert.Equal(t, ReuseDestinationExists, status)
	status, err = Reuse(source, destination, "../deps")
	assert.Zero(t, status)
	assert.Error(t, err)
}

func TestReuseCreatesMissingDestinationParentsAndRejectsSameDirectory(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(source, "packages", "api"), 0o755))
	status, err := Reuse(source, destination, "packages/api")
	require.NoError(t, err)
	assert.Equal(t, ReuseCopied, status)
	_, err = os.Stat(filepath.Join(destination, "packages", "api"))
	require.NoError(t, err)
	before := snapshotTree(t, source)
	status, err = Reuse(source, source, "packages/api")
	require.ErrorContains(t, err, "source and destination trees overlap")
	assert.NotContains(t, err.Error(), "same directory")
	assert.Zero(t, status)
	assert.Equal(t, before, snapshotTree(t, source))
}

func TestReuseResolvesSymlinkComponentsBeforeDotDot(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	selected := filepath.Join(source, "deps")
	require.NoError(t, os.Mkdir(selected, 0o755))
	require.NoError(t, os.Symlink("../../outside", filepath.Join(selected, "alias")))
	require.NoError(t, os.Symlink("alias/../outside", filepath.Join(selected, "escape")))
	_, err := Reuse(source, destination, "deps")
	assert.Error(t, err)
}

func TestReuseAllowsContainedNestedSymlinkDotDot(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	selected := filepath.Join(source, "deps")
	require.NoError(t, os.MkdirAll(filepath.Join(selected, ".bin"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(selected, "tool"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(selected, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(selected, "tool", "bin.js"), []byte("tool"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(selected, "sub", "file"), []byte("sub"), 0o644))
	require.NoError(t, os.Symlink("../tool/bin.js", filepath.Join(selected, ".bin", "tool")))
	require.NoError(t, os.Symlink("../sub/file", filepath.Join(selected, "sub", "link")))
	status, err := Reuse(source, destination, "deps")
	require.NoError(t, err)
	assert.Equal(t, ReuseCopied, status)
}

func TestReuseCopiesNestedGitDirectoriesAsContent(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	selected := filepath.Join(source, "deps")
	gitDir := filepath.Join(selected, "pkg", ".git")
	require.NoError(t, os.MkdirAll(filepath.Join(gitDir, "refs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(selected, "other"), 0o755))
	require.NoError(t, os.Symlink("../pkg/.git/HEAD", filepath.Join(selected, "other", "head-link")))
	require.NoError(t, os.Symlink("pkg/.git", filepath.Join(selected, "git-dir-link")))

	status, err := Reuse(source, destination, "deps")
	require.NoError(t, err)
	assert.Equal(t, ReuseCopied, status)
	copied := filepath.Join(destination, "deps")
	contents, err := os.ReadFile(filepath.Join(copied, "pkg", ".git", "HEAD"))
	require.NoError(t, err)
	assert.Equal(t, "ref: refs/heads/main\n", string(contents))
	info, err := os.Stat(filepath.Join(copied, "pkg", ".git", "refs"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	link, err := os.Readlink(filepath.Join(copied, "other", "head-link"))
	require.NoError(t, err)
	assert.Equal(t, "../pkg/.git/HEAD", link)
	viaLink, err := os.ReadFile(filepath.Join(copied, "other", "head-link"))
	require.NoError(t, err)
	assert.Equal(t, "ref: refs/heads/main\n", string(viaLink))
	link, err = os.Readlink(filepath.Join(copied, "git-dir-link"))
	require.NoError(t, err)
	assert.Equal(t, "pkg/.git", link)
}

func TestReuseRejectsNestedGitLinkThatEscapes(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	selected := filepath.Join(source, "deps")
	require.NoError(t, os.MkdirAll(filepath.Join(selected, "pkg"), 0o755))
	require.NoError(t, os.Symlink("../../.git/HEAD", filepath.Join(selected, "pkg", "escape")))
	_, err := Reuse(source, destination, "deps")
	require.Error(t, err)
	_, err = os.Lstat(filepath.Join(destination, "deps"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestReuseRejectsGitComponentInAnyCase(t *testing.T) {
	for _, path := range []string{".git", ".GIT/x", "foo/.Git", "foo/.gIT/bar"} {
		t.Run(path, func(t *testing.T) {
			source, destination := t.TempDir(), t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(source, path), 0o755))
			status, err := Reuse(source, destination, path)
			require.Error(t, err)
			assert.Zero(t, status)
			entries, err := os.ReadDir(destination)
			require.NoError(t, err)
			assert.Empty(t, entries)
		})
	}
}

func TestReuseCleansReadOnlyDirectories(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	selected := filepath.Join(source, "deps")
	readonly := filepath.Join(selected, "a-readonly")
	require.NoError(t, os.MkdirAll(readonly, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(readonly, "file"), []byte("x"), 0o644))
	require.NoError(t, os.Chmod(readonly, 0o555))
	t.Cleanup(func() { _ = os.Chmod(readonly, 0o755) })
	require.NoError(t, unix.Mkfifo(filepath.Join(selected, "z-fifo"), 0o644))
	_, err := Reuse(source, destination, "deps")
	assert.Error(t, err)
	_, err = os.Lstat(filepath.Join(destination, "deps"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestReuseManyDirectoriesDoesNotNeedOneDescriptorEach(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	selected := filepath.Join(source, "deps")
	require.NoError(t, os.Mkdir(selected, 0o755))
	for i := 0; i < 1200; i++ {
		require.NoError(t, os.Mkdir(filepath.Join(selected, fmt.Sprintf("d-%04d", i)), 0o755))
	}
	status, err := Reuse(source, destination, "deps")
	require.NoError(t, err)
	assert.Equal(t, ReuseCopied, status)
}

func TestReuseRejectsUnsafeLinksAndCleansPartialCopy(t *testing.T) {
	tests := []struct {
		name string
		link string
	}{
		{"absolute", filepath.Join(string(filepath.Separator), "outside")},
		{"escape", "../../outside"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source, destination := t.TempDir(), t.TempDir()
			selected := filepath.Join(source, "deps")
			require.NoError(t, os.Mkdir(selected, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(selected, "before"), []byte("x"), 0o644))
			require.NoError(t, os.Symlink(test.link, filepath.Join(selected, "bad")))
			_, err := Reuse(source, destination, "deps")
			assert.Error(t, err)
			_, err = os.Lstat(filepath.Join(destination, "deps"))
			assert.True(t, errors.Is(err, os.ErrNotExist), "partial destination remains")
		})
	}
}

func TestReuseRejectsCyclesAndUnsupportedEntries(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	selected := filepath.Join(source, "deps")
	require.NoError(t, os.Mkdir(selected, 0o755))
	require.NoError(t, os.Symlink("second", filepath.Join(selected, "first")))
	require.NoError(t, os.Symlink("first", filepath.Join(selected, "second")))
	_, err := Reuse(source, destination, "deps")
	assert.Error(t, err)

	source, destination = t.TempDir(), t.TempDir()
	selected = filepath.Join(source, "deps")
	require.NoError(t, os.Mkdir(selected, 0o755))
	fifo := filepath.Join(selected, "fifo")
	require.NoError(t, mkfifo(fifo))
	_, err = Reuse(source, destination, "deps")
	assert.Error(t, err)
	_, err = os.Lstat(filepath.Join(destination, "deps"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestReuseFailureRemovesWholeAttemptButNotPreexistingRootFiles(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	selected := filepath.Join(source, "deps")
	require.NoError(t, os.MkdirAll(filepath.Join(selected, "a-dir", "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(selected, "a-dir", "nested", "file"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(selected, "b-file"), []byte("y"), 0o644))
	require.NoError(t, unix.Mkfifo(filepath.Join(selected, "z-fifo"), 0o644))
	untouched := filepath.Join(destination, "untouched")
	require.NoError(t, os.WriteFile(untouched, []byte("keep"), 0o644))
	_, err := Reuse(source, destination, "deps")
	require.Error(t, err)
	_, err = os.Lstat(filepath.Join(destination, "deps"))
	assert.ErrorIs(t, err, os.ErrNotExist)
	contents, err := os.ReadFile(untouched)
	require.NoError(t, err)
	assert.Equal(t, "keep", string(contents))
}

func mkfifo(path string) error {
	return unix.Mkfifo(path, 0o644)
}

func TestCleanupAfterCopyPreservesUnrelatedFiles(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(source, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(source, "nested", "file"), []byte("copied"), 0o644))
	require.NoError(t, os.Chmod(filepath.Join(source, "nested"), 0o555))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(source, "nested"), 0o755) })
	require.NoError(t, os.Mkdir(filepath.Join(destination, "deps"), 0o700))
	require.NoError(t, os.Mkdir(filepath.Join(destination, "nested"), 0o755))
	untouched := filepath.Join(destination, "nested", "file")
	require.NoError(t, os.WriteFile(untouched, []byte("keep"), 0o644))
	src, err := openRoot(source)
	require.NoError(t, err)
	defer src.Close()
	root, err := openRoot(destination)
	require.NoError(t, err)
	defer root.Close()
	dst, err := openDir(root.Fd(), "deps")
	require.NoError(t, err)
	defer dst.Close()
	state := newCleanupState(root, "deps")
	var st unix.Stat_t
	require.NoError(t, unix.Fstat(int(dst.Fd()), &st))
	state.addPath("deps", true, uint64(st.Dev), uint64(st.Ino))
	require.NoError(t, copyTree(int(src.Fd()), int(dst.Fd()), "", state))
	_, err = os.Stat(filepath.Join(destination, "deps", "nested", "file"))
	require.NoError(t, err, "exercise cleanup after copying, not pre-validation")
	cause := errors.New("injected failure after copy")
	require.Equal(t, cause, state.fail(cause))
	_, err = os.Lstat(filepath.Join(destination, "deps"))
	require.ErrorIs(t, err, os.ErrNotExist)
	contents, err := os.ReadFile(untouched)
	require.NoError(t, err)
	assert.Equal(t, "keep", string(contents))
}

func TestReuseRejectsSymlinkAncestors(t *testing.T) {
	for _, side := range []string{"source", "destination"} {
		t.Run(side, func(t *testing.T) {
			source, destination, outside := t.TempDir(), t.TempDir(), t.TempDir()
			require.NoError(t, os.Mkdir(filepath.Join(outside, "deps"), 0o755))
			if side == "source" {
				require.NoError(t, os.Symlink(outside, filepath.Join(source, "packages")))
			} else {
				require.NoError(t, os.MkdirAll(filepath.Join(source, "packages", "deps"), 0o755))
				require.NoError(t, os.Symlink(outside, filepath.Join(destination, "packages")))
			}
			_, err := Reuse(source, destination, "packages/deps")
			require.Error(t, err)
			entries, err := os.ReadDir(filepath.Join(outside, "deps"))
			require.NoError(t, err)
			assert.Empty(t, entries)
		})
	}
}

func TestReuseRejectsEscapeThroughContainedAlias(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(source, "deps"), 0o755))
	require.NoError(t, os.Symlink(".", filepath.Join(source, "deps", "alias")))
	require.NoError(t, os.Symlink("alias/../outside", filepath.Join(source, "deps", "escape")))
	_, err := Reuse(source, destination, "deps")
	require.ErrorContains(t, err, "escapes selected directory")
}

// snapshotTree records every entry below root with its mode, contents and
// link target so tests can assert that nothing was modified.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	require.NoError(t, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		desc := info.Mode().String()
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			desc += " -> " + link
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			desc += " " + string(data)
		}
		var st unix.Stat_t
		if err := unix.Lstat(path, &st); err != nil {
			return err
		}
		out[rel] = fmt.Sprintf("%s ino=%d", desc, st.Ino)
		return nil
	}))
	return out
}

func TestReuseDropsSpecialPermissionBits(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	selected := filepath.Join(source, "deps")
	require.NoError(t, os.Mkdir(selected, 0o755))
	file := filepath.Join(selected, "setuid-tool")
	require.NoError(t, os.WriteFile(file, []byte("#!/bin/sh\n"), 0o755))
	if err := unix.Chmod(file, 0o4755); err != nil {
		t.Skipf("cannot set setuid bit: %v", err)
	}
	var st unix.Stat_t
	require.NoError(t, unix.Stat(file, &st))
	if st.Mode&unix.S_ISUID == 0 {
		t.Skip("filesystem does not keep the setuid bit")
	}
	sticky := filepath.Join(selected, "sticky")
	require.NoError(t, os.Mkdir(sticky, 0o755))
	haveSticky := unix.Chmod(sticky, 0o1777) == nil
	if haveSticky {
		require.NoError(t, unix.Stat(sticky, &st))
		haveSticky = st.Mode&unix.S_ISVTX != 0
	}
	setgid := filepath.Join(selected, "setgid-tool")
	require.NoError(t, os.WriteFile(setgid, []byte("x"), 0o755))
	haveSetgid := unix.Chmod(setgid, 0o2755) == nil
	if haveSetgid {
		require.NoError(t, unix.Stat(setgid, &st))
		haveSetgid = st.Mode&unix.S_ISGID != 0
	}

	status, err := Reuse(source, destination, "deps")
	require.NoError(t, err)
	require.Equal(t, ReuseCopied, status)

	var copied unix.Stat_t
	require.NoError(t, unix.Stat(filepath.Join(destination, "deps", "setuid-tool"), &copied))
	assert.Equal(t, uint32(0o755), copied.Mode&0o7777, "setuid bit must not be copied")
	if haveSetgid {
		require.NoError(t, unix.Stat(filepath.Join(destination, "deps", "setgid-tool"), &copied))
		assert.Equal(t, uint32(0o755), copied.Mode&0o7777, "setgid bit must not be copied")
	}
	if haveSticky {
		require.NoError(t, unix.Stat(filepath.Join(destination, "deps", "sticky"), &copied))
		assert.Equal(t, uint32(0o777), copied.Mode&0o7777, "sticky bit must not be copied")
	}
}

func TestReuseDropsSpecialBitsOnSelectedRoot(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	selected := filepath.Join(source, "deps")
	require.NoError(t, os.Mkdir(selected, 0o755))
	if err := unix.Chmod(selected, 0o1777); err != nil {
		t.Skipf("cannot set sticky bit: %v", err)
	}
	var st unix.Stat_t
	require.NoError(t, unix.Stat(selected, &st))
	if st.Mode&unix.S_ISVTX == 0 {
		t.Skip("filesystem does not keep the sticky bit")
	}
	status, err := Reuse(source, destination, "deps")
	require.NoError(t, err)
	require.Equal(t, ReuseCopied, status)
	require.NoError(t, unix.Stat(filepath.Join(destination, "deps"), &st))
	assert.Equal(t, uint32(0o777), st.Mode&0o7777)
}

func TestReuseExistingDestinationIsNeverModified(t *testing.T) {
	setups := map[string]func(t *testing.T, dst string){
		"directory with content": func(t *testing.T, dst string) {
			require.NoError(t, os.MkdirAll(filepath.Join(dst, "deps", "keep"), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(dst, "deps", "keep", "file"), []byte("branch"), 0o600))
		},
		"empty directory": func(t *testing.T, dst string) {
			require.NoError(t, os.Mkdir(filepath.Join(dst, "deps"), 0o711))
		},
		"regular file": func(t *testing.T, dst string) {
			require.NoError(t, os.WriteFile(filepath.Join(dst, "deps"), []byte("file"), 0o644))
		},
		"dangling symlink": func(t *testing.T, dst string) {
			require.NoError(t, os.Symlink("nowhere", filepath.Join(dst, "deps")))
		},
	}
	for name, setup := range setups {
		t.Run(name, func(t *testing.T) {
			source, destination := t.TempDir(), t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(source, "deps", "sub"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(source, "deps", "sub", "file"), []byte("main"), 0o644))
			setup(t, destination)
			before := snapshotTree(t, destination)
			status, err := Reuse(source, destination, "deps")
			require.NoError(t, err)
			assert.Equal(t, ReuseDestinationExists, status)
			assert.Equal(t, before, snapshotTree(t, destination))
			_, err = os.Lstat(filepath.Join(source, "deps", "sub", "file"))
			require.NoError(t, err)
		})
	}
}

func TestReuseExistingNestedDestinationKeepsParents(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(source, "packages", "api", "node_modules"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(destination, "packages", "api", "node_modules"), 0o755))
	before := snapshotTree(t, destination)
	status, err := Reuse(source, destination, "packages/api/node_modules")
	require.NoError(t, err)
	assert.Equal(t, ReuseDestinationExists, status)
	assert.Equal(t, before, snapshotTree(t, destination))
}

func TestReuseFailureKeepsPreexistingParentAndRemovesCreatedParents(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	selected := filepath.Join(source, "packages", "api", "node_modules")
	require.NoError(t, os.MkdirAll(selected, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(selected, "a-file"), []byte("x"), 0o644))
	require.NoError(t, unix.Mkfifo(filepath.Join(selected, "z-fifo"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(destination, "packages"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(destination, "packages", "keep"), []byte("keep"), 0o644))
	before := snapshotTree(t, destination)

	_, err := Reuse(source, destination, "packages/api/node_modules")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "cleanup")
	_, err = os.Lstat(filepath.Join(destination, "packages", "api"))
	assert.ErrorIs(t, err, os.ErrNotExist, "parent created by this attempt must be removed")
	assert.Equal(t, before, snapshotTree(t, destination), "pre-existing parent must remain untouched")
}

func TestReuseFailureRemovesAllParentsItCreated(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	selected := filepath.Join(source, "packages", "api", "node_modules")
	require.NoError(t, os.MkdirAll(selected, 0o755))
	require.NoError(t, unix.Mkfifo(filepath.Join(selected, "fifo"), 0o644))
	_, err := Reuse(source, destination, "packages/api/node_modules")
	require.Error(t, err)
	entries, err := os.ReadDir(destination)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestReuseDestinationExistsDiscardsOnlyParentsItCreated(t *testing.T) {
	// The destination entry exists (so reuse is skipped) and the walk created
	// no parents: nothing must be removed or added.
	source, destination := t.TempDir(), t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(source, "a", "b"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(destination, "a"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(destination, "a", "b"), []byte("file"), 0o644))
	before := snapshotTree(t, destination)
	status, err := Reuse(source, destination, "a/b")
	require.NoError(t, err)
	assert.Equal(t, ReuseDestinationExists, status)
	assert.Equal(t, before, snapshotTree(t, destination))
}

func TestDiscardMadeSkipsParentReplacedByAnotherWriter(t *testing.T) {
	destination := t.TempDir()
	root, err := openRoot(destination)
	require.NoError(t, err)
	defer root.Close()
	parent, made, err := openOrCreateDir(int(root.Fd()), filepath.Join("packages", "api"))
	require.NoError(t, err)
	parent.Close()
	require.Len(t, made, 2)

	// Simulate another writer: our "packages/api" moves away and a different
	// directory takes its name. Create the replacement before removing ours
	// so its inode cannot be reused.
	require.NoError(t, os.Rename(filepath.Join(destination, "packages", "api"), filepath.Join(destination, "ours")))
	require.NoError(t, os.Mkdir(filepath.Join(destination, "packages", "api"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(destination, "packages", "api", "theirs"), []byte("x"), 0o644))

	_ = discardMade(int(root.Fd()), made)
	contents, err := os.ReadFile(filepath.Join(destination, "packages", "api", "theirs"))
	require.NoError(t, err, "a parent this attempt did not create must never be removed")
	assert.Equal(t, "x", string(contents))
}

func TestOpenOrCreateDirDoesNotClaimExistingParents(t *testing.T) {
	destination := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(destination, "packages"), 0o755))
	root, err := openRoot(destination)
	require.NoError(t, err)
	defer root.Close()
	parent, made, err := openOrCreateDir(int(root.Fd()), filepath.Join("packages", "api"))
	require.NoError(t, err)
	parent.Close()
	require.Len(t, made, 1)
	assert.Equal(t, filepath.Join("packages", "api"), made[0].path)
	require.NoError(t, discardMade(int(root.Fd()), made))
	_, err = os.Stat(filepath.Join(destination, "packages"))
	require.NoError(t, err)
	_, err = os.Lstat(filepath.Join(destination, "packages", "api"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestCleanupReportsParentThatCannotBeReopened(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(source, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(source, "nested", "file"), []byte("copied"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(destination, "deps"), 0o700))
	src, err := openRoot(source)
	require.NoError(t, err)
	defer src.Close()
	root, err := openRoot(destination)
	require.NoError(t, err)
	defer root.Close()
	dst, err := openDir(root.Fd(), "deps")
	require.NoError(t, err)
	defer dst.Close()
	state := newCleanupState(root, "deps")
	var st unix.Stat_t
	require.NoError(t, unix.Fstat(int(dst.Fd()), &st))
	state.addPath("deps", true, uint64(st.Dev), uint64(st.Ino))
	require.NoError(t, copyTree(int(src.Fd()), int(dst.Fd()), "", state))

	// Another writer swaps the copied tree away and puts a new directory in
	// its place. Cleanup can no longer reopen the recorded parents and must
	// report that rather than removing anything it does not own.
	require.NoError(t, os.Rename(filepath.Join(destination, "deps"), filepath.Join(destination, "moved")))
	require.NoError(t, os.MkdirAll(filepath.Join(destination, "deps", "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(destination, "deps", "nested", "file"), []byte("theirs"), 0o644))
	before := snapshotTree(t, destination)

	cause := errors.New("injected failure after copy")
	err = state.fail(cause)
	require.ErrorIs(t, err, cause)
	assert.ErrorContains(t, err, "cleanup")
	assert.ErrorContains(t, err, "cleanup ancestor changed")
	assert.Equal(t, before, snapshotTree(t, destination))
}

func swapBeforeParentMkdir(t *testing.T, hook func(dirFd int, name string)) {
	t.Helper()
	saved := beforeParentMkdir
	t.Cleanup(func() { beforeParentMkdir = saved })
	beforeParentMkdir = hook
}

// raceParent makes another "process" create the named parent in the window
// between the missing-parent open and reuse's own Mkdirat.
func raceParent(t *testing.T, target string) *[]string {
	t.Helper()
	var raced []string
	swapBeforeParentMkdir(t, func(dirFd int, name string) {
		if name == target {
			require.NoError(t, unix.Mkdirat(dirFd, name, 0o755))
			raced = append(raced, name)
		}
	})
	return &raced
}

func TestOpenOrCreateDirDoesNotRecordConcurrentlyCreatedParent(t *testing.T) {
	destination := t.TempDir()
	raced := raceParent(t, "packages")
	root, err := openRoot(destination)
	require.NoError(t, err)
	defer root.Close()
	parent, made, err := openOrCreateDir(int(root.Fd()), filepath.Join("packages", "api"))
	require.NoError(t, err)
	parent.Close()
	assert.Equal(t, []string{"packages"}, *raced)
	require.Len(t, made, 1)
	assert.Equal(t, filepath.Join("packages", "api"), made[0].path)
}

func TestReuseUsesConcurrentlyCreatedParent(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	selected := filepath.Join(source, "packages", "node_modules")
	require.NoError(t, os.MkdirAll(selected, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(selected, "file"), []byte("main"), 0o644))
	raced := raceParent(t, "packages")

	status, err := Reuse(source, destination, "packages/node_modules")
	require.NoError(t, err)
	assert.Equal(t, ReuseCopied, status)
	assert.Equal(t, []string{"packages"}, *raced, "hook must have simulated the race")
	contents, err := os.ReadFile(filepath.Join(destination, "packages", "node_modules", "file"))
	require.NoError(t, err)
	assert.Equal(t, "main", string(contents))
}

func TestReuseFailureKeepsConcurrentlyCreatedParent(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	selected := filepath.Join(source, "packages", "api", "node_modules")
	require.NoError(t, os.MkdirAll(selected, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(selected, "file"), []byte("x"), 0o644))
	require.NoError(t, os.Symlink("../../../../outside", filepath.Join(selected, "escape")))
	// "packages" is created by the racing writer; "packages/api" by reuse.
	raced := raceParent(t, "packages")

	_, err := Reuse(source, destination, "packages/api/node_modules")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "cleanup")
	assert.Equal(t, []string{"packages"}, *raced, "hook must have simulated the race")
	info, statErr := os.Stat(filepath.Join(destination, "packages"))
	require.NoError(t, statErr, "a parent created by another process must never be removed")
	assert.True(t, info.IsDir())
	_, statErr = os.Lstat(filepath.Join(destination, "packages", "api"))
	assert.ErrorIs(t, statErr, os.ErrNotExist, "the parent this attempt created must be removed")
}

func TestCleanupReportsOwnershipFailureOnce(t *testing.T) {
	t.Run("opened entry", func(t *testing.T) {
		destination := t.TempDir()
		root, err := openRoot(destination)
		require.NoError(t, err)
		defer root.Close()
		state := newCleanupState(root, "deps")
		// An invalid descriptor makes recording ownership fail deterministically.
		ownErr := state.addOpened("", "entry", false, -1)
		require.Error(t, ownErr)
		err = state.fail(ownErr)
		require.Error(t, err)
		assert.Equal(t, 1, strings.Count(err.Error(), "cleanup ownership for"), err.Error())
		assert.Equal(t, 1, strings.Count(err.Error(), unix.EBADF.Error()), err.Error())
	})
	t.Run("symlink entry", func(t *testing.T) {
		destination := t.TempDir()
		root, err := openRoot(destination)
		require.NoError(t, err)
		defer root.Close()
		state := newCleanupState(root, "deps")
		ownErr := state.add("", "link", -1)
		require.Error(t, ownErr)
		err = state.fail(ownErr)
		require.Error(t, err)
		assert.Equal(t, 1, strings.Count(err.Error(), unix.EBADF.Error()), err.Error())
	})
}
