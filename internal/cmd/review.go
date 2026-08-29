package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/forge"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/shoutcape/treeman/internal/validate"
	"github.com/shoutcape/treeman/internal/worktree"
	"github.com/spf13/cobra"
)

var reviewWorktreeAdd = func(path, branch, startPoint string) (bool, error) {
	err := git.WorktreeAdd(path, branch, startPoint)
	return err == nil, err
}

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
	created, err := runReviewWithResult(cmd, prArg, setupOptions)
	if err != nil {
		return err
	}
	if created.path != "" {
		// Print path to stdout for shell wrapper cd.
		fmt.Fprintln(cmd.OutOrStdout(), created.path)
	}
	return nil
}

type createdWorktree struct {
	mainRoot string
	path     string
	branch   string
}

func runReviewWithResult(cmd *cobra.Command, prArg string, setupOptions creationSetupOptions) (createdWorktree, error) {
	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	if !git.IsInsideRepo() {
		return createdWorktree{}, fmt.Errorf("not inside a git repository")
	}

	forgeInfo, err := resolveReviewForge()
	if err != nil {
		return createdWorktree{}, err
	}

	// Resolve PR number — prompt via fzf if not provided.
	var prNumber int
	if prArg == "" {
		prNumber, err = pickPRNumber(cmd, forgeInfo)
		if err != nil {
			if errors.Is(err, errPickerCancelled) {
				fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "Cancelled."))
				return createdWorktree{}, nil
			}
			return createdWorktree{}, err
		}
	} else {
		prNumber, err = validate.PRNumber(prArg)
		if err != nil {
			return createdWorktree{}, fmt.Errorf("usage: treeman review [pr-number]\n%w", err)
		}
	}

	// Fetch PR/MR metadata.
	info, err := forge.PRMetadata(forgeInfo.forgeType, forgeInfo.repoSlug, forgeInfo.host, prNumber)
	if err != nil {
		return createdWorktree{}, fmt.Errorf("failed to resolve PR/MR #%d with %s: %w", prNumber, forgeInfo.cliTool, err)
	}

	if info.Branch == "" {
		return createdWorktree{}, fmt.Errorf("incomplete PR/MR metadata returned by %s", forgeInfo.cliTool)
	}

	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return createdWorktree{}, err
	}

	worktreePath := worktree.PathForBranch(mainRoot, info.Branch)

	// Guard: branch must not already exist locally.
	if git.BranchExists(info.Branch) {
		existing, _ := git.FindWorktreeForBranch(info.Branch)
		if existing != "" {
			return createdWorktree{}, fmt.Errorf("branch %q already has a worktree at %q", info.Branch, existing)
		}
		return createdWorktree{}, fmt.Errorf("PR/MR head branch %q already exists locally", info.Branch)
	}

	// Guard: directory must not exist.
	if _, err := os.Stat(worktreePath); err == nil {
		return createdWorktree{}, fmt.Errorf("directory %q already exists for branch %q", worktreePath, info.Branch)
	}

	// Fetch the PR/MR ref.
	fetchRef := forge.FetchRef(forgeInfo.forgeType, info.Number)
	fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", fmt.Sprintf("Fetching PR/MR #%d from origin...", info.Number)))
	if err := git.Fetch(fetchRef); err != nil {
		return createdWorktree{}, err
	}

	// Create the worktree.
	fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", fmt.Sprintf("Creating review worktree at %s (branch: %s)...", worktreePath, info.Branch)))
	worktreeCreated, err := reviewWorktreeAdd(worktreePath, info.Branch, "FETCH_HEAD")
	created := createdWorktree{}
	if worktreeCreated {
		created = createdWorktree{mainRoot: mainRoot, path: worktreePath, branch: info.Branch}
	}
	if err != nil {
		return created, err
	}

	// Fetch the branch by name so origin/<branch> remote-tracking ref exists,
	// then set upstream so git pull/push work without explicit remote args.
	// Non-fatal: fork PRs may not have the branch on origin.
	if err := git.Fetch(info.Branch); err != nil {
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("could not fetch branch %q (upstream not set): %v", info.Branch, err)))
	} else if err := git.SetUpstreamInDir(worktreePath, info.Branch); err != nil {
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("could not set upstream for %q: %v", info.Branch, err)))
	}

	if !setupOptions.skipSetup {
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

		summary := runWorktreeSetup(out, render, worktreeSetup{
			mainRoot:      mainRoot,
			worktreePath:  worktreePath,
			branch:        info.Branch,
			projectConfig: cfgResult.Config,
			options:       setupOptions,
		})

		// Print review summary to stderr.
		fmt.Fprintln(out, "")
		printSetupSummary(out, render, summary)
	}
	fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Review worktree ready:"))
	fmt.Fprintf(out, "  PR/MR:  %s\n", render.PR(fmt.Sprintf("#%d", info.Number)))
	fmt.Fprintf(out, "  Title:  %s\n", render.Muted(render.Fit(info.Title, 10)))
	fmt.Fprintf(out, "  Branch: %s\n", render.Branch(render.Fit(info.Branch, 10)))
	fmt.Fprintf(out, "  Path:   %s\n", render.Path(render.Fit(worktreePath, 10)))

	return created, nil
}

