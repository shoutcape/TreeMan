package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreationFlowsReuseMainDependencies(t *testing.T) {
	for _, flow := range []string{"create", "branch", "review"} {
		t.Run(flow, func(t *testing.T) {
			repo := createRemoteRepoWithNestedModule(t)
			require.NoError(t, os.WriteFile(filepath.Join(repo, "package-lock.json"), []byte("{}"), 0o644))
			gitTest(t, repo, "add", "package-lock.json")
			gitTest(t, repo, "commit", "-m", "add dependency manifest")
			gitTest(t, repo, "push", "origin", "main")
			invoker := filepath.Join(t.TempDir(), "invoker")
			gitTest(t, repo, "worktree", "add", "-b", "feature/invoker", invoker)
			for root, marker := range map[string]string{repo: "main", invoker: "invoker"} {
				require.NoError(t, os.Mkdir(filepath.Join(root, "node_modules"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(root, "node_modules", "marker"), []byte(marker), 0o644))
			}
			require.NoError(t, os.WriteFile(filepath.Join(repo, ".treeman.toml"), []byte("[deps]\nreuse = [\"node_modules\"]\n"), 0o600))
			branch := "feature/reuse"
			if flow != "create" {
				gitTest(t, repo, "push", "origin", "main:refs/heads/"+branch)
				if flow == "review" {
					gitTest(t, repo, "push", "origin", "main:refs/pull/1/head")
				}
			}
			binDir := t.TempDir()
			gitPath, err := exec.LookPath("git")
			require.NoError(t, err)
			require.NoError(t, os.Symlink(gitPath, filepath.Join(binDir, "git")))
			require.NoError(t, os.WriteFile(filepath.Join(binDir, "npm"), []byte("#!/bin/sh\n[ -f node_modules/marker ] || exit 1\nprintf installed > installer-ran\n"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(binDir, "gh"), []byte("#!/bin/sh\nprintf '{\"number\":1,\"title\":\"Reuse\",\"head\":{\"ref\":\"feature/reuse\"}}'\n"), 0o755))
			t.Setenv("PATH", binDir)
			t.Setenv("_TREEMAN_FORGE", "github")
			t.Setenv("_TREEMAN_GH_REPO", "owner/repo")
			chdirForTest(t, invoker)
			var stdout, stderr bytes.Buffer
			command := commandWithOutput(&stdout, &stderr)
			options := creationSetupOptions{skipEnv: true, skipDatabase: true, skipHooks: true}
			switch flow {
			case "create":
				err = runCreate(command, branch, options, "")
			case "branch":
				err = runBranch(command, branch, options)
			case "review":
				err = runReview(command, "1", options, "")
			}
			require.NoError(t, err)
			worktree := strings.TrimSpace(stdout.String())
			assert.Equal(t, "main", readTestFile(t, filepath.Join(worktree, "node_modules", "marker")))
			assert.FileExists(t, filepath.Join(worktree, "installer-ran"))
			assert.Contains(t, stderr.String(), "Reused node_modules")

			if flow == "create" {
				require.NoError(t, os.WriteFile(filepath.Join(worktree, "node_modules", "marker"), []byte("branch-edit"), 0o644))
				chdirForTest(t, worktree)
				_, output, err := runSetupIn(t, "", rerunSetupOptions{skipEnv: true, skipDatabase: true, skipHooks: true})
				require.NoError(t, err)
				assert.Contains(t, output, "destination exists")
				assert.Equal(t, "branch-edit", readTestFile(t, filepath.Join(worktree, "node_modules", "marker")))
				assert.Equal(t, "main", readTestFile(t, filepath.Join(repo, "node_modules", "marker")))
			}
		})
	}
}
