package deps

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
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

func TestDetect_IgnoresNestedModules(t *testing.T) {
	dir := t.TempDir()
	nestedModule := filepath.Join(dir, "tools", "generator")
	require.NoError(t, os.MkdirAll(nestedModule, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte("{}"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(nestedModule, "pnpm-lock.yaml"), []byte(""), 0644))

	result, err := Detect(dir)
	require.NoError(t, err)
	require.NotNil(t, result.Installer)
	assert.Equal(t, "package-lock.json", result.Installer.Manifest)
	assert.Equal(t, "npm", result.Installer.Binary)
	assert.Equal(t, []string{"install"}, result.Installer.Args)
}

func TestDetect_PyprojectToml(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[tool.poetry]"), 0644))

	result, err := Detect(dir)
	assert.NoError(t, err)
	assert.True(t, result.Python)
}

func TestDetect_CorepackManagedYarnProject(t *testing.T) {
	tests := []struct {
		name           string
		packageManager string
	}{
		{name: "version", packageManager: "yarn@4.9.2"},
		{name: "version with integrity", packageManager: "yarn@4.9.2+sha224.953c8233f7a92884eee2de69a1b92d1f2ec1655e66d08071ba9a02fa"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			manifest := fmt.Sprintf(`{"packageManager":%q}`, tt.packageManager)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(manifest), 0o644))
			// The explicit package manager takes priority over conflicting lockfiles.
			require.NoError(t, os.WriteFile(filepath.Join(dir, "pnpm-lock.yaml"), nil, 0o644))

			detection, err := Detect(dir)

			require.NoError(t, err)
			require.NotNil(t, detection.Installer)
			assert.Equal(t, "package.json", detection.Installer.Manifest)
			assert.Equal(t, "corepack", detection.Installer.Binary)
			assert.Equal(t, []string{"yarn", "install"}, detection.Installer.Args)
		})
	}
}

func TestDetect_InvalidPackageJSONFallsBackToKnownInstaller(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"packageManager":`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"example\""), 0o644))

	detection, err := Detect(dir)

	require.NoError(t, err)
	require.NotNil(t, detection.Installer)
	assert.Equal(t, "Cargo.toml", detection.Installer.Manifest)
	assert.Equal(t, "cargo", detection.Installer.Binary)
}

func TestDetect_LeavesYarnDescriptorValidationToCorepack(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"packageManager":"yarn"}`), 0o644))

	detection, err := Detect(dir)

	require.NoError(t, err)
	require.NotNil(t, detection.Installer)
	assert.Equal(t, "package.json", detection.Installer.Manifest)
	assert.Equal(t, "corepack", detection.Installer.Binary)
	assert.Equal(t, []string{"yarn", "install"}, detection.Installer.Args)
}

func TestDetect_PackageJSONWithoutYarnDeclarationFallsBackToLockfile(t *testing.T) {
	tests := []struct {
		name        string
		packageJSON string
		lockfile    string
		binary      string
	}{
		{name: "no package manager", packageJSON: `{}`, lockfile: "yarn.lock", binary: "yarn"},
		{name: "different package manager", packageJSON: `{"packageManager":"pnpm@9.0.0"}`, lockfile: "pnpm-lock.yaml", binary: "pnpm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(tt.packageJSON), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(dir, tt.lockfile), nil, 0o644))

			detection, err := Detect(dir)

			require.NoError(t, err)
			require.NotNil(t, detection.Installer)
			assert.Equal(t, tt.lockfile, detection.Installer.Manifest)
			assert.Equal(t, tt.binary, detection.Installer.Binary)
			assert.Equal(t, []string{"install"}, detection.Installer.Args)
		})
	}
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
	assert.Equal(t, "Cargo.toml", detection.Installer.Manifest)
	assert.Equal(t, "cargo", detection.Installer.Binary)
	assert.Equal(t, []string{"fetch"}, detection.Installer.Args)
	require.NoError(t, Run(dir, detection.Installer, &output))
	args, err := os.ReadFile(argsPath)
	require.NoError(t, err)
	assert.Equal(t, "fetch\n", string(args))
	assert.Equal(t, "fetched dependencies\n", output.String())
}

func TestRun_CorepackManagedYarnProject(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"packageManager":"yarn@4.9.2"}`), 0o644))

	binDir := t.TempDir()
	argsPath := filepath.Join(t.TempDir(), "corepack-args")
	workingDirPath := filepath.Join(t.TempDir(), "corepack-working-dir")
	corepackPath := filepath.Join(binDir, "corepack")
	require.NoError(t, os.WriteFile(corepackPath, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$COREPACK_ARGS\"\npwd > \"$COREPACK_WORKING_DIR\"\nprintf 'installed dependencies\\n'\n"), 0o755))
	t.Setenv("COREPACK_ARGS", argsPath)
	t.Setenv("COREPACK_WORKING_DIR", workingDirPath)
	t.Setenv("PATH", binDir)

	var output bytes.Buffer
	detection, err := Detect(dir)
	require.NoError(t, err)
	require.NoError(t, Run(dir, detection.Installer, &output))

	args, err := os.ReadFile(argsPath)
	require.NoError(t, err)
	assert.Equal(t, "yarn\ninstall\n", string(args))
	workingDir, err := os.ReadFile(workingDirPath)
	require.NoError(t, err)
	assert.Equal(t, dir+"\n", string(workingDir))
	assert.Equal(t, "installed dependencies\n", output.String())
}

func TestRun_BinaryNotFound(t *testing.T) {
	t.Setenv("PATH", "")

	err := Run(t.TempDir(), &Installer{Manifest: "pnpm-lock.yaml", Binary: "pnpm"}, &bytes.Buffer{})
	assert.Error(t, err)
}

func TestDetect_InvalidDir(t *testing.T) {
	_, err := Detect("/nonexistent/path/that/does/not/exist")
	assert.Error(t, err)
}
