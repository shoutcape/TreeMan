package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/state"
	"github.com/shoutcape/treeman/internal/terminal"
	"github.com/shoutcape/treeman/internal/worktree"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOfferWarmStartsAnswersClaimOnceAcrossCommandContexts(t *testing.T) {
	for _, test := range []struct {
		name       string
		input      string
		wantConfig bool
	}{
		{name: "yes", input: "y\n", wantConfig: true},
		{name: "no", input: "n\n"},
		{name: "newline", input: "\n"},
		{name: "eof", input: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			project, target := warmStartProject(t)
			firstOut, firstErr := &bytes.Buffer{}, &bytes.Buffer{}
			offerWarmStarts(warmStartCommand(test.input, firstOut, firstErr, false), project, false)
			secondOut, secondErr := &bytes.Buffer{}, &bytes.Buffer{}
			offerWarmStarts(warmStartCommand("y\n", secondOut, secondErr, false), project, false)

			if test.wantConfig {
				assert.Equal(t, "[deps]\nreuse = [\"node_modules\"]\n", readTestFile(t, filepath.Join(project.mainRoot, config.ConfigFileName)))
			} else {
				assert.NoFileExists(t, filepath.Join(project.mainRoot, config.ConfigFileName))
			}
			assert.Empty(t, secondOut.String())
			assert.Empty(t, secondErr.String())
			assert.NoFileExists(t, filepath.Join(target, config.ConfigFileName))
			assert.Equal(t, 1, strings.Count(firstErr.String(), "WARM STARTS"))
		})
	}
}

func TestOfferWarmStartsSkipDepsAndNoninteractiveDoNotClaim(t *testing.T) {
	for _, test := range []struct {
		name string
		cmd  func(*bytes.Buffer, *bytes.Buffer) *cobra.Command
	}{
		{name: "skip deps", cmd: func(out, err *bytes.Buffer) *cobra.Command {
			return warmStartCommand("y\n", out, err, true)
		}},
		{name: "noninteractive", cmd: func(out, err *bytes.Buffer) *cobra.Command {
			return commandWithOutput(out, err)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			project, _ := warmStartProject(t)
			out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
			if test.name == "skip deps" {
				offerWarmStarts(test.cmd(out, errOut), project, true)
			} else {
				offerWarmStarts(test.cmd(out, errOut), project, false)
			}
			assert.Empty(t, errOut.String())

			followOut, followErr := &bytes.Buffer{}, &bytes.Buffer{}
			offerWarmStarts(warmStartCommand("y\n", followOut, followErr, false), project, false)
			assert.Contains(t, followErr.String(), "WARM STARTS")
			assert.FileExists(t, filepath.Join(project.mainRoot, config.ConfigFileName))
		})
	}
}

func TestOfferWarmStartsSilentlySkipsInvalidCandidates(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(string)
	}{
		{name: "missing package", setup: func(root string) {
			require.NoError(t, os.Mkdir(filepath.Join(root, "node_modules"), 0o755))
		}},
		{name: "missing node_modules", setup: func(root string) {
			require.NoError(t, os.WriteFile(filepath.Join(root, "package.json"), []byte("{}"), 0o644))
		}},
		{name: "nonregular package", setup: func(root string) {
			require.NoError(t, os.Mkdir(filepath.Join(root, "package.json"), 0o755))
			require.NoError(t, os.Mkdir(filepath.Join(root, "node_modules"), 0o755))
		}},
		{name: "symlink node_modules", setup: func(root string) {
			require.NoError(t, os.WriteFile(filepath.Join(root, "package.json"), []byte("{}"), 0o644))
			target := filepath.Join(root, "real-modules")
			require.NoError(t, os.Mkdir(target, 0o755))
			require.NoError(t, os.Symlink(target, filepath.Join(root, "node_modules")))
		}},
		{name: "nested only", setup: func(root string) {
			nested := filepath.Join(root, "apps", "web")
			require.NoError(t, os.MkdirAll(filepath.Join(nested, "node_modules"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(nested, "package.json"), []byte("{}"), 0o644))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			project, _ := warmStartProjectWithoutCandidates(t)
			test.setup(project.mainRoot)
			out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
			offerWarmStarts(warmStartCommand("y\n", out, errOut, false), project, false)
			assert.Empty(t, out.String())
			assert.Empty(t, errOut.String())
			assert.NoFileExists(t, filepath.Join(project.mainRoot, config.ConfigFileName))
			claimed, err := state.ClaimWarmStartPrompt(project.protected.CommonDir)
			require.NoError(t, err)
			assert.True(t, claimed, "invalid candidates must not record a prompt claim")
		})
	}
}

