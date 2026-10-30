package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// currentUmask reads the process umask. Tests in this package do not run in
// parallel, so briefly clearing it is safe.
func currentUmask() os.FileMode {
	old := unix.Umask(0)
	unix.Umask(old)
	return os.FileMode(old)
}

func TestEnableWarmStartsNewFileModeFollowsUmask(t *testing.T) {
	for _, tc := range []struct {
		umask int
		want  os.FileMode
	}{
		{0o022, 0o644},
		{0o027, 0o640},
		{0o002, 0o664},
	} {
		t.Run(fmt.Sprintf("%04o", tc.umask), func(t *testing.T) {
			old := unix.Umask(tc.umask)
			defer unix.Umask(old)
			path := filepath.Join(t.TempDir(), ConfigFileName)
			require.NoError(t, EnableWarmStarts(path))
			info, err := os.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, tc.want, info.Mode().Perm())
		})
	}
}

func TestEnableWarmStartsKeepsExistingMode(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o644, 0o664, 0o400 | 0o200} {
		t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ConfigFileName)
			require.NoError(t, os.WriteFile(path, []byte("[deps]\n"), 0o600))
			require.NoError(t, os.Chmod(path, mode))
			require.NoError(t, EnableWarmStarts(path))
			info, err := os.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, mode, info.Mode().Perm())
		})
	}
}

func TestEnableWarmStarts(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"new file", "", "[deps]\nreuse = [\"node_modules\"]\n"},
		{"empty deps", "[deps]\n", "reuse = [\"node_modules\"]\n"},
		{"existing deps", "# keep\n[deps]\nmode = \"copy\"\n[other]\nx = 1\n", "reuse = [\"node_modules\"]\n"},
		{"quoted header", "[\"deps\"]\r\nmode = \"copy\"\r\n", "reuse = [\"node_modules\"]\r\n"},
		{"single quoted header", "['deps']\nmode = \"copy\"\n", "reuse = [\"node_modules\"]\n"},
		{"array table and comments", "# leading\n# comment\n[[plugins]]\ndescription = \"\"\"multi\nline\"\"\"\n", "deps.reuse = [\"node_modules\"]\n"},
		{"table at eof", "[deps]", "reuse = [\"node_modules\"]\n"},
		{"tables after", "name = \"x\"\n[database]\nenv_key = \"DB\"\n", "deps.reuse = [\"node_modules\"]\n"},
		{"multiline fake table", "text = '''\n[deps]\ndeps = {}\n'''\n[deps]\nmode = \"copy\"\n", "reuse = [\"node_modules\"]\n"},
		{"empty existing file", "\n", "deps.reuse = [\"node_modules\"]\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ConfigFileName)
			if tt.input != "" {
				require.NoError(t, os.WriteFile(path, []byte(tt.input), 0o640))
			}
			require.NoError(t, EnableWarmStarts(path))
			assert.NoFileExists(t, path+".lock")
			got, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Contains(t, string(got), tt.want)
			assert.Equal(t, 1, strings.Count(string(got), "node_modules"))
			assert.Empty(t, Load(filepath.Dir(path)).Warning)
			info, err := os.Stat(path)
			require.NoError(t, err)
			if tt.input != "" {
				assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
				withoutAddition := strings.Replace(string(got), tt.want, "", 1)
				assert.Equal(t, strings.TrimRight(tt.input, "\r\n"), strings.TrimRight(withoutAddition, "\r\n"), "preserve every unrelated byte")
			} else {
				assert.Equal(t, os.FileMode(0o666)&^currentUmask(), info.Mode().Perm())
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			require.NoError(t, err)
			assert.Len(t, entries, 1, "leave no lock or temporary file")
		})
	}
}

func TestEnableWarmStartsRefusesUnsafeOrDeclared(t *testing.T) {
	for _, input := range []string{
		"[deps]\nreuse = []\n",
		"deps = { mode = \"copy\" }\n",
		"deps.mode = \"copy\"\n",
		"[database]\n",
		"[deps]\nreuse = [\"node_modules\"]\n",
	} {
		path := filepath.Join(t.TempDir(), ConfigFileName)
		require.NoError(t, os.WriteFile(path, []byte(input), 0o600))
		before, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Error(t, EnableWarmStarts(path))
		after, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, before, after)
		assert.NoFileExists(t, path+".lock")
	}
}

