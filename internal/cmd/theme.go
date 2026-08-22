package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shoutcape/treeman/internal/state"
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
		name, err = pickTheme(cmd)
		if err != nil {
			if err == errPickerCancelled {
				fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneMuted, "○", "Cancelled."))
				return nil
			}
			return err
		}
	}
	if !ui.HasTheme(name) {
		return fmt.Errorf("unknown theme %q; choose one of: %s", name, strings.Join(ui.ThemeNames(), ", "))
	}
	ui.SetTheme(name)

	if err := state.SaveTheme(ui.CurrentTheme()); err != nil {
		return err
	}
	fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneSuccess, "✓", "Theme set to "+ui.CurrentTheme()))
	return nil
}

func pickTheme(cmd *cobra.Command) (string, error) {
	capabilities := sessionFor(cmd).errorOutput
	if !capabilities.Interactive {
		return "", fmt.Errorf("interactive theme selection is unavailable; use `treeman theme list` and `treeman theme set <name>`")
	}
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
		label := name
		if capabilities.RichUI {
			label = ui.ThemePickerRow(name, name == ui.CurrentTheme())
		}
		rows = append(rows, strings.Join([]string{name, label, fmt.Sprint(i)}, "\t"))
	}

	executable := ""
	var err error
	if capabilities.RichUI {
		executable, err = os.Executable()
		if err != nil {
			return "", fmt.Errorf("could not locate treeman for theme preview: %w", err)
		}
	}
	args := themePickerArgs(capabilities.RichUI, executable)
	fzfCmd := exec.Command("fzf", args...)
	fzfCmd.Stdin = strings.NewReader(strings.Join(rows, "\n"))
	fzfCmd.Stderr = cmd.ErrOrStderr()
	selectionOutput, err := fzfCmd.Output()
	if err != nil {
		if pickerCancelled(err) {
			return "", errPickerCancelled
		}
		return "", fmt.Errorf("fzf failed while selecting a theme: %w", err)
	}
	index := pickerSelectionIndex(strings.TrimSpace(string(selectionOutput)), len(names))
	if index < 0 {
		return "", fmt.Errorf("could not map fzf selection to a theme")
	}
	return names[index], nil
}

func themePickerArgs(richUI bool, executable string) []string {
	args := []string{
		"--no-sort", "--height=40%", "--layout=reverse", "--border=rounded", "--delimiter=\t", "--with-nth=2", "--border-label= themes ", "--prompt=theme > ",
	}
	if richUI {
		return append(args,
			"--ansi", "--color="+ui.TransparentFZFColors(),
			"--preview="+shellQuote(executable)+" theme preview {1}", "--preview-window=right:55%:wrap",
		)
	}
	return append(args, "--no-color")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(filepath.Clean(value), "'", "'\\''") + "'"
}
