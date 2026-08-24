package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/database"
	"github.com/shoutcape/treeman/internal/deps"
	"github.com/shoutcape/treeman/internal/envfile"
	"github.com/shoutcape/treeman/internal/forge"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/hooks"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/shoutcape/treeman/internal/validate"
	"github.com/shoutcape/treeman/internal/worktree"
	"github.com/spf13/cobra"
)

func newReviewCmd() *cobra.Command {
	var setupOptions creationSetupOptions
	cmd := &cobra.Command{
		Use:   "review [pr-number]",
		Short: "Create a review worktree from a GitHub PR or GitLab MR",
		Long: `Fetch a PR or MR and create a linked worktree for it.

If pr-number is omitted, an interactive fzf picker lists all open PRs/MRs.

Supports GitHub (gh CLI) and GitLab (glab CLI), including self-hosted GitLab
instances.

The path of the new worktree is printed to stdout so that a shell wrapper
can cd into it.`,
		Aliases: []string{"wtpr", "wtmr"},
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var prArg string
			if len(args) > 0 {
				prArg = args[0]
			}
			return runReview(cmd, prArg, setupOptions)
		},
	}
	addCreationSetupFlags(cmd, &setupOptions)
	return cmd
}

func runReview(cmd *cobra.Command, prArg string, setupOptions creationSetupOptions) error {
	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	if !git.IsInsideRepo() {
		return fmt.Errorf("not inside a git repository")
	}

	// Detect forge from origin remote.
	remoteURL, err := git.OriginRemoteURL()
	if err != nil {
		return err
	}

	forgeType, repoSlug, host, err := forge.ResolveFromRemote(remoteURL)
	if err != nil {
		return err
	}

	// Ensure the CLI tool for this forge is available.
	cliTool := forge.CLITool(forgeType)
	if _, err := exec.LookPath(cliTool); err != nil {
		return fmt.Errorf("%s is required for review with %s repos. Install it from %s",
			cliTool, forgeType, cliInstallURL(forgeType))
	}

	// Resolve PR number — prompt via fzf if not provided.
	var prNumber int
	if prArg == "" {
		prNumber, err = pickPRNumber(cmd, forgeType, repoSlug, host)
		if err != nil {
			if errors.Is(err, errPickerCancelled) {
				fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "Cancelled."))
				return nil
			}
			return err
		}
	} else {
		prNumber, err = validate.PRNumber(prArg)
		if err != nil {
			return fmt.Errorf("usage: treeman review [pr-number]\n%w", err)
		}
	}

	// Fetch PR/MR metadata.
	info, err := forge.PRMetadata(forgeType, repoSlug, host, prNumber)
	if err != nil {
		return fmt.Errorf("failed to resolve PR/MR #%d with %s: %w", prNumber, cliTool, err)
	}

	if info.Branch == "" {
		return fmt.Errorf("incomplete PR/MR metadata returned by %s", cliTool)
	}

	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return err
	}

	worktreePath := worktree.PathForBranch(mainRoot, info.Branch)

	// Guard: branch must not already exist locally.
	if git.BranchExists(info.Branch) {
		existing, _ := git.FindWorktreeForBranch(info.Branch)
		if existing != "" {
			return fmt.Errorf("branch %q already has a worktree at %q", info.Branch, existing)
		}
		return fmt.Errorf("PR/MR head branch %q already exists locally", info.Branch)
	}

	// Guard: directory must not exist.
	if _, err := os.Stat(worktreePath); err == nil {
		return fmt.Errorf("directory %q already exists for branch %q", worktreePath, info.Branch)
	}

	// Fetch the PR/MR ref.
	fetchRef := forge.FetchRef(forgeType, info.Number)
	fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", fmt.Sprintf("Fetching PR/MR #%d from origin...", info.Number)))
	if err := git.Fetch(fetchRef); err != nil {
		return err
	}

	// Create the worktree.
	fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", fmt.Sprintf("Creating review worktree at %s (branch: %s)...", worktreePath, info.Branch)))
	if err := git.WorktreeAdd(worktreePath, info.Branch, "FETCH_HEAD"); err != nil {
		return err
	}

	// Fetch the branch by name so origin/<branch> remote-tracking ref exists,
	// then set upstream so git pull/push work without explicit remote args.
	// Non-fatal: fork PRs may not have the branch on origin.
	if err := git.Fetch(info.Branch); err != nil {
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("could not fetch branch %q (upstream not set): %v", info.Branch, err)))
	} else if err := git.SetUpstreamInDir(worktreePath, info.Branch); err != nil {
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("could not set upstream for %q: %v", info.Branch, err)))
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

	// Copy .env* files.
	if !setupOptions.skipEnv {
		envResult, envErr := envfile.Copy(mainRoot, worktreePath)
		if envErr != nil {
			fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("could not copy env files: %v", envErr)))
		} else if len(envResult.Copied) > 0 {
			for _, f := range envResult.Copied {
				fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Copied "+f))
			}
			fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", fmt.Sprintf("Copied %d env file(s) from main worktree.", len(envResult.Copied))))
		}
	}

	// Set up branch-specific database (best-effort, non-fatal).
	if !setupOptions.skipDatabase {
		dbEnvKey := cfgResult.Config.DatabaseEnvKey()
		dbResult, dbErr := database.SetupBranchDB(worktreePath, info.Branch, dbEnvKey, cfgResult.Config.DatabaseContainer())
		switch {
		case dbErr != nil:
			fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("database setup failed: %v", dbErr)))
		case dbResult.Skipped:
			// No config, no env key, or not a postgres URI -- silently skip.
		default:
			fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Created database "+dbResult.DBName))
		}
	}

	// Install dependencies.
	if !setupOptions.skipDeps {
		fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", "Detecting dependencies..."))
		installResult, installErr := deps.Install(worktreePath, out)
		switch {
		case installErr != nil:
			fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("dependency installation failed: %v", installErr)))
		case installResult.Python:
			fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "Detected Python project, skipping auto-install (activate your venv manually)."))
		case installResult.Skipped:
			fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "No known dependency file detected, skipping install."))
		}
	}

	// Run post-create hooks (best-effort, non-fatal).
	if !setupOptions.skipHooks {
		if postCreateCmds := cfgResult.Config.PostCreateHooks(); len(postCreateCmds) > 0 {
			fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", fmt.Sprintf("Running %d post-create hook(s)...", len(postCreateCmds))))
			for _, r := range hooks.RunPostCreate(worktreePath, postCreateCmds, out) {
				if r.Err != nil {
					fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("hook %q failed: %v", r.Command, r.Err)))
				} else {
					fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Ran: "+r.Command))
				}
			}
		}
	}

	// Print review summary to stderr.
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Review worktree ready:"))
	fmt.Fprintf(out, "  PR/MR:  %s\n", render.PR(fmt.Sprintf("#%d", info.Number)))
	fmt.Fprintf(out, "  Title:  %s\n", render.Muted(render.Fit(info.Title, 10)))
	fmt.Fprintf(out, "  Branch: %s\n", render.Branch(render.Fit(info.Branch, 10)))
	fmt.Fprintf(out, "  Path:   %s\n", render.Path(render.Fit(worktreePath, 10)))
	setupOptions.printSkipped(out, render)

	// Print path to stdout for shell wrapper cd.
	fmt.Fprintln(cmd.OutOrStdout(), worktreePath)

	return nil
}

