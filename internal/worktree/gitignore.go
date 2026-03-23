package worktree

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

const worktreesIgnoreEntry = ".worktrees/"

// EnsureIgnored appends ".worktrees/" to <mainRoot>/.gitignore if the entry
// is not already present. It creates .gitignore if it does not exist.
// Callers should treat any returned error as non-fatal.
func EnsureIgnored(mainRoot string) error {
	gitignorePath := filepath.Join(mainRoot, ".gitignore")

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
		if strings.TrimSpace(line) == worktreesIgnoreEntry {
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
	_, err = out.WriteString(prefix + worktreesIgnoreEntry + "\n")
	return err
}
