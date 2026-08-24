package database

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURIAcceptsCaseInsensitiveSchemesAndDefaultsPort(t *testing.T) {
	parsed, err := ParseURI("POSTGRES://app:secret@localhost/mydb?sslmode=disable")
	require.NoError(t, err)
	assert.Equal(t, "mydb", parsed.Database)
	assert.Equal(t, "localhost", parsed.Host)
	assert.Equal(t, "5432", parsed.Port)
	assert.Equal(t, "app", parsed.User)
	assert.Equal(t, "POSTGRES://app:secret@localhost", parsed.BaseURI)
}

func TestBranchDBNameForRepositoryIsUniqueAndWithinPostgresLimit(t *testing.T) {
	first := BranchDBNameForRepository("myapp", "feature/a-b", "repository-one")
	second := BranchDBNameForRepository("myapp", "feature/a_b", "repository-one")
	otherRepository := BranchDBNameForRepository("myapp", "feature/a-b", "repository-two")
	otherSource := BranchDBNameForRepository("otherapp", "feature/a-b", "repository-one")
	assert.NotEqual(t, first, second)
	assert.NotEqual(t, first, otherRepository)
	assert.NotEqual(t, first, otherSource)
	assert.LessOrEqual(t, len(first), 63)
	assert.Contains(t, first, "__")
}

func TestBranchDBNameForRepositoryDoesNotSplitUTF8(t *testing.T) {
	name := BranchDBNameForRepository(strings.Repeat("database", 10), strings.Repeat("feature/é", 20), "repo")
	assert.True(t, len(name) <= 63)
	assert.True(t, utf8.ValidString(name))
}

func TestReplaceDatabasePreservesOriginalEncoding(t *testing.T) {
	result, err := ReplaceDatabase("postgres://user:p%40ss@host:5432/mydb?sslmode=verify-full", "mydb__branch_hash")
	require.NoError(t, err)
	assert.Equal(t, "postgres://user:p%40ss@host:5432/mydb__branch_hash?sslmode=verify-full", result)
}

func TestReplaceDatabaseEscapesUTF8DatabaseName(t *testing.T) {
	result, err := ReplaceDatabase("postgres://user@host:5432/mydb", "mydb__café_hash")
	require.NoError(t, err)
	assert.Equal(t, "postgres://user@host:5432/mydb__caf%C3%A9_hash", result)
}
