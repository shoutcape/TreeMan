package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const (
	shellBlockStart = "# >>> TreeMan shell integration >>>"
	shellBlockEnd   = "# <<< TreeMan shell integration <<<"
)

type shellConfig struct {
	name string
	path string
}

func newShellCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shell",
		Short: "Manage shell integration",
	}
	cmd.AddCommand(newShellInitCmd())
	cmd.AddCommand(newShellInstallCmd())
	cmd.AddCommand(newShellUninstallCmd())
	cmd.AddCommand(newShellStatusCmd())
	return cmd
}

func newShellInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "init <shell>",
		Short:     "Print shell integration functions",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeShellInit(cmd.OutOrStdout(), args[0])
		},
	}
}

func newShellInstallCmd() *cobra.Command {
	var shell, configPath, binPath string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install shell integration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := resolveShellConfig(shell, configPath)
			if err != nil {
				return err
			}
			if err := installShellIntegration(cfg, binPath); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "TreeMan shell integration installed in %s\n", cfg.path)
			return nil
		},
	}
	cmd.Flags().StringVar(&shell, "shell", "", "Shell to configure: bash, zsh, or fish")
	cmd.Flags().StringVar(&configPath, "config", "", "Shell startup file to update")
	cmd.Flags().StringVar(&binPath, "path", "", "Directory containing the treeman binary to add to PATH")
	return cmd
}

func newShellUninstallCmd() *cobra.Command {
	var shell, configPath string
	var all bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove managed shell integration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if all {
				removed, err := uninstallAllShellIntegrations()
				if err != nil {
					return err
				}
				if removed == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "TreeMan shell integration is not installed.")
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "Removed TreeMan shell integration from %d file(s).\n", removed)
				}
				return nil
			}
			cfg, err := resolveShellConfig(shell, configPath)
			if err != nil {
				return err
			}
			removed, err := uninstallShellIntegration(cfg.path)
			if err != nil {
				return err
			}
			if removed {
				fmt.Fprintf(cmd.OutOrStdout(), "Removed TreeMan shell integration from %s\n", cfg.path)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "TreeMan shell integration is not installed in %s\n", cfg.path)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&shell, "shell", "", "Shell to configure: bash, zsh, or fish")
	cmd.Flags().StringVar(&configPath, "config", "", "Shell startup file to update")
	cmd.Flags().BoolVar(&all, "all", false, "Remove managed integration from all supported startup files")
	return cmd
}

func newShellStatusCmd() *cobra.Command {
	var shell, configPath string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show shell integration status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := resolveShellConfig(shell, configPath)
			if err != nil {
				return err
			}
			state, err := shellIntegrationState(cfg.path, cfg.name)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Shell integration: %s (%s)\n", state, cfg.path)
			return nil
		},
	}
	cmd.Flags().StringVar(&shell, "shell", "", "Shell to inspect: bash, zsh, or fish")
	cmd.Flags().StringVar(&configPath, "config", "", "Shell startup file to inspect")
	return cmd
}

func resolveShellConfig(shell, configPath string) (shellConfig, error) {
	if shell == "" {
		shell = filepath.Base(os.Getenv("SHELL"))
	}
	if shell != "bash" && shell != "zsh" && shell != "fish" {
		return shellConfig{}, fmt.Errorf("unsupported shell %q: use --shell bash, zsh, or fish", shell)
	}
	if configPath != "" {
		return shellConfig{name: shell, path: configPath}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return shellConfig{}, fmt.Errorf("resolve home directory: %w", err)
	}
	switch shell {
	case "bash":
		return shellConfig{name: shell, path: filepath.Join(home, ".bashrc")}, nil
	case "zsh":
		return shellConfig{name: shell, path: filepath.Join(home, ".zshrc")}, nil
	default:
		configHome := os.Getenv("XDG_CONFIG_HOME")
		if configHome == "" {
			configHome = filepath.Join(home, ".config")
		}
		return shellConfig{name: shell, path: filepath.Join(configHome, "fish", "config.fish")}, nil
	}
}

func installShellIntegration(cfg shellConfig, binPath string) error {
	if strings.ContainsAny(binPath, "\r\n") {
		return fmt.Errorf("PATH directory must not contain a newline")
	}
	if binPath != "" && !filepath.IsAbs(binPath) {
		return fmt.Errorf("PATH directory must be absolute")
	}
	contents, err := os.ReadFile(cfg.path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read shell config: %w", err)
	}
	pathLine := ""
	if binPath == "" {
		pathLine = managedShellPathLine(string(contents))
	} else {
		pathLine = shellPathLine(cfg.name, binPath)
	}
	block := managedShellBlock(cfg.name, pathLine)
	updated, found, err := replaceManagedShellBlock(string(contents), block)
	if err != nil {
		return err
	}
	if found && string(contents) == updated {
		return nil
	}
	return writeShellConfig(cfg.path, updated)
}