// pickPRNumber opens an fzf picker populated with open PRs/MRs and returns
// the selected PR/MR number.
func pickPRNumber(cmd *cobra.Command, forgeType forge.Type, repoSlug, host string) (int, error) {
	if !canInteract(cmd) {
		return 0, fmt.Errorf("interactive selection is unavailable; pass a PR or MR number")
	}
	if _, err := exec.LookPath("fzf"); err != nil {
		return 0, fmt.Errorf("fzf is required to pick an open PR/MR; pass a PR number or install fzf")
	}

	prs, err := forge.PRList(forgeType, repoSlug, host)
	if err != nil {
		return 0, fmt.Errorf("failed to list open PRs/MRs: %w", err)
	}
	if len(prs) == 0 {
		return 0, fmt.Errorf("no open PRs/MRs found")
	}

	// Build display lines.
	var sb strings.Builder
	render := commandRenderer(cmd)
	sb.WriteString(render.PRHeader())
	sb.WriteByte('\n')
	for i, pr := range prs {
		sb.WriteString(pickerRow(render.PRRow(pr.Number, pr.Branch, pr.Title), i))
		sb.WriteByte('\n')
	}

	// Pipe to fzf.
	fzfArgs := append(pickerArgs(sessionFor(cmd).errorOutput.Color, " open prs / mrs ", "review > "), "--header-lines=1")
	fzfCmd := exec.Command("fzf", fzfArgs...)
	fzfCmd.Stdin = strings.NewReader(sb.String())
	fzfCmd.Stderr = cmd.ErrOrStderr()

	out, err := fzfCmd.Output()
	if err != nil {
		if pickerCancelled(err) {
			return 0, errPickerCancelled
		}
		return 0, fmt.Errorf("fzf failed while selecting a PR/MR: %w", err)
	}

	selection := strings.TrimSpace(string(out))
	if selection == "" {
		return 0, errPickerCancelled
	}

	index := pickerSelectionIndex(selection, len(prs))
	if index < 0 {
		return 0, fmt.Errorf("could not map fzf selection to a PR/MR")
	}
	return prs[index].Number, nil
}

func cliInstallURL(f forge.Type) string {
	switch f {
	case forge.GitHub:
		return "https://cli.github.com/"
	case forge.GitLab:
		return "https://gitlab.com/gitlab-org/cli"
	default:
		return ""
	}
}
