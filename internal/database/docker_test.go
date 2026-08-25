package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContainerResolverRequiresUniqueLocalPortMatch(t *testing.T) {
	resolver := containerResolver{containers: []dockerContainer{
		{ID: "one", Name: "project-db", Image: "postgres:17", Ports: "0.0.0.0:5432->5432/tcp"},
	}}
	target, err := resolver.Resolve("127.0.0.1", "5432", "")
	require.NoError(t, err)
	assert.Equal(t, ContainerTarget{ID: "one", Name: "project-db"}, target)

	_, err = resolver.Resolve("db.example.test", "5432", "")
	require.ErrorContains(t, err, "requires [database].container")

	resolver.containers = append(resolver.containers, dockerContainer{ID: "two", Name: "other-db", Image: "postgres:16", Ports: "[::]:5432->5432/tcp"})
	_, err = resolver.Resolve("localhost", "5432", "")
	require.ErrorContains(t, err, "multiple PostgreSQL containers")
}

func TestContainerResolverUsesConfiguredContainerExactly(t *testing.T) {
	resolver := containerResolver{containers: []dockerContainer{{ID: "one", Name: "project-db", Image: "custom-db", Ports: ""}}}
	target, err := resolver.Resolve("remote.example.test", "5432", "project-db")
	require.NoError(t, err)
	assert.Equal(t, "project-db", target.Name)

	_, err = resolver.Resolve("localhost", "5432", "missing")
	require.ErrorContains(t, err, "not running")
}

func TestParseDockerSnapshot(t *testing.T) {
	containers, err := parseDockerSnapshot("{\"ID\":\"abc123\",\"Names\":\"db\",\"Image\":\"postgres:17\",\"Ports\":\"127.0.0.1:5432->5432/tcp\"}\n")
	require.NoError(t, err)
	require.Len(t, containers, 1)
	assert.Equal(t, "abc123", containers[0].ID)
	_, err = parseDockerSnapshot("not-json\n")
	require.ErrorContains(t, err, "row 1")
	_, err = parseDockerSnapshot("{\"ID\":\"abc\",\"Names\":\"db\"}\n")
	require.ErrorContains(t, err, "required")
}

func TestOfficialPostgresImageMatching(t *testing.T) {
	for _, test := range []struct {
		image string
		want  bool
	}{
		{"postgres", true}, {"postgres:17-alpine", true}, {"docker.io/library/postgres:16", true},
		{"registry.example/team/postgres@sha256:abc", true}, {"postgis/postgis:17", false},
		{"custom-postgres:latest", false}, {"postgresql:17", false}, {"team/postgres-backup", false},
	} {
		t.Run(test.image, func(t *testing.T) { assert.Equal(t, test.want, isOfficialPostgresImage(test.image)) })
	}
}

func TestBuildPsqlArgsUsesMaintenanceDatabaseAndEscapesSQL(t *testing.T) {
	create := buildPsqlArgs("postgres", "app", `branch"db`)
	assert.Equal(t, []string{"exec", "postgres", "psql", "-U", "app", "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", `CREATE DATABASE "branch""db"`}, create)

	drop := buildDropArgs("postgres", "app", `branch'; DROP DATABASE postgres; --`)
	assert.Equal(t, "postgres", drop[6])
	assert.Equal(t, []string{
		"exec", "postgres", "psql", "-U", "app", "-d", "postgres", "-v", "ON_ERROR_STOP=1",
		"-c", "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = 'branch''; DROP DATABASE postgres; --' AND pid <> pg_backend_pid()",
		"-c", `DROP DATABASE IF EXISTS "branch'; DROP DATABASE postgres; --"`,
	}, drop)
}
