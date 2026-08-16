package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/forge"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/spf13/cobra"
)

var lookPath = exec.LookPath

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check repository readiness and configuration",
		Args:  cobra.NoArgs,
		Run:   runDoctor,
	}
}

func runDoctor(cmd *cobra.Command, _ []string) {
	out := cmd.OutOrStdout()
	if _, err := lookPath("git"); err != nil {
		doctorResult(out, "FAIL", "Git", "Install Git: https://git-scm.com/downloads")
	} else if !git.IsInsideRepo() {
		doctorResult(out, "FAIL", "Repository", "Run treeman doctor from a Git repository.")
	} else {
		doctorResult(out, "PASS", "Git repository", "")
		doctorForge(out)
		doctorConfig(out)
	}

	doctorTool(out, "fzf", "WARN", "Install fzf for interactive selection: https://github.com/junegunn/fzf")
	doctorTool(out, "docker", "WARN", "Install and start Docker for branch database setup: https://docs.docker.com/get-docker/")
	doctorShell(out)
}

func doctorForge(out interface{ Write([]byte) (int, error) }) {
	remoteURL, err := git.OriginRemoteURL()
	if err != nil {
		doctorResult(out, "FAIL", "Origin remote", "Add an origin remote that points to GitHub or GitLab.")
		return
	}

	forgeType, _, _, err := forge.ResolveFromRemote(remoteURL)
	if err != nil {
		doctorResult(out, "FAIL", "Forge", "Set origin to github.com or a GitLab instance.")
		return
	}

	cli := forge.CLITool(forgeType)
	if _, err := lookPath(cli); err != nil {
		doctorResult(out, "FAIL", strings.ToUpper(cli), fmt.Sprintf("Install %s for %s repository support: %s", cli, forgeType, cliInstallURL(forgeType)))
		return
	}
	doctorResult(out, "PASS", fmt.Sprintf("%s CLI", cli), "")
}

func doctorConfig(out interface{ Write([]byte) (int, error) }) {
	root, err := git.MainWorktreeRoot()
	if err != nil {
		doctorResult(out, "FAIL", "Configuration", "Resolve the Git worktree state, then rerun treeman doctor.")
		return
	}

	result := config.Load(root)
	if result.Warning != "" {
		doctorResult(out, "FAIL", "Configuration", "Fix "+result.Warning)
		return
	}
	if result.Path == "" {
		doctorResult(out, "PASS", "Configuration", "No .treeman.toml configured.")
		doctorResult(out, "PASS", "Database configuration", "Branch databases are not configured.")
		return
	}
	doctorResult(out, "PASS", "Configuration", result.Path)
	if result.Config.Database == nil {
		doctorResult(out, "PASS", "Database configuration", "Branch databases are not configured.")
		return
	}
	doctorResult(out, "PASS", "Database configuration", "Uses "+result.Config.Database.EnvKey+". Database connectivity is not checked.")
}

func doctorTool(out interface{ Write([]byte) (int, error) }, tool, missingState, recovery string) {
	if _, err := lookPath(tool); err != nil {
		doctorResult(out, missingState, tool, recovery)
		return
	}
	doctorResult(out, "PASS", tool, "")
}

func doctorShell(out interface{ Write([]byte) (int, error) }) {
	shell := filepath.Base(os.Getenv("SHELL"))
	if shell != "bash" && shell != "zsh" {
		shell = "bash"
	}
	doctorResult(out, "WARN", "Shell integration", fmt.Sprintf("Enable wrappers with: eval \"$(treeman init %s)\"", shell))
}

func doctorResult(out interface{ Write([]byte) (int, error) }, state, check, detail string) {
	if detail == "" {
		fmt.Fprintf(out, "%s  %s\n", state, check)
		return
	}
	fmt.Fprintf(out, "%s  %s: %s\n", state, check, detail)
}
