package database

import (
	"encoding/json"
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
	ResolveID(id string) (ContainerTarget, error)
}

type dockerContainer struct {
	ID    string `json:"ID"`
	Name  string `json:"Names"`
	Image string `json:"Image"`
	Ports string `json:"Ports"`
}

type containerResolver struct {
	containers []dockerContainer
}

// NewContainerResolver loads running Docker containers once. Reusing the
// resolver lets bulk cleanup avoid repeated docker ps calls.
func NewContainerResolver() (ContainerResolver, error) {
	out, err := exec.Command("docker", "ps", "--no-trunc", "--format", "{{json .}}").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps failed: %w", err)
	}
	containers, err := parseDockerSnapshot(string(out))
	if err != nil {
		return nil, err
	}
	return containerResolver{containers: containers}, nil
}

func parseDockerSnapshot(output string) ([]dockerContainer, error) {
	var containers []dockerContainer
	for lineNumber, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var container dockerContainer
		if err := json.Unmarshal([]byte(line), &container); err != nil {
			return nil, fmt.Errorf("parsing docker ps row %d: %w", lineNumber+1, err)
		}
		container.ID = strings.TrimSpace(container.ID)
		container.Name = strings.TrimSpace(container.Name)
		container.Image = strings.TrimSpace(container.Image)
		container.Ports = strings.TrimSpace(container.Ports)
		if container.ID == "" || container.Name == "" || container.Image == "" {
			return nil, fmt.Errorf("invalid docker ps row %d: ID, name, and image are required", lineNumber+1)
		}
		containers = append(containers, container)
	}
	return containers, nil
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
		if isOfficialPostgresImage(container.Image) && publishesPort(container.Ports, port) {
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

func isOfficialPostgresImage(image string) bool {
	name := strings.ToLower(strings.TrimSpace(image))
	if at := strings.IndexByte(name, '@'); at >= 0 {
		name = name[:at]
	}
	lastSlash := strings.LastIndexByte(name, '/')
	lastColon := strings.LastIndexByte(name, ':')
	if lastColon > lastSlash {
		name = name[:lastColon]
	}
	component := name
	if lastSlash >= 0 {
		component = name[lastSlash+1:]
	}
	return component == "postgres"
}

// ResolveID confirms that the exact container recorded during setup is still
// running. Cleanup must not fall back to a container that later reused a name
// or published the same port.
func (r containerResolver) ResolveID(id string) (ContainerTarget, error) {
	for _, container := range r.containers {
		if container.ID == id {
			return ContainerTarget{ID: container.ID, Name: container.Name}, nil
		}
	}
	return ContainerTarget{}, fmt.Errorf("recorded PostgreSQL container %q is not running", id)
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

// DatabaseExists reports whether a database is present. The question goes to
// the maintenance database rather than to the database itself, so a database
// that exists but refuses connections is still reported as present.
func DatabaseExists(container, user, dbName string) (bool, error) {
	args := buildExistsArgs(container, user, dbName)
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("checking database %q failed: %s", dbName, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)) == "1", nil
}

// DropDatabases terminates active connections and drops a set of owned
// databases. Each -c is a separate request: PostgreSQL cannot run DROP
// DATABASE inside the transaction used for a multi-statement request.
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

// buildPsqlArgs constructs docker exec arguments for CREATE DATABASE.
func buildPsqlArgs(container, user, dbName string) []string {
	return []string{
		"exec", container,
		"psql", "-U", user, "-d", "postgres",
		"-v", "ON_ERROR_STOP=1",
		"-c", fmt.Sprintf("CREATE DATABASE %s", quoteIdentifier(dbName)),
	}
}

// buildExistsArgs constructs a docker exec request that prints exactly "1"
// when the database exists and nothing when it does not. -tA strips the
// headers and alignment psql would otherwise add.
func buildExistsArgs(container, user, dbName string) []string {
	return []string{
		"exec", container,
		"psql", "-U", user, "-d", "postgres",
		"-v", "ON_ERROR_STOP=1",
		"-tAc", fmt.Sprintf("SELECT 1 FROM pg_database WHERE datname = %s", quoteLiteral(dbName)),
	}
}

// buildDropArgs constructs separate psql requests for connection termination
// and database removal.
func buildDropArgs(container, user string, dbNames ...string) []string {
	args := []string{
		"exec", container,
		"psql", "-U", user, "-d", "postgres",
		"-v", "ON_ERROR_STOP=1",
	}
	for _, dbName := range dbNames {
		args = append(args,
			"-c",
			fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = %s AND pid <> pg_backend_pid()", quoteLiteral(dbName)),
			"-c",
			fmt.Sprintf("DROP DATABASE IF EXISTS %s", quoteIdentifier(dbName)),
		)
	}
	return args
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func quoteLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}
