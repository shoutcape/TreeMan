package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/database"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/terminal"
	_ "github.com/shoutcape/treeman/internal/terminal/ghostty"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/shoutcape/treeman/internal/worktree"
	"github.com/spf13/cobra"
)

func newDeleteCmd() *cobra.Command {
	var flagPath string
	var flagBranch string
	var flagYes bool
	var flagBackground bool

	cmd := &cobra.Command{
		Use:   "delete [query]",
		Short: "Delete a worktree and its branch via fzf",
		Long: `Open an interactive fzf picker listing all deletable worktrees.

The main worktree and the default branch are protected from deletion.
An optional query pre-filters the list.

After confirmation, the selected worktree is removed and its branch is
deleted with git branch -D. Deletion runs in the background so the
command returns immediately.

Non-interactive mode (for lazygit / scripts):
  treeman delete --path <path> --branch <branch> --yes`,
		Aliases: []string{"wtd"},
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Background mode: we are the detached child, do the actual work.
			if flagBackground {
				return runDeleteBackground(flagPath, flagBranch)
			}

			// Non-interactive mode: --path + --branch provided directly.
			if flagPath != "" || flagBranch != "" {
				return runDeleteDirect(flagPath, flagBranch, flagYes)
			}

			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			return runDelete(cmd, query, flagYes)
		},
	}

	cmd.Flags().StringVar(&flagPath, "path", "", "Worktree path to delete (skips fzf picker)")
	cmd.Flags().StringVar(&flagBranch, "branch", "", "Branch to delete (skips fzf picker)")
	cmd.Flags().BoolVarP(&flagYes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&flagBackground, "background", false, "Run deletion in background (internal flag)")
	cmd.Flags().MarkHidden("background")

	return cmd
}

// runDeleteDirect deletes a worktree by explicit path + branch, used by
// lazygit keybindings where fzf is not available and targets are known.
func runDeleteDirect(path, branch string, skipConfirm bool) error {
	if path == "" || branch == "" {
		return fmt.Errorf("--path and --branch are both required in non-interactive mode")
	}

	if !git.IsInsideRepo() {
		return fmt.Errorf("not inside a git repository")
	}

	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return err
	}

	if !skipConfirm {
		fmt.Fprintf(os.Stderr, "About to delete:\n")
		fmt.Fprintf(os.Stderr, "  Worktree: %s\n", path)
		fmt.Fprintf(os.Stderr, "  Branch:   %s\n", branch)
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprint(os.Stderr, "Are you sure? [y/N] ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			if !strings.EqualFold(strings.TrimSpace(scanner.Text()), "y") {
				fmt.Fprintln(os.Stderr, "Cancelled.")
				return nil
			}
		}
	}

	return spawnBackgroundDelete(path, branch, mainRoot)
}

func runDelete(cmd *cobra.Command, query string, skipConfirm bool) error {
	if _, err := exec.LookPath("fzf"); err != nil {
		return fmt.Errorf("fzf is required for delete. Install it from https://github.com/junegunn/fzf")
	}

	if !git.IsInsideRepo() {
		return fmt.Errorf("not inside a git repository")
	}

	entries, err := git.WorktreeList()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("no worktrees found")
	}
	if len(entries) == 1 {
		fmt.Fprintln(os.Stderr, "Only one worktree exists -- nothing to delete.")
		return nil
	}

	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return err
	}

	// Build display rows, excluding the main worktree.
	var displayLines []string
	var fullPaths []string
	var branches []string

	for _, e := range entries {
		if e.Path == mainRoot {
			continue
		}
		displayLines = append(displayLines, ui.WorktreeRow(e.Path, e.Branch))
		fullPaths = append(fullPaths, e.Path)
		branches = append(branches, e.Branch)
	}

	if len(displayLines) == 0 {
		fmt.Fprintln(os.Stderr, "No deletable worktrees -- only the main worktree exists.")
		return nil
	}

	display := strings.Join(displayLines, "\n")

	fzfArgs := []string{
		"--ansi",
		"--border-label", " delete worktree ",
		"--prompt=delete > ",
		"--select-1",
		"--exit-0",
	}
	if query != "" {
		fzfArgs = append(fzfArgs, "--query", query)
	}

	fzfCmd := exec.Command("fzf", fzfArgs...)
	fzfCmd.Stdin = strings.NewReader(display)
	fzfCmd.Stderr = os.Stderr

	out, err := fzfCmd.Output()
	if err != nil {
		// User cancelled.
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
	branch := branches[idx]

	// Confirm deletion (unless --yes was passed).
	fmt.Fprintf(os.Stderr, "About to delete:\n")
	fmt.Fprintf(os.Stderr, "  Worktree: %s\n", dest)
	fmt.Fprintf(os.Stderr, "  Branch:   %s\n", branch)
	fmt.Fprintln(os.Stderr, "")

	if !skipConfirm && !confirmYN(cmd, "Are you sure? [y/N] ") {
		fmt.Fprintln(os.Stderr, "Cancelled.")
		return nil
	}

	return spawnBackgroundDelete(dest, branch, mainRoot)
}

