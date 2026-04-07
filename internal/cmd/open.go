package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/terminal"
	_ "github.com/shoutcape/treeman/internal/terminal/ghostty"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/shoutcape/treeman/internal/worktree"
	"github.com/spf13/cobra"
)

func newOpenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "open [query]",
		Short: "Open a worktree in a terminal with configured layout",
		Long: `Open an interactive fzf picker listing all worktrees.

An optional query pre-filters the list.

The selected worktree is opened in the configured terminal emulator
with the configured layout splits. If a terminal is already open for
the selected worktree, it is focused instead.

This command does not cd in the current shell -- it only controls
the terminal emulator.`,
		Aliases: []string{"wto"},
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			return runOpen(query)
		},
	}
}

func runOpen(query string) error {
	if !git.IsInsideRepo() {
		return fmt.Errorf("not inside a git repository")
	}

	entries, err := git.WorktreeList()
	if err != nil {
		return fmt.Errorf("not in a git repository or no worktrees found")
	}
	if len(entries) == 0 {
		return fmt.Errorf("no worktrees found")
	}

	if _, err := exec.LookPath("fzf"); err != nil {
		return fmt.Errorf("fzf is required for open. Install it from https://github.com/junegunn/fzf")
	}

	// Build parallel display-rows and entry slices.
	var displayLines []string
	var fullPaths []string
	var branchList []string
	for _, e := range entries {
		displayLines = append(displayLines, ui.WorktreeRow(e.Path, e.Branch))
		fullPaths = append(fullPaths, e.Path)
		branchList = append(branchList, e.Branch)
	}

	display := strings.Join(displayLines, "\n")

	fzfArgs := []string{
		"--ansi",
		"--border-label", " open worktree ",
		"--prompt=open > ",
	}
	if query != "" {
		fzfArgs = append(fzfArgs, "--query", query)
	}

	fzfCmd := exec.Command("fzf", fzfArgs...)
	fzfCmd.Stdin = strings.NewReader(display)
	fzfCmd.Stderr = os.Stderr

	out, err := fzfCmd.Output()
	if err != nil {
		// User cancelled -- not an error.
		return nil
	}

	selection := strings.TrimSpace(string(out))
	if selection == "" {
		return nil
	}

	// Map selection back to path + branch.
	idx := matchIndex(displayLines, selection)
	if idx < 0 {
		return fmt.Errorf("could not map fzf selection to a worktree")
	}
	dest := fullPaths[idx]
	branch := branchList[idx]

	// Load terminal config.
	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return err
	}

	cfgResult := config.Load(mainRoot)
	if cfgResult.Warning != "" {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", cfgResult.Warning)
	}

	termCfg := config.MergeTerminalConfig(
		config.LoadGlobal("").Config.Terminal,
		cfgResult.Config.Terminal,
	)

	mgr := terminal.NewManager(termCfg)
	if mgr == nil {
		return fmt.Errorf("no terminal integration configured -- set [terminal] in .treeman.toml or ~/.config/treeman/config.toml")
	}

	wtInfo := terminal.WorktreeInfo{
		Path:   dest,
		Branch: branch,
		Slug:   worktree.BranchSlug(branch),
	}

	// Try to focus an existing terminal first.
	focused, err := mgr.Focus(wtInfo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not focus terminal: %v\n", err)
	}
	if focused {
		fmt.Fprintf(os.Stderr, "Focused existing terminal for %s\n", branch)
		return nil
	}

	// No existing terminal -- open a new one with layout.
	fmt.Fprintf(os.Stderr, "Opening terminal for %s...\n", branch)
	if err := mgr.Open(wtInfo); err != nil {
		return fmt.Errorf("could not open terminal: %w", err)
	}

	return nil
}
