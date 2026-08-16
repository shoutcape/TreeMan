package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/database"
	"github.com/shoutcape/treeman/internal/deps"
	"github.com/shoutcape/treeman/internal/envfile"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/hooks"
	"github.com/shoutcape/treeman/internal/validate"
	"github.com/shoutcape/treeman/internal/worktree"
	"github.com/spf13/cobra"
)

const databaseDocsURL = "https://github.com/shoutcape/TreeMan/blob/main/docs/integrations/postgresql.md"

func newCreateCmd() *cobra.Command {
	var setupOptions creationSetupOptions
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
			return runCreate(cmd, args[0], setupOptions)
		},
	}
	addCreationSetupFlags(cmd, &setupOptions)

	return cmd
}

func runCreate(cmd *cobra.Command, branch string, setupOptions creationSetupOptions) error {
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
	environmentStatus := "skipped (requested)"
	if !setupOptions.skipEnv {
		result, err := envfile.Copy(mainRoot, worktreePath)
		environmentStatus = "skipped (no environment files found)"
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not copy env files: %v\n", err)
			environmentStatus = fmt.Sprintf("failed: %v", err)
		} else if len(result.Copied) > 0 {
			for _, f := range result.Copied {
				fmt.Fprintf(os.Stderr, "  Copied %s\n", f)
			}
			fmt.Fprintf(os.Stderr, "Copied %d env file(s) from main worktree.\n", len(result.Copied))
			environmentStatus = fmt.Sprintf("completed: copied %d file(s)", len(result.Copied))
		}
	}

	// Load project config for database management.
	var cfgResult config.LoadResult
	if !setupOptions.skipDatabase || !setupOptions.skipHooks {
		cfgResult = config.Load(mainRoot)
		if cfgResult.Warning != "" {
			fmt.Fprintf(os.Stderr, "Warning: %s\n", cfgResult.Warning)
		}
	}

	// Set up branch-specific database (best-effort, non-fatal).
	databaseStatus := "skipped (requested)"
	if !setupOptions.skipDatabase {
		dbEnvKey := cfgResult.Config.DatabaseEnvKey()
		databaseStatus = "skipped (database management not configured)"
		if dbEnvKey != "" {
			dbResult, dbErr := database.SetupBranchDB(worktreePath, branch, dbEnvKey)
			switch {
			case dbErr != nil:
				fmt.Fprintf(os.Stderr, "Warning: database setup failed: %v\n", dbErr)
				databaseStatus = fmt.Sprintf("failed: %v", dbErr)
			case dbResult.Skipped:
				databaseStatus = fmt.Sprintf("skipped (no PostgreSQL URI found for %s)", dbEnvKey)
			default:
				fmt.Fprintf(os.Stderr, "  Created database %s\n", dbResult.DBName)
				databaseStatus = fmt.Sprintf("completed: created %s", dbResult.DBName)
			}
		}
	}

	// Install dependencies.
	dependenciesStatus := "skipped (requested)"
	if !setupOptions.skipDeps {
		fmt.Fprintln(os.Stderr, "Detecting dependencies...")
		installResult, installErr := deps.Install(worktreePath)
		dependenciesStatus = "skipped"
		switch {
		case installErr != nil:
			fmt.Fprintf(os.Stderr, "Warning: dependency installation failed: %v\n", installErr)
			dependenciesStatus = fmt.Sprintf("failed: %v", installErr)
		case installResult.Python:
			fmt.Fprintln(os.Stderr, "Detected Python project — skipping auto-install (activate your venv manually).")
			dependenciesStatus = "skipped (Python project requires manual venv activation)"
		case installResult.Skipped:
			fmt.Fprintln(os.Stderr, "No known dependency file detected, skipping install.")
		case installResult.Installer != nil:
			fmt.Fprintf(os.Stderr, "Detected %s -- running %s %s...\n",
				installResult.Installer.Lockfile,
				installResult.Installer.Binary,
				joinArgs(installResult.Installer.Args),
			)
			dependenciesStatus = fmt.Sprintf("completed: installed with %s", installResult.Installer.Binary)
		}
	}

	// Run post-create hooks (best-effort, non-fatal).
	hooksStatus := "skipped (requested)"
	if !setupOptions.skipHooks {
		if postCreateCmds := cfgResult.Config.PostCreateHooks(); len(postCreateCmds) > 0 {
			fmt.Fprintf(os.Stderr, "Running %d post-create hook(s)...\n", len(postCreateCmds))
			hookResults := hooks.RunPostCreate(worktreePath, postCreateCmds)
			for _, r := range hookResults {
				if r.Err != nil {
					fmt.Fprintf(os.Stderr, "Warning: hook %q failed: %v\n", r.Command, r.Err)
				} else {
					fmt.Fprintf(os.Stderr, "  Ran: %s\n", r.Command)
				}
			}
			hooksStatus = summarizeHooks(hookResults)
		} else {
			hooksStatus = "skipped (no post-create hooks configured)"
		}
	}

	// Print result to stderr for the user.
	fmt.Fprintln(os.Stderr, "")
	printSetupSummary(os.Stderr, setupSummary{
		environment:  environmentStatus,
		dependencies: dependenciesStatus,
		database:     databaseStatus,
		hooks:        hooksStatus,
		databaseDocs: strings.HasPrefix(databaseStatus, "skipped"),
	})
	fmt.Fprintln(os.Stderr, "Worktree ready:")
	fmt.Fprintf(os.Stderr, "  Branch: %s\n", branch)
	fmt.Fprintf(os.Stderr, "  Path:   %s\n", worktreePath)
	setupOptions.printSkipped(os.Stderr)

	// Print path to stdout so the shell wrapper can cd into it.
	fmt.Fprintln(cmd.OutOrStdout(), worktreePath)

	return nil
}

type setupSummary struct {
	environment  string
	dependencies string
	database     string
	hooks        string
	databaseDocs bool
}

func printSetupSummary(w io.Writer, summary setupSummary) {
	fmt.Fprintln(w, "Setup:")
	fmt.Fprintf(w, "  Environment:  %s\n", summary.environment)
	fmt.Fprintf(w, "  Dependencies: %s\n", summary.dependencies)
	fmt.Fprintf(w, "  Database:     %s\n", summary.database)
	fmt.Fprintf(w, "  Hooks:        %s\n", summary.hooks)
	if summary.databaseDocs {
		fmt.Fprintf(w, "  Configure:    %s\n", databaseDocsURL)
	}
}

func summarizeHooks(results []hooks.RunResult) string {
	succeeded := 0
	var failures []string
	for _, result := range results {
		if result.Err != nil {
			failures = append(failures, fmt.Sprintf("%q: %v", result.Command, result.Err))
			continue
		}
		succeeded++
	}

	if len(failures) == 0 {
		return fmt.Sprintf("completed: %d succeeded", succeeded)
	}
	return fmt.Sprintf("completed: %d succeeded, %d failed: %s", succeeded, len(failures), strings.Join(failures, "; "))
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
