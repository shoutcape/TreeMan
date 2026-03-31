package database

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

		k, value, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}

		if k == key {
			return stripQuotes(value), nil
		}
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

		k, _, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}

		if k == key {
			lines[i] = key + "=" + newValue
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("%s not found in %s", key, envPath)
	}

	output := strings.Join(lines, "\n")
	if err := os.WriteFile(envPath, []byte(output), 0600); err != nil {
		return fmt.Errorf("writing %s: %w", envPath, err)
	}

	return nil
}

// ReadDatabaseURI is a convenience wrapper for backward compatibility.
// It reads the value of the given envKey from the .env file.
// If envKey is empty, it returns "" (database management not configured).
func ReadDatabaseURI(dir, envKey string) (string, error) {
	if envKey == "" {
		return "", nil
	}
	return ReadEnvValue(dir, envKey)
}

// RewriteDatabaseURI is a convenience wrapper for backward compatibility.
// It rewrites the value of the given envKey in the .env file.
func RewriteDatabaseURI(dir, envKey, newURI string) error {
	return RewriteEnvValue(dir, envKey, newURI)
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
