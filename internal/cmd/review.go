package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/shoutcape/treeman/internal/forge"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/shoutcape/treeman/internal/validate"
	"github.com/shoutcape/treeman/internal/worktree"
	"github.com/spf13/cobra"
)

func newReviewCmd() *cobra.Command {
	var setupOptions creationSetupOptions
	var execCommand string
	cmd := &cobra.Command{
		Use:   "review [pr-number]",
		Short: "Create a review worktree from a GitHub PR or GitLab MR",
		Long: `Fetch a PR or MR and create a linked worktree for it.

If pr-number is omitted, an interactive fzf picker lists all open PRs/MRs.

Supports GitHub (gh CLI) and GitLab (glab CLI), including self-hosted GitLab
instances.

Shell integration changes directory to the new worktree; without it, the
path is printed to stdout. With --exec, TreeMan runs the given command in
the new worktree instead.`,
		Aliases: []string{"wtpr", "wtmr"},
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var prArg string
			if len(args) > 0 {
				prArg = args[0]
			}
			return runReview(cmd, prArg, setupOptions, execCommand)
		},
	}
	addCreationSetupFlags(cmd, &setupOptions)
	addLaunchFlag(cmd, &execCommand)
	return cmd
}

func runReview(cmd *cobra.Command, prArg string, setupOptions creationSetupOptions, execCommand string) error {
	created, err := createReviewWorktree(cmd, prArg)
	if err != nil || created.worktree.Path == "" {
		return err
	}

	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	summary := setupCreatedWorktree(out, render, created.mainRoot, created.worktree, setupOptions)
	fmt.Fprintln(out)
	printSetupSummary(out, render, summary)
	fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Review worktree ready:"))
	fmt.Fprintf(out, "  PR/MR:  %s\n", render.PR(fmt.Sprintf("#%d", created.info.Number)))
	fmt.Fprintf(out, "  Title:  %s\n", render.Muted(render.Fit(created.info.Title, 10)))
	fmt.Fprintf(out, "  Branch: %s\n", render.Branch(render.Fit(created.worktree.Branch, 10)))
	fmt.Fprintf(out, "  Path:   %s\n", render.Path(render.Fit(created.worktree.Path, 10)))
	return deliverWorktree(cmd, created.worktree.Path, execCommand)
}

type reviewWorktreeCreation struct {
	mainRoot string
	worktree git.CreatedWorktree
	info     forge.PRInfo
}

