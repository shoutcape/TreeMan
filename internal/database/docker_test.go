package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildPsqlArgs(t *testing.T) {
	t.Run("standard args", func(t *testing.T) {
		args := buildPsqlArgs("my-postgres-1", "postgres://postgres:postgres@127.0.0.1:5432", "myapp__jd_fix_123")
		expected := []string{
			"exec", "my-postgres-1",
			"psql", "-U", "postgres",
			"-c", `CREATE DATABASE "myapp__jd_fix_123"`,
		}
		assert.Equal(t, expected, args)
	})

	t.Run("custom user", func(t *testing.T) {
		args := buildPsqlArgs("pg-1", "postgresql://myuser:pass@host:5432", "testdb")
		expected := []string{
			"exec", "pg-1",
			"psql", "-U", "myuser",
			"-c", `CREATE DATABASE "testdb"`,
		}
		assert.Equal(t, expected, args)
	})
}

func TestBuildDropArgs(t *testing.T) {
	t.Run("standard args", func(t *testing.T) {
		args := buildDropArgs("my-postgres-1", "postgres://postgres:postgres@127.0.0.1:5432", "myapp__jd_fix_123")
		expected := []string{
			"exec", "my-postgres-1",
			"psql", "-U", "postgres",
			"-c", `DROP DATABASE IF EXISTS "myapp__jd_fix_123"`,
		}
		assert.Equal(t, expected, args)
	})
}

func TestExtractUser(t *testing.T) {
	tests := []struct {
		name    string
		baseURI string
		want    string
	}{
		{"standard postgres URI", "postgres://postgres:postgres@127.0.0.1:5432", "postgres"},
		{"custom user", "postgresql://myuser:pass@host:5432", "myuser"},
		{"no user info", "postgres://host:5432", "postgres"},
		{"empty string", "", "postgres"},
		{"invalid URI", "not-a-uri", "postgres"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractUser(tt.baseURI)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseContainerName(t *testing.T) {
	t.Run("single container with newline", func(t *testing.T) {
		result := parseContainerName("my-project-postgres-1\n")
		assert.Equal(t, "my-project-postgres-1", result)
	})

	t.Run("multiple containers returns first", func(t *testing.T) {
		result := parseContainerName("project-postgres-1\nother-postgres-1\n")
		assert.Equal(t, "project-postgres-1", result)
	})

	t.Run("empty string", func(t *testing.T) {
		result := parseContainerName("")
		assert.Equal(t, "", result)
	})

	t.Run("whitespace only", func(t *testing.T) {
		result := parseContainerName("  \n")
		assert.Equal(t, "", result)
	})
}

func TestFindPostgresInOutput(t *testing.T) {
	t.Run("finds postgres image", func(t *testing.T) {
		result := findPostgresInOutput("my-project-postgres-1\tpostgres:17.6-alpine\n")
		assert.Equal(t, "my-project-postgres-1", result)
	})

	t.Run("no postgres container", func(t *testing.T) {
		result := findPostgresInOutput("redis-1\tredis:7\nnginx-1\tnginx:latest\n")
		assert.Equal(t, "", result)
	})

	t.Run("empty string", func(t *testing.T) {
		result := findPostgresInOutput("")
		assert.Equal(t, "", result)
	})
}