func TestEnableWarmStartsRefusesSymlinkAndInvalid(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	path := filepath.Join(dir, ConfigFileName)
	require.NoError(t, os.WriteFile(target, []byte("x = 1\n"), 0o600))
	require.NoError(t, os.Symlink(target, path))
	assert.Error(t, EnableWarmStarts(path))

	require.NoError(t, os.Remove(path))
	require.NoError(t, os.WriteFile(path, []byte("[database\n"), 0o600))
	assert.Error(t, EnableWarmStarts(path))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "[database\n", string(got))
}

func TestEnableWarmStartsRefusesHardlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ConfigFileName)
	other := filepath.Join(dir, "other.toml")
	require.NoError(t, os.WriteFile(path, []byte("[deps]\n"), 0o600))
	require.NoError(t, os.Link(path, other))
	assert.Error(t, EnableWarmStarts(path))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "[deps]\n", string(got))
}

func TestEnableWarmStartsRefusesWhenConfigChangesBeforeRename(t *testing.T) {
	const original = "[deps]\nmode = \"copy\"\n"
	tests := map[string]func(t *testing.T, path string) []byte{
		"content change": func(t *testing.T, path string) []byte {
			changed := []byte("[deps]\nmode = \"copy\"\n# editor\n")
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
			require.NoError(t, err)
			_, err = f.Write(changed)
			require.NoError(t, err)
			require.NoError(t, f.Close())
			return changed
		},
		"same size and mtime content change": func(t *testing.T, path string) []byte {
			info, err := os.Stat(path)
			require.NoError(t, err)
			changed := []byte("[deps]\nmode = \"COPY\"\n")
			require.Len(t, changed, len(original))
			f, err := os.OpenFile(path, os.O_WRONLY, 0)
			require.NoError(t, err)
			_, err = f.WriteAt(changed, 0)
			require.NoError(t, err)
			require.NoError(t, f.Close())
			require.NoError(t, os.Chtimes(path, info.ModTime(), info.ModTime()))
			return changed
		},
		"replaced file with same bytes": func(t *testing.T, path string) []byte {
			replacement := path + ".editor"
			require.NoError(t, os.WriteFile(replacement, []byte(original), 0o640))
			require.NoError(t, os.Rename(replacement, path))
			return []byte(original)
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ConfigFileName)
			require.NoError(t, os.WriteFile(path, []byte(original), 0o640))
			before, err := os.Stat(path)
			require.NoError(t, err)

			saved := replaceFile
			t.Cleanup(func() { replaceFile = saved })
			var want []byte
			calls := 0
			replaceFile = func(p string, data []byte, mode os.FileMode, check func() error) error {
				calls++
				want = change(t, p)
				return saved(p, data, mode, check)
			}

			err = EnableWarmStarts(path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "config changed")
			assert.Equal(t, 1, calls)
			got, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			assert.Equal(t, string(want), string(got), "file must stay as the other writer left it")
			assert.NotContains(t, string(got), "node_modules")
			after, statErr := os.Stat(path)
			require.NoError(t, statErr)
			if name == "replaced file with same bytes" {
				assert.False(t, os.SameFile(before, after), "replacement inode must remain")
			} else {
				assert.True(t, os.SameFile(before, after), "edited inode must remain")
			}
			entries, dirErr := os.ReadDir(filepath.Dir(path))
			require.NoError(t, dirErr)
			assert.Len(t, entries, 1, "no temporary file may remain")
		})
	}
}

func TestEnableWarmStartsUsesReplaceFileWhenUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), ConfigFileName)
	require.NoError(t, os.WriteFile(path, []byte("[deps]\n"), 0o640))
	saved := replaceFile
	t.Cleanup(func() { replaceFile = saved })
	calls := 0
	replaceFile = func(p string, data []byte, mode os.FileMode, check func() error) error {
		calls++
		assert.Equal(t, os.FileMode(0o640), mode)
		return saved(p, data, mode, check)
	}
	require.NoError(t, EnableWarmStarts(path))
	assert.Equal(t, 1, calls)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(got), "reuse = [\"node_modules\"]")
}
