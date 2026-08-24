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

func TestBuildPsqlArgsUsesMaintenanceDatabaseAndEscapesSQL(t *testing.T) {
	create := buildPsqlArgs("postgres", "app", `branch"db`)
	assert.Equal(t, []string{"exec", "postgres", "psql", "-U", "app", "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", `CREATE DATABASE "branch""db"`}, create)

	drop := buildDropArgs("postgres", "app", `branch'; DROP DATABASE postgres; --`)
	assert.Equal(t, "postgres", drop[6])
	assert.Contains(t, drop[10], "'branch''; DROP DATABASE postgres; --'")
	assert.Contains(t, drop[10], `DROP DATABASE IF EXISTS "branch'; DROP DATABASE postgres; --"`)
}
