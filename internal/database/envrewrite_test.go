package database

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ReadEnvValue tests ---

func TestReadEnvValue_Found(t *testing.T) {
	dir := t.TempDir()
	content := `# App config
NODE_ENV=development
DATABASE_URI=postgres://postgres:postgres@127.0.0.1:5432/myapp
SECRET_KEY=abc123
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0600))

	uri, err := ReadEnvValue(dir, "DATABASE_URI")
	require.NoError(t, err)
	assert.Equal(t, "postgres://postgres:postgres@127.0.0.1:5432/myapp", uri)
}

func TestReadEnvValue_DifferentKey(t *testing.T) {
	dir := t.TempDir()
	content := `DATABASE_URL=postgres://user:pass@host:5432/mydb
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0600))

	uri, err := ReadEnvValue(dir, "DATABASE_URL")
	require.NoError(t, err)
	assert.Equal(t, "postgres://user:pass@host:5432/mydb", uri)
}

func TestReadEnvValue_NotFound(t *testing.T) {
	dir := t.TempDir()
	content := `NODE_ENV=development
SECRET_KEY=abc123
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0600))

	uri, err := ReadEnvValue(dir, "DATABASE_URI")
	require.NoError(t, err)
	assert.Equal(t, "", uri)
}

func TestReadEnvValue_NoFile(t *testing.T) {
	dir := t.TempDir()
	// No .env file created

	uri, err := ReadEnvValue(dir, "DATABASE_URI")
	require.NoError(t, err)
	assert.Equal(t, "", uri)
}

func TestReadEnvValue_DoubleQuoted(t *testing.T) {
	dir := t.TempDir()
	content := `DATABASE_URI="postgres://user:pass@host:5432/mydb"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0600))

	uri, err := ReadEnvValue(dir, "DATABASE_URI")
	require.NoError(t, err)
	assert.Equal(t, "postgres://user:pass@host:5432/mydb", uri)
}

func TestReadEnvValue_SingleQuoted(t *testing.T) {
	dir := t.TempDir()
	content := `DATABASE_URI='postgres://user:pass@host:5432/mydb'
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0600))

	uri, err := ReadEnvValue(dir, "DATABASE_URI")
	require.NoError(t, err)
	assert.Equal(t, "postgres://user:pass@host:5432/mydb", uri)
}

func TestReadEnvValue_SkipsComments(t *testing.T) {
	dir := t.TempDir()
	content := `# DATABASE_URI=postgres://old:old@host:5432/olddb
DATABASE_URI=postgres://real:real@host:5432/realdb
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0600))

	uri, err := ReadEnvValue(dir, "DATABASE_URI")
	require.NoError(t, err)
	assert.Equal(t, "postgres://real:real@host:5432/realdb", uri)
}

func TestReadEnvValue_EmptyLines(t *testing.T) {
	dir := t.TempDir()
	content := `
NODE_ENV=development

DATABASE_URI=postgres://host:5432/db

`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0600))

	uri, err := ReadEnvValue(dir, "DATABASE_URI")
	require.NoError(t, err)
	assert.Equal(t, "postgres://host:5432/db", uri)
}

// --- ReadDatabaseURI convenience wrapper tests ---

func TestReadDatabaseURI_EmptyKey(t *testing.T) {
	dir := t.TempDir()
	content := `DATABASE_URI=postgres://host:5432/db
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0600))

	// Empty envKey means database management not configured -- should return "".
	uri, err := ReadDatabaseURI(dir, "")
	require.NoError(t, err)
	assert.Equal(t, "", uri)
}

func TestReadDatabaseURI_WithKey(t *testing.T) {
	dir := t.TempDir()
	content := `DATABASE_URI=postgres://postgres:postgres@127.0.0.1:5432/myapp
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0600))

	uri, err := ReadDatabaseURI(dir, "DATABASE_URI")
	require.NoError(t, err)
	assert.Equal(t, "postgres://postgres:postgres@127.0.0.1:5432/myapp", uri)
}

// --- RewriteEnvValue tests ---