type reviewForge struct {
	forgeType forge.Type
	repoSlug  string
	host      string
	cliTool   string
}

func resolveReviewForge() (reviewForge, error) {
	remoteURL, err := git.OriginRemoteURL()
	if err != nil {
		return reviewForge{}, err
	}

	forgeType, repoSlug, host, err := forge.ResolveFromRemote(remoteURL)
	if err != nil {
		return reviewForge{}, err
	}

	cliTool := forge.CLITool(forgeType)
	if _, err := exec.LookPath(cliTool); err != nil {
		return reviewForge{}, fmt.Errorf("%s is required for review with %s repos. Install it from %s",
			cliTool, forgeType, cliInstallURL(forgeType))
	}
	return reviewForge{forgeType: forgeType, repoSlug: repoSlug, host: host, cliTool: cliTool}, nil
}

func loadReviewPickerData(forgeInfo reviewForge) ([]forge.PRInfo, error) {
	prs, err := forge.PRList(forgeInfo.forgeType, forgeInfo.repoSlug, forgeInfo.host)
	if err != nil {
		return nil, fmt.Errorf("failed to list open PRs/MRs: %w", err)
	}
	if len(prs) == 0 {
		return nil, fmt.Errorf("no open PRs/MRs found")
	}
	return prs, nil
}

// reviewPickerResults loads and renders the complete wtmr picker payload without
// starting fzf, returning the number of open reviews.
func reviewPickerResults(cmd *cobra.Command) (int, error) {
	if !git.IsInsideRepo() {
		return 0, fmt.Errorf("not inside a git repository")
	}
	forgeInfo, err := resolveReviewForge()
	if err != nil {
		return 0, err
	}
	prs, err := loadReviewPickerData(forgeInfo)
	if err != nil {
		return 0, err
	}
	_ = reviewPickerPayload(cmd, prs)
	return len(prs), nil
}

// pickPRNumber opens an fzf picker populated with open PRs/MRs and returns
// the selected PR/MR number.
func pickPRNumber(cmd *cobra.Command, forgeInfo reviewForge) (int, error) {
	if !canInteract(cmd) {
		return 0, fmt.Errorf("interactive selection is unavailable; pass a PR or MR number")
	}
	if _, err := exec.LookPath("fzf"); err != nil {
		return 0, fmt.Errorf("fzf is required to pick an open PR/MR; pass a PR number or install fzf")
	}

	prs, err := loadReviewPickerData(forgeInfo)
	if err != nil {
		return 0, err
	}

	payload := reviewPickerPayload(cmd, prs)

	// Pipe to fzf.
	fzfArgs := append(pickerArgs(sessionFor(cmd).errorOutput.Color, " open prs / mrs ", "review > "), "--header-lines=1")
	fzfCmd := exec.Command("fzf", fzfArgs...)
	fzfCmd.Stdin = strings.NewReader(payload)
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

func reviewPickerPayload(cmd *cobra.Command, prs []forge.PRInfo) string {
	var sb strings.Builder
	render := commandRenderer(cmd)
	sb.WriteString(render.PRHeader())
	sb.WriteByte('\n')
	for i, pr := range prs {
		sb.WriteString(pickerRow(render.PRRow(pr.Number, pr.Branch, pr.Title), i))
		sb.WriteByte('\n')
	}
	return sb.String()
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
