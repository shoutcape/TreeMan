package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"syscall"

	"github.com/BurntSushi/toml"
	"github.com/shoutcape/treeman/internal/deps"
	"github.com/shoutcape/treeman/internal/fsutil"
)

// replaceFile is a variable so tests can change the config between the
// writer's read and its final recheck.
var replaceFile = fsutil.AtomicReplaceFile

var depsTable = regexp.MustCompile(`^\s*\[\s*(?:deps|"deps"|'deps')\s*\]\s*(?:#.*)?$`)

// EnableWarmStarts adds node_modules to an explicitly named .treeman.toml.
// It deliberately edits by insertion rather than marshaling, so comments,
// formatting, unknown keys, and mode-specific settings remain untouched.
func EnableWarmStarts(path string) error {
	// Callers serialize automatic opt-in before reaching this API. Do not create
	// a lock beside the project config: that would pollute the repository.
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return createWarmStartConfig(path)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("config %q is not a regular file", path)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Nlink > 1 {
		return fmt.Errorf("config %q has multiple hard links", path)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := validateConfigBytes(original); err != nil {
		return fmt.Errorf("refusing invalid config: %w", err)
	}
	updated, err := warmStartBytes(original)
	if err != nil {
		return err
	}
	if updated == nil {
		return errors.New("[deps].reuse is already declared")
	}
	if err := validateConfigBytes(updated); err != nil {
		return fmt.Errorf("refusing unsafe config insertion: %w", err)
	}

	// Recheck after the replacement is written and synced, right before the
	// rename. An editor saving in the remaining instant can still lose its
	// write; editors take no lock TreeMan could share.
	return replaceFile(path, updated, info.Mode().Perm(), func() error {
		currentInfo, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("recheck config: %w", err)
		}
		if !sameFileMetadata(info, currentInfo) {
			return errors.New("config changed while preparing warm-start configuration")
		}
		current, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(original, current) {
			return errors.New("config changed while preparing warm-start configuration")
		}
		return nil
	})
}

func createWarmStartConfig(path string) error {
	data := []byte("[deps]\nreuse = [\"node_modules\"]\n")
	f, err := createUmaskTemp(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("create config: %w", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// Publish complete contents without replacing a concurrently created file.
	// The temporary name is removed before returning; no shared config remains.
	if err := os.Link(f.Name(), path); err != nil {
		return fmt.Errorf("publish config: %w", err)
	}
	return fsutil.SyncDirectory(filepath.Dir(path))
}

// createUmaskTemp creates an exclusive temporary file with mode 0666 minus
// the umask, so a published config gets the same mode as any new project file.
// os.CreateTemp always uses 0600.
func createUmaskTemp(dir string) (*os.File, error) {
	for range 100 {
		var suffix [8]byte
		if _, err := rand.Read(suffix[:]); err != nil {
			return nil, err
		}
		name := filepath.Join(dir, ".treeman-config-"+hex.EncodeToString(suffix[:]))
		f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		return f, err
	}
	return nil, errors.New("could not choose a temporary config name")
}

func validateConfigBytes(data []byte) error {
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return err
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return err
	}
	if cfg.Database != nil && cfg.Database.EnvKey == "" {
		return errors.New("[database] section requires env_key")
	}
	if cfg.Database != nil && cfg.Database.Container != "" && strings.TrimSpace(cfg.Database.Container) == "" {
		return errors.New("[database].container cannot be whitespace")
	}
	return deps.ValidateReusePaths(cfg.ReusePaths())
}

func warmStartBytes(original []byte) ([]byte, error) {
	var raw map[string]any
	if _, err := toml.Decode(string(original), &raw); err != nil {
		return nil, fmt.Errorf("refusing invalid config: %w", err)
	}
	if value, ok := raw["deps"]; ok {
		section, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("cannot safely extend deps configuration")
		}
		if _, declared := section["reuse"]; declared {
			return nil, nil
		}
	}

	type insertionPoint struct {
		position int
		root     bool
	}
	var positions []insertionPoint
	lines := strings.SplitAfter(string(original), "\n")
	offset := 0
	for _, line := range lines {
		trimmed := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if depsTable.MatchString(trimmed) {
			positions = append(positions, insertionPoint{position: offset + len(line)})
		}
		offset += len(line)
	}
	// Extend a real [deps] table first. If deps already exists in another
	// syntax (such as an inline table), refuse rather than rely on permissive
	// decoder behavior that other TOML readers may reject.
	if _, exists := raw["deps"]; !exists {
		positions = append(positions, insertionPoint{position: rootInsertion(original), root: true})
		positions = append(positions, insertionPoint{position: 0, root: true})
	}
	for _, point := range positions {
		position := point.position
		insertion := "reuse = [\"node_modules\"]\n"
		if !point.root {
			insertion = lineEnding(original, insertion)
		} else {
			insertion = lineEnding(original, "deps.reuse = [\"node_modules\"]\n")
		}
		if position > 0 && original[position-1] != '\n' {
			insertion = newline(original) + insertion
		}
		candidate := insertAt(original, position, insertion)
		if candidateConfigMatches(original, candidate) {
			return candidate, nil
		}
	}
	return nil, errors.New("cannot safely insert [deps].reuse without rewriting the config")
}

func newline(data []byte) string {
	if bytes.Contains(data, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

func lineEnding(data []byte, line string) string {
	return strings.ReplaceAll(line, "\n", newline(data))
}

func rootInsertion(data []byte) int {
	offset := 0
	for _, line := range strings.SplitAfter(string(data), "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"))
		if strings.HasPrefix(trimmed, "[") {
			return offset
		}
		offset += len(line)
	}
	return len(data)
}

func candidateConfigMatches(original, candidate []byte) bool {
	var before, after map[string]any
	if _, err := toml.Decode(string(original), &before); err != nil {
		return false
	}
	if _, err := toml.Decode(string(candidate), &after); err != nil {
		return false
	}
	var cfg Config
	if err := toml.Unmarshal(candidate, &cfg); err != nil || !reflect.DeepEqual(cfg.ReusePaths(), []string{"node_modules"}) {
		return false
	}
	section, ok := after["deps"].(map[string]any)
	if !ok {
		return false
	}
	delete(section, "reuse")
	if len(section) == 0 {
		if _, existed := before["deps"]; existed {
			return reflect.DeepEqual(before, after)
		}
		delete(after, "deps")
	}
	return reflect.DeepEqual(before, after)
}

func insertAt(data []byte, position int, insertion string) []byte {
	result := make([]byte, 0, len(data)+len(insertion))
	result = append(result, data[:position]...)
	result = append(result, insertion...)
	result = append(result, data[position:]...)
	return result
}

// sameFileMetadata reports whether a and b are the same file with unchanged
// size and modification time.
func sameFileMetadata(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime() == b.ModTime()
}
