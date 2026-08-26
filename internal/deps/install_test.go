package deps

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetect_NoDepsFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644))

	result, err := Detect(dir)
	assert.NoError(t, err)
	assert.False(t, result.Python)
	assert.Nil(t, result.Installer)
}

func TestDetect_PythonProject(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask"), 0644))

	result, err := Detect(dir)
	assert.NoError(t, err)
	assert.True(t, result.Python)
	assert.Nil(t, result.Installer)
}

func TestInstall_OnlyRunsRootInstaller(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell script")
	}

	dir := t.TempDir()
	nestedModule := filepath.Join(dir, "tools", "generator")
	require.NoError(t, os.MkdirAll(nestedModule, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte("{}"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(nestedModule, "pnpm-lock.yaml"), []byte(""), 0644))

	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.Mkdir(binDir, 0755))
	logPath := filepath.Join(dir, "commands.log")
	command := []byte("#!/bin/sh\nprintf '%s:%s:%s\\n' \"${0##*/}\" \"$PWD\" \"$*\" >> \"$LOG_FILE\"\n")
	for _, name := range []string{"npm", "pnpm"} {
		require.NoError(t, os.WriteFile(filepath.Join(binDir, name), command, 0755))
	}
	t.Setenv("PATH", binDir)
	t.Setenv("LOG_FILE", logPath)

	detection, err := Detect(dir)
	require.NoError(t, err)
	require.NotNil(t, detection.Installer)
	assert.Equal(t, "npm", detection.Installer.Binary)
	require.NoError(t, Run(dir, detection.Installer, &bytes.Buffer{}))
	log, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Equal(t, "npm:"+dir+":install\n", string(log))
}

func TestDetect_PyprojectToml(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[tool.poetry]"), 0644))

	result, err := Detect(dir)
	assert.NoError(t, err)
	assert.True(t, result.Python)
}

func TestRun_CargoProject(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"example\""), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Cargo.lock"), []byte("version = 4"), 0644))

	binDir := t.TempDir()
	argsPath := filepath.Join(t.TempDir(), "cargo-args")
	cargoPath := filepath.Join(binDir, "cargo")
	require.NoError(t, os.WriteFile(cargoPath, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CARGO_ARGS\"\nprintf 'fetched dependencies\\n'\n"), 0755))
	t.Setenv("CARGO_ARGS", argsPath)
	t.Setenv("PATH", binDir)

	var output bytes.Buffer
	detection, err := Detect(dir)

	require.NoError(t, err)
	require.NotNil(t, detection.Installer)
	assert.Equal(t, "Cargo.toml", detection.Installer.Lockfile)
	assert.Equal(t, "cargo", detection.Installer.Binary)
	assert.Equal(t, []string{"fetch"}, detection.Installer.Args)
	require.NoError(t, Run(dir, detection.Installer, &output))
	args, err := os.ReadFile(argsPath)
	require.NoError(t, err)
	assert.Equal(t, "fetch\n", string(args))
	assert.Equal(t, "fetched dependencies\n", output.String())
}

func TestRun_BinaryNotFound(t *testing.T) {
	t.Setenv("PATH", "")

	err := Run(t.TempDir(), &Installer{Lockfile: "pnpm-lock.yaml", Binary: "pnpm"}, &bytes.Buffer{})
	assert.Error(t, err)
}

func TestDetect_InvalidDir(t *testing.T) {
	_, err := Detect("/nonexistent/path/that/does/not/exist")
	assert.Error(t, err)
}
