package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/deps"
	"github.com/shoutcape/treeman/internal/envfile"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/hooks"
	"github.com/shoutcape/treeman/internal/ui"
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
	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
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
	fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", fmt.Sprintf("Fetching latest %s from origin...", defaultBranch)))
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
	fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", fmt.Sprintf("Creating worktree at %s (branch: %s)...", worktreePath, branch)))
	if err := git.WorktreeAdd(worktreePath, branch, "origin/"+defaultBranch); err != nil {
		return err
	}

	// Load project config (needed for gitignore, database, and hooks).
	cfgResult := config.Load(mainRoot)
	if cfgResult.Warning != "" {
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", cfgResult.Warning))
	}

	// Ensure .worktrees/ is gitignored if opted in (best-effort, non-fatal).
	if cfgResult.Config.ShouldUpdateGitignore() {
		if err := worktree.EnsureIgnored(mainRoot); err != nil {
			fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("could not update .gitignore: %v", err)))
		}
	}

	// Copy .env* files (best-effort, non-fatal).
	environmentStatus := "skipped (requested)"
	if !setupOptions.skipEnv {
		result, err := envfile.Copy(mainRoot, worktreePath)
		environmentStatus = "skipped (no environment files found)"
		if err != nil {
			fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("could not copy env files: %v", err)))
			environmentStatus = fmt.Sprintf("failed: %v", err)
		} else if len(result.Copied) > 0 {
			for _, f := range result.Copied {
				fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Copied "+f))
			}
			fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", fmt.Sprintf("Copied %d env file(s) from main worktree.", len(result.Copied))))
			environmentStatus = fmt.Sprintf("completed: copied %d file(s)", len(result.Copied))
		}
	}

	// Set up branch-specific database (best-effort, non-fatal).
	databaseStatus := "skipped (requested)"
	if !setupOptions.skipDatabase {
		databaseStatus = setupCreatedDatabase(out, render, cfgResult.Config, worktreePath, branch)
	}

	// Install dependencies.
	dependenciesStatus := "skipped (requested)"
	if !setupOptions.skipDeps {
		fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", "Detecting dependencies..."))
		installResult, installErr := deps.Install(worktreePath, out)
		dependenciesStatus = "skipped"
		switch {
		case installErr != nil:
			fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("dependency installation failed: %v", installErr)))
			dependenciesStatus = fmt.Sprintf("failed: %v", installErr)
		case installResult.Python:
			fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "Detected Python project, skipping auto-install (activate your venv manually)."))
			dependenciesStatus = "skipped (Python project requires manual venv activation)"
		case installResult.Skipped:
			fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "No known dependency file detected, skipping install."))
		case installResult.Installer != nil:
			fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", fmt.Sprintf("Detected %s, running %s %s...",
				installResult.Installer.Lockfile,
				installResult.Installer.Binary,
				joinArgs(installResult.Installer.Args),
			)))
			dependenciesStatus = fmt.Sprintf("completed: installed with %s", installResult.Installer.Binary)
		}
	}

	// Run post-create hooks (best-effort, non-fatal).
	hooksStatus := "skipped (requested)"
	if !setupOptions.skipHooks {
		if postCreateCmds := cfgResult.Config.PostCreateHooks(); len(postCreateCmds) > 0 {
			fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", fmt.Sprintf("Running %d post-create hook(s)...", len(postCreateCmds))))
			hookResults := hooks.RunPostCreate(worktreePath, postCreateCmds, out)
			for _, r := range hookResults {
				if r.Err != nil {
					fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("hook %q failed: %v", r.Command, r.Err)))
				} else {
					fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Ran: "+r.Command))
				}
			}
			hooksStatus = summarizeHooks(hookResults)
		} else {
			hooksStatus = "skipped (no post-create hooks configured)"
		}
	}

	// Print result to stderr for the user.
	fmt.Fprintln(out, "")
	printSetupSummary(out, render, setupSummary{
		environment:  environmentStatus,
		dependencies: dependenciesStatus,
		database:     databaseStatus,
		hooks:        hooksStatus,
		databaseDocs: strings.HasPrefix(databaseStatus, "skipped"),
	})
	fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Worktree ready:"))
	fmt.Fprintf(out, "  Branch: %s\n", render.Branch(render.Fit(branch, 10)))
	fmt.Fprintf(out, "  Path:   %s\n", render.Path(render.Fit(worktreePath, 10)))
	setupOptions.printSkipped(out, render)

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

func printSetupSummary(w io.Writer, render ui.Renderer, summary setupSummary) {
	fmt.Fprintln(w, render.Title("SETUP"))
	writeSetupStatus(w, render, "Environment", summary.environment)
	writeSetupStatus(w, render, "Dependencies", summary.dependencies)
	if summary.databaseDocs {
		fmt.Fprintf(w, "  %s  %-14s %s\n", render.Tone(ui.ToneMuted, "○"), "Database", render.Link(render.Fit("Not configured. Configure database", 20), databaseDocsURL))
	} else {
		writeSetupStatus(w, render, "Database", summary.database)
	}
	writeSetupStatus(w, render, "Hooks", summary.hooks)
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