// spawnBackgroundDelete validates guards, then spawns a detached subprocess
// to perform the actual deletion. The parent returns immediately.
func spawnBackgroundDelete(dest, branch, mainRoot string) error {
	// Run guards before spawning so the user gets immediate feedback.
	if dest == mainRoot {
		return fmt.Errorf("cannot delete the main worktree")
	}

	defaultBranch, _ := git.DetectDefaultBranch()
	if defaultBranch != "" && branch == defaultBranch {
		return fmt.Errorf("cannot delete the default branch %q", branch)
	}

	// Find our own executable.
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not find treeman executable: %w", err)
	}

	// Spawn detached child process.
	child := exec.Command(self, "delete",
		"--path", dest,
		"--branch", branch,
		"--yes",
		"--background",
	)
	// Set working directory to mainRoot so git commands work.
	child.Dir = mainRoot
	// Detach from parent: no stdin/stdout/stderr, new process group.
	child.Stdin = nil
	child.Stdout = nil
	child.Stderr = nil
	child.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := child.Start(); err != nil {
		return fmt.Errorf("could not start background deletion: %w", err)
	}

	// Release the child so it isn't reaped when we exit.
	child.Process.Release()

	fmt.Fprintf(os.Stderr, "deleting: %s\n", branch)
	return nil
}

// runDeleteBackground performs the actual deletion in a background subprocess.
// Errors are written to a log file for reporting on the next treeman command.
func runDeleteBackground(dest, branch string) error {
	if !git.IsInsideRepo() {
		return logDeleteError(branch, "not inside a git repository")
	}

	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return logDeleteError(branch, err.Error())
	}

	// Load config for database/terminal cleanup.
	cfgResult := config.Load(mainRoot)
	dbEnvKey := cfgResult.Config.DatabaseEnvKey()
	termCfg := config.MergeTerminalConfig(
		config.LoadGlobal("").Config.Terminal,
		cfgResult.Config.Terminal,
	)

	// Drop branch-specific database (best-effort).
	if dbEnvKey != "" {
		if err := database.CleanupBranchDB(dest, dbEnvKey); err != nil {
			logDeleteError(branch, fmt.Sprintf("database cleanup failed: %v", err))
		}
	}

	// Close terminals for this worktree (best-effort).
	if mgr := terminal.NewManager(termCfg); mgr != nil {
		mgr.Close(terminal.WorktreeInfo{
			Path:   dest,
			Branch: branch,
			Slug:   worktree.BranchSlug(branch),
		})
	}

	// Remove worktree.
	if err := git.WorktreeRemove(dest); err != nil {
		// If directory is already gone, treat as success.
		if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
			return logDeleteError(branch, err.Error())
		}
	}

	// Delete branch.
	if err := git.DeleteBranch(branch); err != nil {
		if !strings.Contains(err.Error(), "not found") {
			return logDeleteError(branch, err.Error())
		}
	}

	return nil
}

// logDeleteError writes an error to the delete log file so it can be
// reported on the next treeman command.
func logDeleteError(branch, msg string) error {
	logPath := deleteLogPath()
	if logPath == "" {
		return fmt.Errorf("%s", msg)
	}

	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return fmt.Errorf("%s", msg)
	}

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("%s", msg)
	}
	defer f.Close()

	fmt.Fprintf(f, "delete %s: %s\n", branch, msg)
	return fmt.Errorf("%s", msg)
}

// deleteLogPath returns the path to the delete error log.
func deleteLogPath() string {
	dir := dataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "delete-errors.log")
}

// dataDir returns the treeman data directory, respecting $XDG_DATA_HOME.
func dataDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "treeman")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "treeman")
}

// reportDeleteErrors reads and displays any errors from background deletions,
// then clears the log. Called from root PersistentPreRunE.
func reportDeleteErrors() {
	logPath := deleteLogPath()
	if logPath == "" {
		return
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		return // no log file = no errors
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return
	}

	fmt.Fprintf(os.Stderr, "Background deletion error(s):\n")
	for _, line := range strings.Split(content, "\n") {
		fmt.Fprintf(os.Stderr, "  %s\n", line)
	}
	fmt.Fprintln(os.Stderr, "")

	// Clear the log.
	os.Remove(logPath)
}

// matchIndex returns the index in displayLines that matches the fzf selection,
// using ANSI-stripped comparison.
func matchIndex(displayLines []string, selection string) int {
	plainSelection := ui.StripANSI(strings.TrimSpace(selection))
	for i, line := range displayLines {
		if ui.StripANSI(line) == plainSelection {
			return i
		}
	}
	return -1
}

// confirmYN prints prompt and reads a y/Y response from stdin.
func confirmYN(cmd *cobra.Command, prompt string) bool {
	fmt.Fprint(os.Stderr, prompt)
	scanner := bufio.NewScanner(cmd.InOrStdin())
	if scanner.Scan() {
		answer := strings.TrimSpace(scanner.Text())
		return strings.EqualFold(answer, "y")
	}
	return false
}
