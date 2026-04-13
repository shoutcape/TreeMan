// Package queue manages the async delete queue for treeman.
//
// Pending deletions are stored in a JSON file so they survive process exit
// and can be drained at the start of the next treeman command.
package queue

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const fileName = "delete-queue.json"

// Entry is a single pending deletion.
type Entry struct {
	Path     string    `json:"path"`
	Branch   string    `json:"branch"`
	RepoRoot string    `json:"repoRoot"`
	QueuedAt time.Time `json:"queuedAt"`
}

// DataDir returns the treeman data directory, respecting $XDG_DATA_HOME.
// Falls back to ~/.local/share/treeman.
func DataDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "treeman")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "treeman")
}

// queuePath returns the full path to the queue file.
func queuePath() string {
	dir := DataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, fileName)
}

// readAll reads all entries from the queue file.
// Returns nil, nil if the file does not exist.
func readAll() ([]Entry, error) {
	path := queuePath()
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// writeAll writes entries to the queue file, creating parent dirs if needed.
// If entries is empty, the file is removed.
func writeAll(entries []Entry) error {
	path := queuePath()
	if path == "" {
		return nil
	}
	if len(entries) == 0 {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Enqueue appends an entry to the queue file.
// Creates the file and parent dirs if they do not exist.
// If the queue file is malformed JSON, returns an error rather than
// overwriting it (preserves the file per spec).
func Enqueue(e Entry) error {
	entries, err := readAll()
	if err != nil {
		return fmt.Errorf("could not read queue (fix or remove %s): %w", queuePath(), err)
	}
	entries = append(entries, e)
	return writeAll(entries)
}

// Peek returns all entries without modifying the queue.
// Returns nil, nil if the queue is empty or absent.
func Peek() ([]Entry, error) {
	return readAll()
}

// Drain reads all entries and calls fn for each one. Entries where fn
// returns nil are removed. Entries where fn returns an error are retained
// for retry on the next drain. Returns the number of failed entries and
// any file I/O error.
func Drain(fn func(Entry) error) (int, error) {
	entries, err := readAll()
	if err != nil {
		return 0, err
	}
	if len(entries) == 0 {
		return 0, nil
	}

	var remaining []Entry
	failCount := 0
	for _, e := range entries {
		if err := fn(e); err != nil {
			remaining = append(remaining, e)
			failCount++
		}
	}

	if err := writeAll(remaining); err != nil {
		return failCount, err
	}
	return failCount, nil
}
