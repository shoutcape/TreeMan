package database

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// ContainerTarget identifies a running container selected for database work.
type ContainerTarget struct {
	ID   string
	Name string
}

// ContainerResolver resolves configured containers or unambiguous local port
// matches from one Docker snapshot.
type ContainerResolver interface {
	Resolve(host, port, configuredName string) (ContainerTarget, error)
}

type dockerContainer struct {
	ID    string
	Name  string
	Image string
	Ports string
}

type containerResolver struct {
	containers []dockerContainer
}

// NewContainerResolver loads running Docker containers once. Reusing the
// resolver lets bulk cleanup avoid repeated docker ps calls.
func NewContainerResolver() (ContainerResolver, error) {
	out, err := exec.Command("docker", "ps", "--format", "{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Ports}}").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps failed: %w", err)
	}
	containers := make([]dockerContainer, 0)
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) != 4 {
			continue
		}
		container := dockerContainer{
			ID:    strings.TrimSpace(parts[0]),
			Name:  strings.TrimSpace(parts[1]),
			Image: strings.TrimSpace(parts[2]),
			Ports: strings.TrimSpace(parts[3]),
		}
		if container.ID != "" && container.Name != "" {
			containers = append(containers, container)
		}
	}
	return containerResolver{containers: containers}, nil
}

func (r containerResolver) Resolve(host, port, configuredName string) (ContainerTarget, error) {
	if configuredName != "" {
		for _, container := range r.containers {
			if container.Name == configuredName || container.ID == configuredName {
				return ContainerTarget{ID: container.ID, Name: container.Name}, nil
			}
		}
		return ContainerTarget{}, fmt.Errorf("configured PostgreSQL container %q is not running", configuredName)
	}
	if !isLocalDatabaseHost(host) {
		return ContainerTarget{}, fmt.Errorf("database host %q requires [database].container", host)
	}
	var matches []dockerContainer
	for _, container := range r.containers {
		if strings.Contains(strings.ToLower(container.Image), "postgres") && publishesPort(container.Ports, port) {
			matches = append(matches, container)
		}
	}
	if len(matches) == 0 {
		return ContainerTarget{}, fmt.Errorf("no running PostgreSQL container publishes port %s", port)
	}
	if len(matches) > 1 {
		names := make([]string, 0, len(matches))
		for _, match := range matches {
			names = append(names, match.Name)
		}
		sort.Strings(names)
		return ContainerTarget{}, fmt.Errorf("multiple PostgreSQL containers publish port %s (%s); set [database].container", port, strings.Join(names, ", "))
	}
	return ContainerTarget{ID: matches[0].ID, Name: matches[0].Name}, nil
}

func isLocalDatabaseHost(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	return host == "" || host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func publishesPort(ports, port string) bool {
	return strings.Contains(ports, ":"+port+"->")
}

// CreateDatabase creates a database through the PostgreSQL maintenance
// database. Callers create the ownership record before invoking this function,
// so accepting an existing name makes an interrupted setup retryable.
func CreateDatabase(container, user, dbName string) error {
	args := buildPsqlArgs(container, user, dbName)
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "already exists") {
			return nil
		}
		return fmt.Errorf("create database %q failed: %s", dbName, strings.TrimSpace(string(out)))
	}
	return nil
}

// DropDatabases terminates active connections and drops a set of owned
// databases in one psql invocation.
func DropDatabases(container, user string, dbNames []string) error {
	if len(dbNames) == 0 {
		return nil
	}
	args := buildDropArgs(container, user, dbNames...)
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("drop databases failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// DropDatabase drops one database. It remains available for focused callers.
func DropDatabase(container, user, dbName string) error {
	return DropDatabases(container, user, []string{dbName})
}

// buildPsqlArgs constructs docker exec arguments for CREATE DATABASE.
func buildPsqlArgs(container, user, dbName string) []string {
	return []string{
		"exec", container,
		"psql", "-U", user, "-d", "postgres",
		"-v", "ON_ERROR_STOP=1",
		"-c", fmt.Sprintf("CREATE DATABASE %s", quoteIdentifier(dbName)),
	}
}

// buildDropArgs constructs one psql command for a batch of owned databases.
func buildDropArgs(container, user string, dbNames ...string) []string {
	commands := make([]string, 0, len(dbNames)*2)
	for _, dbName := range dbNames {
		commands = append(commands,
			fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = %s AND pid <> pg_backend_pid()", quoteLiteral(dbName)),
			fmt.Sprintf("DROP DATABASE IF EXISTS %s", quoteIdentifier(dbName)),
		)
	}
	return []string{
		"exec", container,
		"psql", "-U", user, "-d", "postgres",
		"-v", "ON_ERROR_STOP=1",
		"-c", strings.Join(commands, "; "),
	}
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func quoteLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}
