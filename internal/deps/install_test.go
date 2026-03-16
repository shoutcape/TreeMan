package deps

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstall_NoDepsFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644))
	// No lockfile → skipped.
	result, err := Install(dir)
	assert.NoError(t, err)
	assert.True(t, result.Skipped)
	assert.Nil(t, result.Installer)
}

func TestInstall_PythonProject(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask"), 0644))

	result, err := Install(dir)
	assert.NoError(t, err)
	assert.True(t, result.Python)
	assert.Nil(t, result.Installer)
}

func TestInstall_PyprojectToml(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[tool.poetry]"), 0644))

	result, err := Install(dir)
	assert.NoError(t, err)
	assert.True(t, result.Python)
}

func TestInstall_BinaryNotFound(t *testing.T) {
	// Create a directory with a pnpm-lock.yaml but where pnpm is definitely
	// not on $PATH (we override PATH to empty).
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pnpm-lock.yaml"), []byte(""), 0644))

	// Temporarily clear PATH so no binary can be found.
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "")
	defer func() { os.Setenv("PATH", origPath) }()

	result, err := Install(dir)
	// Should not hard-fail — mirrors wt.sh warning-only behaviour.
	assert.Error(t, err) // warning error returned
	assert.True(t, result.Skipped)
}

func TestInstall_InvalidDir(t *testing.T) {
	_, err := Install("/nonexistent/path/that/does/not/exist")
	assert.Error(t, err)
}