func TestOfferWarmStartsSkipsConfiguredAndInheritedConfig(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{name: "empty reuse", setup: func(t *testing.T, root string) {
			require.NoError(t, os.WriteFile(filepath.Join(root, config.ConfigFileName), []byte("[deps]\nreuse = []\n"), 0o600))
		}},
		{name: "configured reuse", setup: func(t *testing.T, root string) {
			require.NoError(t, os.WriteFile(filepath.Join(root, config.ConfigFileName), []byte("[deps]\nreuse = [\"node_modules\"]\n"), 0o600))
		}},
		{name: "inherited", setup: func(t *testing.T, root string) {
			parent := filepath.Dir(root)
			require.NoError(t, os.WriteFile(filepath.Join(parent, config.ConfigFileName), []byte("[worktree]\n"), 0o600))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			project, _ := warmStartProject(t)
			test.setup(t, project.mainRoot)
			out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
			offerWarmStarts(warmStartCommand("y\n", out, errOut, false), project, false)
			assert.Empty(t, out.String())
			assert.Empty(t, errOut.String())
		})
	}
}

func TestOfferWarmStartsRendersHyperlinkOrURLFallback(t *testing.T) {
	for _, hyperlinks := range []bool{true, false} {
		t.Run(map[bool]string{true: "hyperlink", false: "plain"}[hyperlinks], func(t *testing.T) {
			project, _ := warmStartProject(t)
			out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
			offerWarmStarts(warmStartCommand("n\n", out, errOut, hyperlinks), project, false)
			if hyperlinks {
				assert.Contains(t, errOut.String(), "\x1b]8;;"+warmStartsDocsURL)
			} else {
				assert.Contains(t, errOut.String(), "What are warm starts? "+warmStartsDocsURL)
			}
		})
	}
}

func TestOfferWarmStartsWriteRefusalWarnsAndDoesNotRepeat(t *testing.T) {
	project, _ := warmStartProject(t)
	configPath := filepath.Join(project.mainRoot, config.ConfigFileName)
	backing := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(backing, []byte("[project]\nname = \"test\"\n"), 0o600))
	require.NoError(t, os.Link(backing, configPath))

	firstOut, firstErr := &bytes.Buffer{}, &bytes.Buffer{}
	offerWarmStarts(warmStartCommand("y\n", firstOut, firstErr, false), project, false)
	secondOut, secondErr := &bytes.Buffer{}, &bytes.Buffer{}
	offerWarmStarts(warmStartCommand("y\n", secondOut, secondErr, false), project, false)

	assert.Contains(t, firstErr.String(), "could not configure warm starts")
	assert.Empty(t, secondOut.String())
	assert.Empty(t, secondErr.String())
}

func TestOfferWarmStartsIsOncePerRepository(t *testing.T) {
	project, _ := warmStartProject(t)
	other, _ := warmStartProject(t)
	for _, candidate := range []projectPaths{project, other} {
		out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
		offerWarmStarts(warmStartCommand("n\n", out, errOut, false), candidate, false)
		assert.Equal(t, 1, strings.Count(errOut.String(), "WARM STARTS"))
	}
}

func TestRunCreateWarmStartOfferKeepsStdoutAsPathOnly(t *testing.T) {
	repo, _ := createMergedCleanWorktree(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "package.json"), []byte("{}\n"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(repo, "node_modules"), 0o755))
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	changeToDir(t, repo)
	pathWithOnlyGit(t)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	cmd := warmStartCommand("y\n", stdout, stderr, false)

	require.NoError(t, runCreate(cmd, "future-only", creationSetupOptions{
		skipEnv: true, skipDatabase: true, skipHooks: true,
	}, ""))

	target := strings.TrimSpace(stdout.String())
	assert.Equal(t, target+"\n", stdout.String())
	assert.FileExists(t, filepath.Join(repo, config.ConfigFileName))
	assert.NoFileExists(t, filepath.Join(target, config.ConfigFileName))
	assert.NoDirExists(t, filepath.Join(target, "node_modules"), "accepting must not copy into the worktree whose setup finished")
	assert.Contains(t, stderr.String(), "Current setup was not rerun")
	assert.Less(t, strings.Index(stderr.String(), "SETUP"), strings.Index(stderr.String(), "WARM STARTS"))
}

func warmStartProject(t *testing.T) (projectPaths, string) {
	project, target := warmStartProjectWithoutCandidates(t)
	require.NoError(t, os.WriteFile(filepath.Join(project.mainRoot, "package.json"), []byte("{}\n"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(project.mainRoot, "node_modules"), 0o755))
	return project, target
}

func warmStartProjectWithoutCandidates(t *testing.T) (projectPaths, string) {
	root := t.TempDir()
	common := t.TempDir()
	target := t.TempDir()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	return projectPaths{
		mainRoot:  root,
		protected: worktree.Protected{CommonDir: common},
	}, target
}

func warmStartCommand(input string, stdout, stderr *bytes.Buffer, hyperlinks bool) *cobra.Command {
	cmd := commandWithOutput(stdout, stderr)
	cmd.SetIn(strings.NewReader(input))
	caps := terminal.Capabilities{InputTTY: true, OutputTTY: true, Interactive: true, Color: hyperlinks, Hyperlinks: hyperlinks, Width: 120}
	cmd.SetContext(context.WithValue(context.Background(), terminalSessionKey{}, terminalSession{errorOutput: caps, standardOut: caps}))
	return cmd
}
