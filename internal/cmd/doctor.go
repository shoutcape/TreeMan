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
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
)

var lookPath = exec.LookPath

type diagnosticStatus int

const (
	diagnosticPass diagnosticStatus = iota
	diagnosticInfo
	diagnosticWarn
	diagnosticFail
)

type diagnostic struct {
	status  diagnosticStatus
	name    string
	message string
	hint    string
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check repository readiness and configuration",
		Args:  cobra.NoArgs,
		Run:   runDoctor,
	}
}

func runDoctor(cmd *cobra.Command, _ []string) {
	diagnostics := collectDiagnostics()
	writeDiagnostics(cmd.OutOrStdout(), diagnostics)
}

func collectDiagnostics() []diagnostic {
	diagnostics := make([]diagnostic, 0, 7)
	if _, err := lookPath("git"); err != nil {
		diagnostics = append(diagnostics, diagnostic{
			status: diagnosticFail, name: "Git", message: "Not installed",
			hint: "Install Git: https://git-scm.com/downloads",
		})
	} else if !git.IsInsideRepo() {
		diagnostics = append(diagnostics, diagnostic{
			status: diagnosticFail, name: "Repository", message: "Not detected",
			hint: "Run treeman doctor from a Git repository.",
		})
	} else {
		diagnostics = append(diagnostics, diagnostic{status: diagnosticPass, name: "Repository", message: "Git repository detected"})
		diagnostics = append(diagnostics, collectForgeDiagnostic())
		diagnostics = append(diagnostics, collectConfigDiagnostics()...)
	}

	diagnostics = append(diagnostics, collectToolDiagnostic("fzf"))
	diagnostics = append(diagnostics, collectToolDiagnostic("docker"))
	diagnostics = append(diagnostics, collectShellDiagnostic())
	return diagnostics
}

func collectForgeDiagnostic() diagnostic {
	remoteURL, err := git.OriginRemoteURL()
	if err != nil {
		return diagnostic{
			status: diagnosticFail, name: "Forge CLI", message: "Origin remote not found",
			hint: "Add an origin remote that points to GitHub or GitLab.",
		}
	}

	forgeType, _, _, err := forge.ResolveFromRemote(remoteURL)
	if err != nil {
		return diagnostic{
			status: diagnosticFail, name: "Forge CLI", message: "Unsupported forge",
			hint: "Set origin to github.com or a GitLab instance.",
		}
	}

	cli := forge.CLITool(forgeType)
	forgeName := "GitHub"
	if forgeType == forge.GitLab {
		forgeName = "GitLab"
	}
	if _, err := lookPath(cli); err != nil {
		return diagnostic{
			status: diagnosticFail, name: "Forge CLI", message: forgeName + " repository detected; " + cli + " not installed",
			hint: "Install " + cli + ": " + cliInstallURL(forgeType),
		}
	}
	return diagnostic{status: diagnosticPass, name: "Forge CLI", message: forgeName + " repository; " + cli + " installed"}
}

func collectConfigDiagnostics() []diagnostic {
	root, err := git.MainWorktreeRoot()
	if err != nil {
		return []diagnostic{{
			status: diagnosticFail, name: "Configuration", message: "Git worktree state unavailable",
			hint: "Resolve the Git worktree state, then rerun treeman doctor.",
		}}
	}

	result := config.Load(root)
	if result.Warning != "" {
		return []diagnostic{{
			status: diagnosticFail, name: "Configuration", message: "Invalid .treeman.toml",
			hint: "Fix " + result.Warning,
		}}
	}
	if result.Path == "" {
		return []diagnostic{
			{status: diagnosticInfo, name: "Configuration", message: "No .treeman.toml found; optional setup disabled"},
			{status: diagnosticInfo, name: "Database setup", message: "Not configured; add [database] to enable"},
		}
	}
	if result.Config.Database == nil {
		return []diagnostic{
			{status: diagnosticPass, name: "Configuration", message: "Valid .treeman.toml"},
			{status: diagnosticInfo, name: "Database setup", message: "Not configured; add [database] to enable"},
		}
	}
	return []diagnostic{
		{status: diagnosticPass, name: "Configuration", message: "Valid .treeman.toml"},
		{status: diagnosticPass, name: "Database setup", message: "Configured with " + result.Config.Database.EnvKey},
	}
}

