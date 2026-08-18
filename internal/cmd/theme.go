package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
)

func newThemeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "theme",
		Short: "Select a terminal color theme",
		RunE: func(cmd *cobra.Command, args []string) error {
			return setTheme(cmd, "")
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "set [theme]",
		Short: "Select and save a terminal color theme",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			return setTheme(cmd, name)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "current",
		Short: "Print the active terminal color theme",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), ui.CurrentTheme())
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List available terminal color themes",
		Run: func(cmd *cobra.Command, args []string) {
			for _, name := range ui.ThemeNames() {
				marker := " "
				if name == ui.CurrentTheme() {
					marker = "*"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", marker, name)
			}
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:    "preview <theme>",
		Short:  "Preview a terminal color theme",
		Args:   cobra.ExactArgs(1),
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			preview, ok := ui.ThemePreview(args[0])
			if !ok {
				return fmt.Errorf("unknown theme %q", args[0])
			}
			fmt.Fprintln(cmd.OutOrStdout(), preview)
			return nil
		},
	})
	return cmd
}

func setTheme(cmd *cobra.Command, name string) error {
	var err error
	if name == "" {
		name, err = pickTheme()
		if err != nil {
			if err == errPickerCancelled {
				fmt.Fprintln(os.Stderr, "Cancelled.")
				return nil
			}
			return err
		}
	}
	if !ui.HasTheme(name) {
		return fmt.Errorf("unknown theme %q; choose one of: %s", name, strings.Join(ui.ThemeNames(), ", "))
	}
	ui.SetTheme(name)

	root, err := git.MainWorktreeRoot()
	if err != nil {
		return fmt.Errorf("themes are configured per repository: %w", err)
	}
	if _, err := config.SaveTheme(root, ui.CurrentTheme()); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, ui.RenderStatus(ui.ToneSuccess, "✓", "Theme set to "+ui.CurrentTheme()))
	return nil
}

func pickTheme() (string, error) {
	if _, err := exec.LookPath("fzf"); err != nil {
		return "", fmt.Errorf("fzf is required to select a theme. Install it from https://github.com/junegunn/fzf")
	}
	names := ui.ThemeNames()
	for i, name := range names {
		if name == ui.CurrentTheme() {
			// Keep the active theme under fzf's initial cursor without filtering.
			names = append([]string{name}, append(names[:i], names[i+1:]...)...)
			break
		}
	}
	var rows []string
	for i, name := range names {
		rows = append(rows, strings.Join([]string{name, ui.ThemePickerRow(name, name == ui.CurrentTheme()), fmt.Sprint(i)}, "\t"))
	}

	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("could not locate treeman for theme preview: %w", err)
	}
	args := []string{
		"--ansi", "--no-sort", "--height=40%", "--layout=reverse", "--border=rounded", "--delimiter=\t", "--with-nth=2", "--border-label= themes ", "--prompt=theme > ",
		"--color=" + ui.TransparentFZFColors(),
		"--preview=" + shellQuote(executable) + " theme preview {1}", "--preview-window=right:55%:wrap",
	}
	fzfCmd := exec.Command("fzf", args...)
	fzfCmd.Stdin = strings.NewReader(strings.Join(rows, "\n"))
	fzfCmd.Stderr = os.Stderr
	out, err := fzfCmd.Output()
	if err != nil {
		if pickerCancelled(err) {
			return "", errPickerCancelled
		}
		return "", fmt.Errorf("fzf failed while selecting a theme: %w", err)
	}
	index := pickerSelectionIndex(strings.TrimSpace(string(out)), len(names))
	if index < 0 {
		return "", fmt.Errorf("could not map fzf selection to a theme")
	}
	return names[index], nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(filepath.Clean(value), "'", "'\\''") + "'"
}