func TestRewriteEnvValue_Basic(t *testing.T) {
	dir := t.TempDir()
	original := "NODE_ENV=development\nDATABASE_URI=postgres://old@host:5432/olddb\nSECRET_KEY=abc123\n"
	envPath := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(envPath, []byte(original), 0600))

	err := RewriteEnvValue(dir, "DATABASE_URI", "postgres://new@host:5432/newdb")
	require.NoError(t, err)

	got, err := os.ReadFile(envPath)
	require.NoError(t, err)

	expected := "NODE_ENV=development\nDATABASE_URI=postgres://new@host:5432/newdb\nSECRET_KEY=abc123\n"
	assert.Equal(t, expected, string(got))
}

func TestRewriteEnvValue_DifferentKey(t *testing.T) {
	dir := t.TempDir()
	original := "NODE_ENV=development\nDATABASE_URL=postgres://old@host:5432/olddb\nSECRET_KEY=abc123\n"
	envPath := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(envPath, []byte(original), 0600))

	err := RewriteEnvValue(dir, "DATABASE_URL", "postgres://new@host:5432/newdb")
	require.NoError(t, err)

	got, err := os.ReadFile(envPath)
	require.NoError(t, err)

	expected := "NODE_ENV=development\nDATABASE_URL=postgres://new@host:5432/newdb\nSECRET_KEY=abc123\n"
	assert.Equal(t, expected, string(got))
}

func TestRewriteEnvValue_PreservesComments(t *testing.T) {
	dir := t.TempDir()
	original := "# This is a comment\n# DATABASE_URI=postgres://commented@host:5432/commented\nDATABASE_URI=postgres://real@host:5432/realdb\nOTHER=value\n"
	envPath := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(envPath, []byte(original), 0600))

	err := RewriteEnvValue(dir, "DATABASE_URI", "postgres://new@host:5432/newdb")
	require.NoError(t, err)

	got, err := os.ReadFile(envPath)
	require.NoError(t, err)

	expected := "# This is a comment\n# DATABASE_URI=postgres://commented@host:5432/commented\nDATABASE_URI=postgres://new@host:5432/newdb\nOTHER=value\n"
	assert.Equal(t, expected, string(got))
}

func TestRewriteEnvValue_NoFile(t *testing.T) {
	dir := t.TempDir()
	// No .env file

	err := RewriteEnvValue(dir, "DATABASE_URI", "postgres://new@host:5432/newdb")
	assert.Error(t, err)
}

func TestRewriteEnvValue_KeyNotFound(t *testing.T) {
	dir := t.TempDir()
	content := "NODE_ENV=development\nSECRET_KEY=abc123\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0600))

	err := RewriteEnvValue(dir, "DATABASE_URI", "postgres://new@host:5432/newdb")
	assert.Error(t, err)
}

func TestRewriteEnvValue_PreservesBlankLines(t *testing.T) {
	dir := t.TempDir()
	original := "NODE_ENV=development\n\nDATABASE_URI=postgres://old@host:5432/olddb\n\nSECRET_KEY=abc123\n"
	envPath := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(envPath, []byte(original), 0600))

	err := RewriteEnvValue(dir, "DATABASE_URI", "postgres://new@host:5432/newdb")
	require.NoError(t, err)

	got, err := os.ReadFile(envPath)
	require.NoError(t, err)

	expected := "NODE_ENV=development\n\nDATABASE_URI=postgres://new@host:5432/newdb\n\nSECRET_KEY=abc123\n"
	assert.Equal(t, expected, string(got))
}

func TestRewriteEnvValue_ReplacesQuotedValue(t *testing.T) {
	dir := t.TempDir()
	original := "DATABASE_URI=\"postgres://old@host:5432/olddb\"\nOTHER=value\n"
	envPath := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(envPath, []byte(original), 0600))

	err := RewriteEnvValue(dir, "DATABASE_URI", "postgres://new@host:5432/newdb")
	require.NoError(t, err)

	got, err := os.ReadFile(envPath)
	require.NoError(t, err)

	expected := "DATABASE_URI=postgres://new@host:5432/newdb\nOTHER=value\n"
	assert.Equal(t, expected, string(got))
}
