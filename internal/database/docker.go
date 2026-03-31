package database

import (
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

// FindPostgresContainer discovers a running PostgreSQL container via docker ps.
// If a port is provided, it tries to match a container exposing that port.
// Otherwise it falls back to scanning for any postgres container.
func FindPostgresContainer(port string) (string, error) {
	// If we have a port, try to find a container publishing that specific port.
	if port != "" {
		out, err := exec.Command("docker", "ps",
			"--filter", "publish="+port,
			"--format", "{{.Names}}\t{{.Image}}",
		).CombinedOutput()
		if err == nil {
			name := findPostgresInOutput(string(out))
			if name != "" {
				return name, nil
			}
		}
	}

	// Fallback: try ancestor filter.
	out, err := exec.Command("docker", "ps", "--filter", "ancestor=postgres", "--format", "{{.Names}}").CombinedOutput()
	if err == nil {
		name := parseContainerName(string(out))
		if name != "" {
			return name, nil
		}
	}

	// Final fallback: scan all containers for postgres in image name.
	out, err = exec.Command("docker", "ps", "--format", "{{.Names}}\t{{.Image}}").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker ps failed: %w", err)
	}

	name := findPostgresInOutput(string(out))
	if name == "" {
		return "", fmt.Errorf("no running PostgreSQL container found")
	}
	return name, nil
}

// CreateDatabase creates a new database in the given PostgreSQL container.
// If the database already exists, it returns nil.
// The user is extracted from the baseURI and psql connects locally inside
// the container (avoiding network address ambiguity).
func CreateDatabase(container, baseURI, dbName string) error {
	args := buildPsqlArgs(container, baseURI, dbName)
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "already exists") {
			return nil
		}
		return fmt.Errorf("create database %q failed: %s", dbName, strings.TrimSpace(string(out)))
	}
	return nil
}

// DropDatabase drops a database in the given PostgreSQL container.
func DropDatabase(container, baseURI, dbName string) error {
	args := buildDropArgs(container, baseURI, dbName)
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("drop database %q failed: %s", dbName, strings.TrimSpace(string(out)))
	}
	return nil
}

// extractUser extracts the username from a postgres URI.
// Falls back to "postgres" if parsing fails.
func extractUser(baseURI string) string {
	parsed, err := url.Parse(baseURI)
	if err != nil || parsed.User == nil {
		return "postgres"
	}
	user := parsed.User.Username()
	if user == "" {
		return "postgres"
	}
	return user
}

// buildPsqlArgs constructs the docker exec arguments for CREATE DATABASE.
// It uses -U <user> to connect locally inside the container instead of
// passing the full URI (which would have incorrect network addresses).
func buildPsqlArgs(container, baseURI, dbName string) []string {
	user := extractUser(baseURI)
	return []string{
		"exec", container,
		"psql", "-U", user,
		"-c", fmt.Sprintf(`CREATE DATABASE "%s"`, dbName),
	}
}

// buildDropArgs constructs the docker exec arguments for DROP DATABASE IF EXISTS.
func buildDropArgs(container, baseURI, dbName string) []string {
	user := extractUser(baseURI)
	return []string{
		"exec", container,
		"psql", "-U", user,
		"-c", fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, dbName),
	}
}

// parseContainerName extracts the first non-empty line from docker ps output.
func parseContainerName(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// findPostgresInOutput scans tab-separated "name\timage" lines for a postgres image.
func findPostgresInOutput(output string) string {
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		image := strings.TrimSpace(parts[1])
		if name != "" && strings.Contains(strings.ToLower(image), "postgres") {
			return name
		}
	}
	return ""
}
