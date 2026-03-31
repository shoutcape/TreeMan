package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
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
			return runReview(cmd, prArg)
		},
	}
	return cmd
}

func runReview(cmd *cobra.Command, prArg string) error {
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

	// For GitLab we also need jq (used by glab api).
	if forgeType == forge.GitLab {
		if _, err := exec.LookPath("jq"); err != nil {
			return fmt.Errorf("jq is required for review with GitLab repos. Install it from https://jqlang.github.io/jq/")
		}
	}

	// Resolve PR number — prompt via fzf if not provided.
	var prNumber int
	if prArg == "" {
		prNumber, err = pickPRNumber(forgeType, repoSlug, host)
		if err != nil {
			return err
		}
	} else {
		if err := validate.PRNumber(prArg); err != nil {
			return fmt.Errorf("usage: treeman review [pr-number]\n%w", err)
		}
		prNumber, _ = strconv.Atoi(prArg)
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
	fmt.Fprintf(os.Stderr, "Fetching PR/MR #%d from origin...\n", info.Number)
	if err := git.Fetch(fetchRef); err != nil {
		return err
	}

	// Create the worktree.
	fmt.Fprintf(os.Stderr, "Creating review worktree at %s (branch: %s)...\n", worktreePath, info.Branch)
	if err := git.WorktreeAdd(worktreePath, info.Branch, "FETCH_HEAD"); err != nil {
		return err
	}

	// Ensure .worktrees/ is gitignored (best-effort, non-fatal).
	if err := worktree.EnsureIgnored(mainRoot); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not update .gitignore: %v\n", err)
	}

	// Copy .env* files.
	envResult, envErr := envfile.Copy(mainRoot, worktreePath)
	if envErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not copy env files: %v\n", envErr)
	} else if len(envResult.Copied) > 0 {
		for _, f := range envResult.Copied {
			fmt.Fprintf(os.Stderr, "  Copied %s\n", f)
		}
		fmt.Fprintf(os.Stderr, "Copied %d env file(s) from main worktree.\n", len(envResult.Copied))
	}

	// Load project config for database management.
	cfgResult := config.Load(mainRoot)
	if cfgResult.Warning != "" {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", cfgResult.Warning)
	}

	// Set up branch-specific database (best-effort, non-fatal).
	dbEnvKey := cfgResult.Config.DatabaseEnvKey()
	dbResult, dbErr := database.SetupBranchDB(worktreePath, info.Branch, dbEnvKey)
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
		fmt.Fprintln(os.Stderr, "Detected Python project -- skipping auto-install (activate your venv manually).")
	case installResult.Skipped:
		fmt.Fprintln(os.Stderr, "No known dependency file detected, skipping install.")
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

	// Print review summary to stderr.
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Review worktree ready:")
	fmt.Fprintf(os.Stderr, "  PR/MR:  #%d\n", info.Number)
	fmt.Fprintf(os.Stderr, "  Title:  %s\n", info.Title)
	fmt.Fprintf(os.Stderr, "  Branch: %s\n", info.Branch)
	fmt.Fprintf(os.Stderr, "  Path:   %s\n", worktreePath)

	// Print path to stdout for shell wrapper cd.
	fmt.Fprintln(cmd.OutOrStdout(), worktreePath)

	return nil
}

// pickPRNumber opens an fzf picker populated with open PRs/MRs and returns
// the selected PR/MR number.
//
// Mirrors _wt_pick_pr_number in wt.sh:366.
func pickPRNumber(forgeType forge.Type, repoSlug, host string) (int, error) {
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
	sb.WriteString(ui.PRHeader())
	sb.WriteByte('\n')
	for _, pr := range prs {
		sb.WriteString(ui.PRRow(pr.Number, pr.Branch, pr.Title))
		sb.WriteByte('\n')
	}

	// Pipe to fzf.
	fzfCmd := exec.Command("fzf",
		"--ansi",
		"--border-label", " open prs / mrs ",
		"--header-lines=1",
		"--prompt=review > ",
		"--select-1",
		"--exit-0",
	)
	fzfCmd.Stdin = strings.NewReader(sb.String())
	fzfCmd.Stderr = os.Stderr

	out, err := fzfCmd.Output()
	if err != nil {
		// Exit code 130 = user cancelled (Ctrl-C / Esc).
		return 0, fmt.Errorf("no PR/MR selected")
	}

	selection := strings.TrimSpace(string(out))
	if selection == "" {
		return 0, fmt.Errorf("no PR/MR selected")
	}

	// Strip ANSI codes and extract the first field "#<number>".
	plain := ui.StripANSI(selection)
	fields := strings.Fields(plain)
	if len(fields) == 0 {
		return 0, fmt.Errorf("could not parse fzf selection")
	}

	numStr := strings.TrimPrefix(fields[0], "#")
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("could not parse PR/MR number from selection %q", fields[0])
	}
	return n, nil
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