func createReviewWorktree(cmd *cobra.Command, prArg string) (reviewWorktreeCreation, error) {
	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	if !git.IsInsideRepo() {
		return reviewWorktreeCreation{}, fmt.Errorf("not inside a git repository")
	}

	forgeInfo, err := resolveReviewForge()
	if err != nil {
		return reviewWorktreeCreation{}, err
	}

	// Resolve PR number — prompt via fzf if not provided.
	var prNumber int
	if prArg == "" {
		prNumber, err = pickPRNumber(cmd, forgeInfo)
		if err != nil {
			if errors.Is(err, errPickerCancelled) {
				fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "Cancelled."))
				return reviewWorktreeCreation{}, nil
			}
			return reviewWorktreeCreation{}, err
		}
	} else {
		prNumber, err = validate.PRNumber(prArg)
		if err != nil {
			return reviewWorktreeCreation{}, fmt.Errorf("usage: treeman review [pr-number]\n%w", err)
		}
	}

	// Fetch PR/MR metadata.
	info, err := forge.PRMetadata(commandContext(cmd), forgeInfo.forgeType, forgeInfo.repoSlug, forgeInfo.host, prNumber)
	if err != nil {
		return reviewWorktreeCreation{}, fmt.Errorf("failed to resolve PR/MR #%d with %s: %w", prNumber, forgeInfo.cliTool, err)
	}

	if info.Branch == "" {
		return reviewWorktreeCreation{}, fmt.Errorf("incomplete PR/MR metadata returned by %s", forgeInfo.cliTool)
	}

	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return reviewWorktreeCreation{}, err
	}

	existing, err := git.WorktreeList()
	if err != nil {
		return reviewWorktreeCreation{}, err
	}
	worktreePath, err := worktree.ResolvePathForBranch(mainRoot, info.Branch, existing)
	if err != nil {
		return reviewWorktreeCreation{}, err
	}

	// Guard: branch must not already exist locally.
	if git.BranchExists(info.Branch) {
		existing, _ := git.FindWorktreeForBranch(info.Branch)
		if existing != "" {
			return reviewWorktreeCreation{}, fmt.Errorf("branch %q already has a worktree at %q", info.Branch, existing)
		}
		return reviewWorktreeCreation{}, fmt.Errorf("PR/MR head branch %q already exists locally", info.Branch)
	}

	// Guard: directory must not exist.
	if _, err := os.Stat(worktreePath); err == nil {
		return reviewWorktreeCreation{}, fmt.Errorf("directory %q already exists for branch %q", worktreePath, info.Branch)
	}

	// Fetch the PR/MR ref.
	fetchRef := forge.FetchRef(forgeInfo.forgeType, info.Number)
	fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", fmt.Sprintf("Fetching PR/MR #%d from origin...", info.Number)))
	fetchedSHA, err := git.FetchCommit(fetchRef)
	if err != nil {
		return reviewWorktreeCreation{}, err
	}

	// Create the worktree.
	fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", fmt.Sprintf("Creating review worktree at %s (branch: %s)...", worktreePath, info.Branch)))
	created, err := git.CreateWorktree(worktreePath, info.Branch, fetchedSHA)
	if err != nil {
		return reviewWorktreeCreation{}, err
	}

	// Fetch the branch by name so origin/<branch> remote-tracking ref exists,
	// then set upstream so git pull/push work without explicit remote args.
	// Non-fatal: fork PRs may not have the branch on origin.
	if err := git.Fetch(info.Branch); err != nil {
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("could not fetch branch %q (upstream not set): %v", info.Branch, err)))
	} else if err := git.SetUpstreamInDir(worktreePath, info.Branch); err != nil {
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("could not set upstream for %q: %v", info.Branch, err)))
	}

	return reviewWorktreeCreation{mainRoot: mainRoot, worktree: created, info: info}, nil
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

var reviewPickerPRBatches = forge.StreamPRBatches

// streamReviewRows emits one picker row per open PR/MR as each forge batch
// arrives, appending every PR to prs so a selection can be resolved once the
// picker closes.
func streamReviewRows(render ui.Renderer, forgeInfo reviewForge, prs *[]forge.PRInfo) pickerProducer {
	return func(ctx context.Context, emit func(string) error) error {
		err := reviewPickerPRBatches(ctx, forgeInfo.forgeType, forgeInfo.repoSlug, forgeInfo.host, func(batch []forge.PRInfo) error {
			for _, pr := range batch {
				*prs = append(*prs, pr)
				if err := emit(render.PRRow(pr.Number, pr.Branch, pr.Title)); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil && !consumerClosed(err) {
			return fmt.Errorf("failed to list open PRs/MRs: %w", err)
		}
		return err
	}
}

// reviewPickerResults streams the whole wtmr picker payload without starting
// fzf, returning the number of open reviews and how long the first row took.
func reviewPickerResults(cmd *cobra.Command) (int, time.Duration, error) {
	if !git.IsInsideRepo() {
		return 0, 0, fmt.Errorf("not inside a git repository")
	}
	writer := newPickerResultsWriter()
	forgeInfo, err := resolveReviewForge()
	if err != nil {
		return 0, 0, err
	}
	var prs []forge.PRInfo
	render := commandRenderer(cmd)
	count, err := streamPickerRows(commandContext(cmd), writer, render.PRHeader(), streamReviewRows(render, forgeInfo, &prs))
	if err != nil {
		return 0, 0, err
	}
	if count == 0 {
		return 0, 0, fmt.Errorf("no open PRs/MRs found")
	}
	return count, writer.firstRow(), nil
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

	// fzf starts before the first forge batch lands, so the picker is on screen
	// and filtering while the remaining results are still being fetched.
	render := commandRenderer(cmd)
	var prs []forge.PRInfo
	index, count, err := runStreamingPicker(cmd, pickerRequest{
		label:  " open prs / mrs ",
		prompt: "review > ",
		header: render.PRHeader(),
	}, streamReviewRows(render, forgeInfo, &prs))
	if err != nil {
		if errors.Is(err, errPickerCancelled) && count == 0 {
			return 0, fmt.Errorf("no open PRs/MRs found")
		}
		return 0, err
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