func collectToolDiagnostic(tool string) diagnostic {
	if tool == "fzf" {
		if _, err := lookPath(tool); err != nil {
			return diagnostic{status: diagnosticWarn, name: "Interactive picker", message: "fzf not installed", hint: "Install fzf: https://github.com/junegunn/fzf"}
		}
		return diagnostic{status: diagnosticPass, name: "Interactive picker", message: "fzf installed"}
	}

	if _, err := lookPath(tool); err != nil {
		return diagnostic{status: diagnosticWarn, name: "Container support", message: "Docker not installed", hint: "Install and start Docker: https://docs.docker.com/get-docker/"}
	}
	return diagnostic{status: diagnosticPass, name: "Container support", message: "Docker installed; daemon unchecked"}
}

func collectShellDiagnostic() diagnostic {
	shell := filepath.Base(os.Getenv("SHELL"))
	if shell != "bash" && shell != "zsh" {
		shell = "bash"
	}
	return diagnostic{
		status: diagnosticWarn, name: "Shell integration", message: "Not detected",
		hint: fmt.Sprintf("Add to ~/.%src:\neval \"$(treeman init %s)\"", shell, shell),
	}
}

func writeDiagnostics(out interface{ Write([]byte) (int, error) }, diagnostics []diagnostic) {
	fmt.Fprintf(out, "\n%sDIAGNOSTICS%s\n\n", ui.ColorCyan, ui.ColorReset)
	counts := [4]int{}
	for _, diagnostic := range diagnostics {
		counts[diagnostic.status]++
		symbol, color := diagnosticAppearance(diagnostic.status)
		fmt.Fprintf(out, "  %s%s%s  %-20s %s%s%s\n", color, symbol, ui.ColorReset, diagnostic.name, ui.ColorDim, diagnostic.message, ui.ColorReset)
		if diagnostic.hint != "" {
			writeDiagnosticHint(out, diagnostic.hint)
		}
	}

	summary := make([]string, 0, 4)
	if counts[diagnosticPass] > 0 {
		summary = append(summary, fmt.Sprintf("%d passed", counts[diagnosticPass]))
	}
	if counts[diagnosticInfo] > 0 {
		summary = append(summary, fmt.Sprintf("%d informational", counts[diagnosticInfo]))
	}
	if counts[diagnosticWarn] > 0 {
		summary = append(summary, fmt.Sprintf("%d warning", counts[diagnosticWarn]))
	}
	if counts[diagnosticFail] > 0 {
		summary = append(summary, fmt.Sprintf("%d failed", counts[diagnosticFail]))
	}
	fmt.Fprintf(out, "\n%s%s%s\n", ui.ColorStatus, strings.Join(summary, " · "), ui.ColorReset)
}

func diagnosticAppearance(status diagnosticStatus) (symbol, color string) {
	switch status {
	case diagnosticInfo:
		return "○", ui.ColorDim
	case diagnosticWarn:
		return "!", ui.ColorWarning
	case diagnosticFail:
		return "✗", ui.ColorFailure
	default:
		return "✓", ui.ColorStatus
	}
}

func writeDiagnosticHint(out interface{ Write([]byte) (int, error) }, hint string) {
	lines := strings.Split(hint, "\n")
	for i, line := range lines {
		color := ui.ColorDim
		if i > 0 {
			color = ui.ColorPath
		}
		fmt.Fprintf(out, "\n     %s%s%s", color, line, ui.ColorReset)
	}
	fmt.Fprintln(out)
}
