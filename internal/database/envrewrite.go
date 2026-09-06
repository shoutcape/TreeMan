package database

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shoutcape/treeman/internal/fsutil"
)

const envFileName = ".env"

// ReadEnvValue reads the .env file in dir and returns the value of the given key.
// Returns "" with no error if the file doesn't exist or the variable is not found.
// Handles double-quoted and single-quoted values by stripping the quotes.
func ReadEnvValue(dir, key string) (string, error) {
	envPath := filepath.Join(dir, envFileName)

	f, err := os.Open(envPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("opening %s: %w", envPath, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines and comments
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		assignment, ok := parseEnvAssignment(line)
		if !ok || assignment.key != key {
			continue
		}
		return stripQuotes(strings.TrimSpace(assignment.value)), nil
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("reading %s: %w", envPath, err)
	}

	return "", nil
}

// RewriteEnvValue reads the .env file in dir, replaces the line for the given
// key with the new value, and writes the file back. All other lines (including
// comments, blank lines, and other variables) are preserved exactly.
// Returns an error if the .env file doesn't exist or the key is not found.
func RewriteEnvValue(dir, key, newValue string) error {
	envPath := filepath.Join(dir, envFileName)

	info, err := os.Lstat(envPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", envPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to rewrite symlinked environment file %s", envPath)
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", envPath, err)
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip comments and empty lines
		if strings.HasPrefix(trimmed, "#") || trimmed == "" {
			continue
		}

		assignment, ok := parseEnvAssignment(line)
		if !ok || assignment.key != key {
			continue
		}
		value := newValue
		trimmedValue := strings.TrimSpace(assignment.value)
		if len(trimmedValue) >= 2 && ((trimmedValue[0] == '"' && trimmedValue[len(trimmedValue)-1] == '"') || (trimmedValue[0] == '\'' && trimmedValue[len(trimmedValue)-1] == '\'')) {
			value = string(trimmedValue[0]) + newValue + string(trimmedValue[0])
		}
		lines[i] = assignment.prefix + value + assignment.suffix
		found = true
		break
	}

	if !found {
		return fmt.Errorf("%s not found in %s", key, envPath)
	}

	output := strings.Join(lines, "\n")
	if err := fsutil.AtomicWriteFile(envPath, []byte(output), info.Mode().Perm()); err != nil {
		return fmt.Errorf("writing %s: %w", envPath, err)
	}

	return nil
}

type envAssignment struct {
	key    string
	prefix string
	value  string
	suffix string
}

// parseEnvAssignment retains the spelling around an assignment while exposing
// its key. It accepts the common optional export prefix used by shell dotenvs.
func parseEnvAssignment(line string) (envAssignment, bool) {
	equals := strings.IndexByte(line, '=')
	if equals < 0 {
		return envAssignment{}, false
	}
	left := line[:equals]
	keyPart := strings.TrimSpace(left)
	if strings.HasPrefix(keyPart, "export ") {
		keyPart = strings.TrimSpace(strings.TrimPrefix(keyPart, "export "))
	}
	if keyPart == "" {
		return envAssignment{}, false
	}
	valueStart := equals + 1
	for valueStart < len(line) && (line[valueStart] == ' ' || line[valueStart] == '\t') {
		valueStart++
	}
	value := line[valueStart:]
	trailing := len(value)
	for trailing > 0 && (value[trailing-1] == ' ' || value[trailing-1] == '\t') {
		trailing--
	}
	return envAssignment{key: keyPart, prefix: line[:valueStart], value: value[:trailing], suffix: value[trailing:]}, true
}

// stripQuotes removes surrounding double or single quotes from a value.
func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
