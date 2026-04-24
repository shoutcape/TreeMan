package cmd

import (
	"fmt"
	"os"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/database"
	"github.com/shoutcape/treeman/internal/deps"
	"github.com/shoutcape/treeman/internal/envfile"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/hooks"
	"github.com/shoutcape/treeman/internal/terminal"
	_ "github.com/shoutcape/treeman/internal/terminal/ghostty"
	"github.com/shoutcape/treeman/internal/validate"
	"github.com/shoutcape/treeman/internal/worktree"
	"github.com/spf13/cobra"
)

func newCreateCmd() *cobra.Command {
	var flagNoOpen bool

	cmd := &cobra.Command{
		Use:   "create <branch-name>",
		Short: "Create a new worktree + branch",
		Long: `Create a new linked worktree and branch from the latest default branch.

The worktree is placed under .worktrees/<branch-slug> inside the repository.

.env* files are automatically copied from the main worktree, and
dependencies are installed if a known lockfile is detected.

The path of the new worktree is printed to stdout so that a shell wrapper
can cd into it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreate(cmd, args[0], flagNoOpen)
		},
	}

	cmd.Flags().BoolVarP(&flagNoOpen, "no-open", "n", false, "Skip opening a terminal tab/pane")

	return cmd
}

func runCreate(cmd *cobra.Command, branch string, noOpen bool) error {
	// Validate branch name.
	if err := validate.BranchName(branch); err != nil {
		return err
	}

	// Must be inside a git repo.
	if !git.IsInsideRepo() {
		return fmt.Errorf("not inside a git repository")
	}

	// Main worktree root.
	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return err
	}

	// Resolve default branch.
	defaultBranch, err := git.DetectDefaultBranch()
	if err != nil {
		return err
	}

	// Guard: branch must not already exist.
	if git.BranchExists(branch) {
		return fmt.Errorf("branch %q already exists locally", branch)
	}

	// Fetch latest default branch.
	fmt.Fprintf(os.Stderr, "Fetching latest %s from origin...\n", defaultBranch)
	if err := git.Fetch(defaultBranch); err != nil {
		return err
	}

	// Build worktree path.
	worktreePath := worktree.PathForBranch(mainRoot, branch)

	// Guard: directory must not already exist.
	if _, err := os.Stat(worktreePath); err == nil {
		return fmt.Errorf("directory %q already exists", worktreePath)
	}

	// Create worktree + branch.
	fmt.Fprintf(os.Stderr, "Creating worktree at %s (branch: %s)...\n", worktreePath, branch)
	if err := git.WorktreeAdd(worktreePath, branch, "origin/"+defaultBranch); err != nil {
		return err
	}

	// Ensure .worktrees/ is gitignored (best-effort, non-fatal).
	if err := worktree.EnsureIgnored(mainRoot); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not update .gitignore: %v\n", err)
	}

	// Copy .env* files (best-effort, non-fatal).
	result, err := envfile.Copy(mainRoot, worktreePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not copy env files: %v\n", err)
	} else if len(result.Copied) > 0 {
		for _, f := range result.Copied {
			fmt.Fprintf(os.Stderr, "  Copied %s\n", f)
		}
		fmt.Fprintf(os.Stderr, "Copied %d env file(s) from main worktree.\n", len(result.Copied))
	}

	// Load project config for database management.
	cfgResult := config.Load(mainRoot)
	if cfgResult.Warning != "" {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", cfgResult.Warning)
	}

	// Set up branch-specific database (best-effort, non-fatal).
	dbEnvKey := cfgResult.Config.DatabaseEnvKey()
	dbResult, dbErr := database.SetupBranchDB(worktreePath, branch, dbEnvKey)
	switch {
	case dbErr != nil:
		fmt.Fprintf(os.Stderr, "Warning: database setup failed: %v\n", dbErr)
	case dbResult.Skipped:
		// No config, no env key, or not a postgres URI -- silently skip.
	default:
		fmt.Fprintf(os.Stderr, "  Created database %s\n", dbResult.DBName)
	}

	// Install dependencies.
	fmt.Fprintln(os.Stderr, "Detecting dependencies...")
	installResult, installErr := deps.Install(worktreePath)
	switch {
	case installErr != nil:
		fmt.Fprintf(os.Stderr, "Warning: dependency installation failed: %v\n", installErr)
	case installResult.Python:
		fmt.Fprintln(os.Stderr, "Detected Python project — skipping auto-install (activate your venv manually).")
	case installResult.Skipped:
		fmt.Fprintln(os.Stderr, "No known dependency file detected, skipping install.")
	case installResult.Installer != nil:
		fmt.Fprintf(os.Stderr, "Detected %s -- running %s %s...\n",
			installResult.Installer.Lockfile,
			installResult.Installer.Binary,
			joinArgs(installResult.Installer.Args),
		)
	}

	// Run post-create hooks (best-effort, non-fatal).
	if postCreateCmds := cfgResult.Config.PostCreateHooks(); len(postCreateCmds) > 0 {
		fmt.Fprintf(os.Stderr, "Running %d post-create hook(s)...\n", len(postCreateCmds))
		for _, r := range hooks.RunPostCreate(worktreePath, postCreateCmds) {
			if r.Err != nil {
				fmt.Fprintf(os.Stderr, "Warning: hook %q failed: %v\n", r.Command, r.Err)
			} else {
				fmt.Fprintf(os.Stderr, "  Ran: %s\n", r.Command)
			}
		}
	}

	// Open terminal for the new worktree (best-effort).
	terminalOpened := false
	if !noOpen {
		termCfg := config.MergeTerminalConfig(
			config.LoadGlobal("").Config.Terminal,
			cfgResult.Config.Terminal,
		)
		if mgr := terminal.NewManager(termCfg); mgr != nil {
			fmt.Fprintln(os.Stderr, "Opening Ghostty terminal...")
			if err := mgr.Open(terminal.WorktreeInfo{
				Path:   worktreePath,
				Branch: branch,
				Slug:   worktree.BranchSlug(branch),
			}); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not open terminal: %v\n", err)
			} else {
				terminalOpened = true
			}
		}
	}

	// Print result to stderr for the user.
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Worktree ready:")
	fmt.Fprintf(os.Stderr, "  Branch: %s\n", branch)
	fmt.Fprintf(os.Stderr, "  Path:   %s\n", worktreePath)

	// Print path to stdout so the shell wrapper can cd into it.
	// Skip when a terminal was opened -- the user is already there.
	if !terminalOpened {
		fmt.Fprintln(cmd.OutOrStdout(), worktreePath)
	}

	return nil
}

func joinArgs(args []string) string {
	result := ""
	for i, a := range args {
		if i > 0 {
			result += " "
		}
		result += a
	}
	return result
}
