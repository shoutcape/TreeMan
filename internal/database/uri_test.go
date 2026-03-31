package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	t.Run("standard postgres URI", func(t *testing.T) {
		parsed, err := ParseURI("postgres://postgres:postgres@127.0.0.1:5432/myapp")
		require.NoError(t, err)
		assert.Equal(t, "myapp", parsed.Database)
		assert.Equal(t, "postgres://postgres:postgres@127.0.0.1:5432", parsed.BaseURI)
		assert.Equal(t, "", parsed.Query)
	})

	t.Run("postgresql scheme", func(t *testing.T) {
		parsed, err := ParseURI("postgresql://user:pass@localhost:5432/mydb")
		require.NoError(t, err)
		assert.Equal(t, "mydb", parsed.Database)
		assert.Equal(t, "postgresql://user:pass@localhost:5432", parsed.BaseURI)
		assert.Equal(t, "", parsed.Query)
	})

	t.Run("URI with query params", func(t *testing.T) {
		parsed, err := ParseURI("postgres://user:pass@host:5432/mydb?sslmode=verify-full&connect_timeout=10")
		require.NoError(t, err)
		assert.Equal(t, "mydb", parsed.Database)
		assert.Equal(t, "postgres://user:pass@host:5432", parsed.BaseURI)
		assert.Equal(t, "sslmode=verify-full&connect_timeout=10", parsed.Query)
	})

	t.Run("URI with percent-encoded password", func(t *testing.T) {
		parsed, err := ParseURI("postgres://user:p%40ss%23word@host:5432/mydb")
		require.NoError(t, err)
		assert.Equal(t, "mydb", parsed.Database)
		assert.Equal(t, "postgres://user:p%40ss%23word@host:5432", parsed.BaseURI)
		assert.Equal(t, "", parsed.Query)
	})

	t.Run("non-postgres scheme returns error", func(t *testing.T) {
		_, err := ParseURI("mysql://user:pass@host:3306/mydb")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported scheme")
	})

	t.Run("empty string returns error", func(t *testing.T) {
		_, err := ParseURI("")
		require.Error(t, err)
	})

	t.Run("no database in path returns error", func(t *testing.T) {
		_, err := ParseURI("postgres://user:pass@host:5432")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no database")
	})

	t.Run("just a slash no db name returns error", func(t *testing.T) {
		_, err := ParseURI("postgres://user:pass@host:5432/")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no database")
	})
}

func TestBranchDBName(t *testing.T) {
	t.Run("slashes and hyphens to underscores", func(t *testing.T) {
		result := BranchDBName("myapp", "jd/fix-123/add-user-auth")
		assert.Equal(t, "myapp__jd_fix_123_add_user_auth", result)
	})

	t.Run("simple branch name", func(t *testing.T) {
		result := BranchDBName("myapp", "hotfix")
		assert.Equal(t, "myapp__hotfix", result)
	})

	t.Run("dots to underscores", func(t *testing.T) {
		result := BranchDBName("db", "feat/v2.0-support")
		assert.Equal(t, "db__feat_v2_0_support", result)
	})

	t.Run("truncated to 63 chars", func(t *testing.T) {
		result := BranchDBName("myapp", "feature/very-long-branch-name-that-goes-on-and-on-and-on-and-on-and-on-forever")
		assert.LessOrEqual(t, len(result), 63)
		// Should start with the original DB name and double underscore
		assert.True(t, len(result) >= len("myapp__"))
		assert.Equal(t, "myapp__", result[:7])
	})
}

func TestReplaceDatabase(t *testing.T) {
	t.Run("simple replacement", func(t *testing.T) {
		result, err := ReplaceDatabase("postgres://postgres:postgres@127.0.0.1:5432/myapp", "myapp__jd_fix_123_add_user_auth")
		require.NoError(t, err)
		assert.Equal(t, "postgres://postgres:postgres@127.0.0.1:5432/myapp__jd_fix_123_add_user_auth", result)
	})

	t.Run("preserves query params", func(t *testing.T) {
		result, err := ReplaceDatabase("postgres://user:pass@host:5432/mydb?sslmode=verify-full", "mydb__hotfix")
		require.NoError(t, err)
		assert.Equal(t, "postgres://user:pass@host:5432/mydb__hotfix?sslmode=verify-full", result)
	})

	t.Run("preserves percent-encoded password", func(t *testing.T) {
		result, err := ReplaceDatabase("postgres://user:p%40ss%23word@host:5432/mydb", "mydb__branch")
		require.NoError(t, err)
		assert.Equal(t, "postgres://user:p%40ss%23word@host:5432/mydb__branch", result)
	})

	t.Run("invalid URI returns error", func(t *testing.T) {
		_, err := ReplaceDatabase("", "newdb")
		require.Error(t, err)
	})
}
