package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/forge"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/shoutcape/treeman/internal/worktree"
	"github.com/spf13/cobra"
)

var lookPath = exec.LookPath

var dockerDaemonReady = func() error {
	// docker info checks daemon connectivity without listing or changing containers.
	return exec.Command("docker", "info", "--format", "{{.ServerVersion}}").Run()
}

var shellIntegrationPattern = regexp.MustCompile(`^\s*eval\s+(?:"\$\(\s*treeman\s+init\s+%s\s*\)"|'\$\(\s*treeman\s+init\s+%s\s*\)'|\$\(\s*treeman\s+init\s+%s\s*\))\s*(?:#.*)?$`)

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
		RunE:  runDoctor,
	}
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	diagnostics := collectDiagnostics()
	writeDiagnostics(cmd.ErrOrStderr(), commandRenderer(cmd), diagnostics)
	for _, diagnostic := range diagnostics {
		if diagnostic.status == diagnosticFail {
			return fmt.Errorf("doctor found failed diagnostics; resolve them and rerun")
		}
	}
	return nil
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

	diagnostics = append(diagnostics, collectFzfDiagnostic())
	diagnostics = append(diagnostics, collectDockerDiagnostic())
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
	if _, err := worktree.ResolveDir(root, result.Config.WorktreeDirSetting()); err != nil {
		return []diagnostic{{
			status: diagnosticFail, name: "Configuration", message: "Invalid .treeman.toml",
			hint: "Fix " + err.Error(),
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

func collectFzfDiagnostic() diagnostic {
	if _, err := lookPath("fzf"); err != nil {
		return diagnostic{status: diagnosticWarn, name: "Interactive picker", message: "fzf not installed", hint: "Install fzf: https://github.com/junegunn/fzf"}
	}
	return diagnostic{status: diagnosticPass, name: "Interactive picker", message: "fzf installed"}
}

func collectDockerDiagnostic() diagnostic {
	if _, err := lookPath("docker"); err != nil {
		return diagnostic{status: diagnosticWarn, name: "Container support", message: "Docker not installed", hint: "Install and start Docker: https://docs.docker.com/get-docker/"}
	}
	if err := dockerDaemonReady(); err != nil {
		return diagnostic{status: diagnosticWarn, name: "Container support", message: "Docker installed; daemon unavailable", hint: "Start Docker, then rerun treeman doctor."}
	}
	return diagnostic{status: diagnosticPass, name: "Container support", message: "Docker installed; daemon ready"}
}

func collectShellDiagnostic() diagnostic {
	shellPath := os.Getenv("SHELL")
	if shellPath == "" {
		return diagnostic{status: diagnosticInfo, name: "Shell integration", message: "SHELL is not set; integration cannot be verified"}
	}

	shell := filepath.Base(shellPath)
	if shell != "bash" && shell != "zsh" {
		return diagnostic{status: diagnosticInfo, name: "Shell integration", message: "Unsupported shell " + shell + "; only bash and zsh can be verified"}
	}
	configPath := filepath.Join("~", "."+shell+"rc")
	home, err := os.UserHomeDir()
	if err != nil {
		return diagnostic{status: diagnosticInfo, name: "Shell integration", message: "Home directory unavailable; integration cannot be verified"}
	}
	data, err := os.ReadFile(filepath.Join(home, "."+shell+"rc"))
	if err == nil && hasShellIntegration(string(data), shell) {
		return diagnostic{status: diagnosticPass, name: "Shell integration", message: "Configured in " + configPath}
	}
	if err != nil && !os.IsNotExist(err) {
		return diagnostic{status: diagnosticInfo, name: "Shell integration", message: "Unable to read " + configPath + "; integration cannot be verified"}
	}
	return diagnostic{
		status: diagnosticInfo, name: "Shell integration", message: "Not configured",
		hint: "Run: treeman shell install",
	}
}

func hasShellIntegration(contents, shell string) bool {
	if strings.Contains(contents, shellBlockStart) && strings.Contains(contents, shellBlockEnd) {
		return true
	}
	pattern := regexp.MustCompile(fmt.Sprintf(shellIntegrationPattern.String(), regexp.QuoteMeta(shell), regexp.QuoteMeta(shell), regexp.QuoteMeta(shell)))
	for _, line := range strings.Split(contents, "\n") {
		if pattern.MatchString(line) {
			return true
		}
	}
	return false
}

func writeDiagnostics(out interface{ Write([]byte) (int, error) }, render ui.Renderer, diagnostics []diagnostic) {
	fmt.Fprintf(out, "\n%s\n\n", render.Title("DIAGNOSTICS"))
	counts := [4]int{}
	for _, diagnostic := range diagnostics {
		counts[diagnostic.status]++
	}

	for _, section := range []struct {
		title  string
		passed bool
	}{
		{title: "READY", passed: true},
		{title: "UNAVAILABLE OR NOT CONFIGURED"},
	} {
		sectionDiagnostics := make([]diagnostic, 0, len(diagnostics))
		for _, diagnostic := range diagnostics {
			if (diagnostic.status == diagnosticPass) == section.passed {
				sectionDiagnostics = append(sectionDiagnostics, diagnostic)
			}
		}
		if len(sectionDiagnostics) == 0 {
			continue
		}

		fmt.Fprintf(out, "%s\n\n", render.Header(section.title))
		for _, diagnostic := range sectionDiagnostics {
			symbol, tone := diagnosticAppearance(diagnostic.status)
			fmt.Fprintf(out, "  %s  %-20s %s\n", render.Tone(tone, symbol), diagnostic.name, render.Muted(diagnostic.message))
			if diagnostic.hint != "" {
				writeDiagnosticHint(out, render, diagnostic.hint)
			}
		}
		fmt.Fprintln(out)
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
	fmt.Fprintf(out, "\n%s\n", render.Tone(diagnosticSummaryTone(counts), strings.Join(summary, " · ")))
}

func diagnosticAppearance(status diagnosticStatus) (symbol string, tone ui.Tone) {
	switch status {
	case diagnosticInfo:
		return "○", ui.ToneMuted
	case diagnosticWarn:
		return "!", ui.ToneWarning
	case diagnosticFail:
		return "✗", ui.ToneFailure
	default:
		return "✓", ui.ToneSuccess
	}
}

func diagnosticSummaryTone(counts [4]int) ui.Tone {
	if counts[diagnosticFail] > 0 {
		return ui.ToneFailure
	}
	if counts[diagnosticWarn] > 0 {
		return ui.ToneWarning
	}
	return ui.ToneSuccess
}

func writeDiagnosticHint(out interface{ Write([]byte) (int, error) }, render ui.Renderer, hint string) {
	lines := strings.Split(hint, "\n")
	for i, line := range lines {
		style := render.Muted
		if i > 0 {
			style = render.Path
		}
		fmt.Fprintf(out, "\n     %s", style(line))
	}
	fmt.Fprintln(out)
}
