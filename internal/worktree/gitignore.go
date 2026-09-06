package worktree

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shoutcape/treeman/internal/fsutil"
)

// defaultIgnoreEntry is the entry written for the default worktree directory.
// It is kept exactly as it has always been so that repositories which already
// carry it do not gain a second, differently spelled line.
const defaultIgnoreEntry = ".worktrees/"

// EnsureIgnored appends the worktree parent directory to <mainRoot>/.gitignore
// if the entry is not already present. It creates .gitignore if it does not
// exist. Callers should treat any returned error as non-fatal.
//
// A worktreeDir outside the repository is left alone: a .gitignore entry is
// repository-relative, so there is nothing to write for a directory Git will
// never look at anyway.
func EnsureIgnored(mainRoot, worktreeDir string) error {
	entry, inside, err := IgnoreEntry(mainRoot, worktreeDir)
	if err != nil {
		return err
	}
	if !inside {
		return nil
	}
	return appendIgnoreEntry(filepath.Join(mainRoot, ".gitignore"), entry)
}

// IgnoreEntry returns the .gitignore line that hides worktreeDir from the
// repository at mainRoot. inside is false when the directory is not inside the
// repository, in which case there is no entry to write.
func IgnoreEntry(mainRoot, worktreeDir string) (entry string, inside bool, err error) {
	canonicalRoot, err := fsutil.CanonicalPath(mainRoot)
	if err != nil {
		return "", false, fmt.Errorf("cannot resolve the main worktree root %q: %w", mainRoot, err)
	}
	canonicalDir, err := fsutil.CanonicalPath(worktreeDir)
	if err != nil {
		return "", false, fmt.Errorf("cannot resolve the worktree directory %q: %w", worktreeDir, err)
	}
	if !fsutil.Contains(canonicalRoot, canonicalDir) {
		return "", false, nil
	}

	relative, err := filepath.Rel(canonicalRoot, canonicalDir)
	if err != nil {
		return "", false, fmt.Errorf("cannot place %q inside %q: %w", worktreeDir, mainRoot, err)
	}
	relative = filepath.ToSlash(relative)
	if relative == DefaultDir {
		return defaultIgnoreEntry, true, nil
	}
	return "/" + escapeIgnorePattern(relative) + "/", true, nil
}

// escapeIgnorePattern quotes the characters Git reads as pattern syntax so a
// directory whose name contains them is matched literally. Spaces are escaped
// as well, because Git strips unescaped ones from the end of a line and a
// directory name may end in one.
func escapeIgnorePattern(path string) string {
	var b strings.Builder
	for _, r := range path {
		switch r {
		case '\\', '*', '?', '[', ']', '!', '#', ' ':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func appendIgnoreEntry(gitignorePath, entry string) error {
	// Read existing lines (ignore not-exist; we'll create the file below).
	var lines []string
	f, err := os.Open(gitignorePath)
	if err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		f.Close()
		if scanErr := scanner.Err(); scanErr != nil {
			return scanErr
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	// Check if already present.
	for _, line := range lines {
		if line == entry || entry == defaultIgnoreEntry && strings.TrimSpace(line) == entry {
			return nil
		}
	}

	// Append the entry.
	out, err := os.OpenFile(gitignorePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer out.Close()

	// Ensure we start on a new line.
	prefix := ""
	if len(lines) > 0 && lines[len(lines)-1] != "" {
		prefix = "\n"
	}
	_, err = out.WriteString(prefix + entry + "\n")
	return err
}