func managedShellBlock(shell, pathLine string) string {
	initLine := fmt.Sprintf("eval \"$(treeman shell init %s)\"", shell)
	if shell == "fish" {
		initLine = "treeman shell init fish | source"
	}
	lines := []string{shellBlockStart}
	if pathLine != "" {
		lines = append(lines, pathLine)
	}
	lines = append(lines, initLine, shellBlockEnd, "")
	return strings.Join(lines, "\n")
}

func shellPathLine(shell, binPath string) string {
	if binPath == "" {
		return ""
	}
	if shell == "fish" {
		return fmt.Sprintf("set -gx PATH %s $PATH", fishQuote(binPath))
	}
	return fmt.Sprintf("export PATH=\"%s:$PATH\"", posixPathQuote(binPath))
}

func managedShellPathLine(contents string) string {
	start := strings.Index(contents, shellBlockStart)
	end := strings.Index(contents, shellBlockEnd)
	if start == -1 || end == -1 || end < start {
		return ""
	}
	for _, line := range strings.Split(contents[start:end], "\n") {
		if strings.HasPrefix(line, "export PATH=") || strings.HasPrefix(line, "set -gx PATH ") {
			return line
		}
	}
	return ""
}

func replaceManagedShellBlock(contents, block string) (string, bool, error) {
	start := strings.Index(contents, shellBlockStart)
	end := strings.Index(contents, shellBlockEnd)
	if start == -1 && end != -1 || start != -1 && end == -1 || end != -1 && end < start {
		return "", false, fmt.Errorf("TreeMan shell integration block is malformed; repair it manually")
	}
	if start != -1 {
		blockEnd := end + len(shellBlockEnd)
		if blockEnd < len(contents) && contents[blockEnd] == '\n' {
			blockEnd++
		}
		return contents[:start] + block + contents[blockEnd:], true, nil
	}
	if contents != "" && !strings.HasSuffix(contents, "\n") {
		contents += "\n"
	}
	if contents != "" {
		contents += "\n"
	}
	return contents + block, false, nil
}

func uninstallShellIntegration(path string) (bool, error) {
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read shell config: %w", err)
	}
	start := strings.Index(string(contents), shellBlockStart)
	end := strings.Index(string(contents), shellBlockEnd)
	if start == -1 && end == -1 {
		return false, nil
	}
	if start == -1 || end == -1 || end < start {
		return false, fmt.Errorf("TreeMan shell integration block is malformed; repair it manually")
	}
	blockEnd := end + len(shellBlockEnd)
	if blockEnd < len(contents) && contents[blockEnd] == '\n' {
		blockEnd++
	}
	return true, writeShellConfig(path, string(contents[:start])+string(contents[blockEnd:]))
}

func uninstallAllShellIntegrations() (int, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, fmt.Errorf("resolve home directory: %w", err)
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	paths := []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".bash_profile"),
		filepath.Join(home, ".zshrc"),
		filepath.Join(configHome, "fish", "config.fish"),
	}
	removed := 0
	for _, path := range paths {
		didRemove, err := uninstallShellIntegration(path)
		if err != nil {
			return removed, err
		}
		if didRemove {
			removed++
		}
	}
	return removed, nil
}

func shellIntegrationState(path, shell string) (string, error) {
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "not installed", nil
	}
	if err != nil {
		return "", fmt.Errorf("read shell config: %w", err)
	}
	start := strings.Index(string(contents), shellBlockStart)
	end := strings.Index(string(contents), shellBlockEnd)
	if start != -1 || end != -1 {
		if start == -1 || end == -1 || end < start {
			return "malformed", nil
		}
		return "installed", nil
	}
	if hasLegacyShellIntegration(string(contents), shell) {
		return "legacy", nil
	}
	return "not installed", nil
}

func writeShellConfig(path, contents string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create shell config directory: %w", err)
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect shell config: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".treeman-shell-*")
	if err != nil {
		return fmt.Errorf("create shell config temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(contents); err != nil {
		tmp.Close()
		return fmt.Errorf("write shell config: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return fmt.Errorf("set shell config permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close shell config: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace shell config: %w", err)
	}
	return nil
}

func posixPathQuote(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "$", "\\$", "`", "\\`")
	return replacer.Replace(value)
}

func fishQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "\\'") + "'"
}

func hasLegacyShellIntegration(contents, shell string) bool {
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if line == fmt.Sprintf("eval \"$(treeman init %s)\"", shell) ||
			line == fmt.Sprintf("eval \"$(treeman shell init %s)\"", shell) ||
			line == fmt.Sprintf("treeman init %s | source", shell) {
			return true
		}
	}
	return false
}
