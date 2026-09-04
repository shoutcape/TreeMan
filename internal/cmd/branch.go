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
	"github.com/shoutcape/treeman/internal/worktree"
	"github.com/spf13/cobra"
)

func newBranchCmd() *cobra.Command {
	var setupOptions creationSetupOptions
	var launchOptions worktreeLaunchOptions
	cmd := &cobra.Command{
		Use:   "branch [query]",
		Short: "Create a worktree from a remote branch",
		Long: `Fetch a remote branch and create a linked worktree for it.

If no argument is given, an interactive fzf picker lists all remote branches
(excluding the default branch and branches that already exist locally).

If a query is provided, it pre-filters the fzf list. An exact match selects
automatically without showing the picker.

Requires the forge CLI (gh for GitHub, glab for GitLab) to list branches
and open MRs/PRs.

The path of the new worktree is printed to stdout so that a shell wrapper
can cd into it. With --exec, TreeMan runs the given command in the new
worktree instead of printing the path.`,
		Aliases: []string{"wtb"},
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := launchOptions.validate(cmd); err != nil {
				return err
			}
			var query string
			if len(args) > 0 {
				query = args[0]
			}
			return runBranchWithSetup(cmd, query, setupOptions, launchOptions)
		},
	}
	addCreationSetupFlags(cmd, &setupOptions)
	addLaunchFlag(cmd, &launchOptions)

	return cmd
}

func runBranch(cmd *cobra.Command, query string) error {
	return runBranchWithSetup(cmd, query, creationSetupOptions{}, worktreeLaunchOptions{})
}

func runBranchWithSetup(cmd *cobra.Command, query string, setupOptions creationSetupOptions, launchOptions worktreeLaunchOptions) error {
	created, err := createBranchWorktree(cmd, query)
	if err != nil || created.worktree.Path == "" {
		return err
	}

	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	summary := setupCreatedWorktree(out, render, created.mainRoot, created.worktree, setupOptions)
	fmt.Fprintln(out)
	printSetupSummary(out, render, summary)
	fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Worktree ready:"))
	fmt.Fprintf(out, "  Branch: %s\n", render.Branch(render.Fit(created.worktree.Branch, 10)))
	if pr, ok := created.prMap[created.worktree.Branch]; ok {
		fmt.Fprintf(out, "  MR/PR:  #%d - %s\n", pr.Number, pr.Title)
	}
	fmt.Fprintf(out, "  Path:   %s\n", render.Path(render.Fit(created.worktree.Path, 10)))
	return deliverWorktree(cmd, created.worktree.Path, launchOptions)
}

type branchWorktreeCreation struct {
	mainRoot string
	worktree git.CreatedWorktree
	prMap    map[string]forge.PRInfo
}

func createBranchWorktree(cmd *cobra.Command, query string) (branchWorktreeCreation, error) {
	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	if !git.IsInsideRepo() {
		return branchWorktreeCreation{}, fmt.Errorf("not inside a git repository")
	}

	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return branchWorktreeCreation{}, err
	}

	var selected forge.BranchInfo
	prMap := make(map[string]forge.PRInfo)
	if query != "" && git.RemoteBranchExists(query) {
		// An exact branch name can be fetched by git without forge discovery or fzf.
		selected.Name = query
	} else {
		selection, err := pickBranchForForge(cmd, query)
		if err != nil {
			if errors.Is(err, errPickerCancelled) {
				fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "Cancelled."))
				return branchWorktreeCreation{}, nil
			}
			return branchWorktreeCreation{}, err
		}
		selected, prMap = selection.branch, selection.prMap
	}

	branch := selected.Name
	existing, err := git.WorktreeList()
	if err != nil {
		return branchWorktreeCreation{}, err
	}
	worktreePath, err := worktree.ResolvePathForBranch(mainRoot, branch, existing)
	if err != nil {
		return branchWorktreeCreation{}, err
	}

	// Guard: directory must not exist.
	if _, err := os.Stat(worktreePath); err == nil {
		return branchWorktreeCreation{}, fmt.Errorf("directory %q already exists for branch %q", worktreePath, branch)
	}

	// Fetch the branch from origin.
	fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", fmt.Sprintf("Fetching branch %s from origin...", branch)))
	if err := git.FetchRemoteBranch(branch); err != nil {
		return branchWorktreeCreation{}, fmt.Errorf("failed to fetch branch %q: %w", branch, err)
	}

	// Create the worktree tracking the remote branch.
	fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", fmt.Sprintf("Creating worktree at %s (branch: %s)...", worktreePath, branch)))
	created, err := git.CreateWorktreeFromRemote(worktreePath, branch)
	if err != nil {
		return branchWorktreeCreation{}, err
	}

	// Set upstream so git pull/push work.
	if err := git.SetUpstreamInDir(worktreePath, branch); err != nil {
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("could not set upstream for %q: %v", branch, err)))
	}

	return branchWorktreeCreation{mainRoot: mainRoot, worktree: created, prMap: prMap}, nil
}

type branchPickerData struct {
	branches []forge.BranchInfo
	prMap    map[string]forge.PRInfo
}

var (
	branchPickerBranchList    = forge.BranchList
	branchPickerPRList        = forge.PRList
	branchPickerBranchSHAs    = git.BranchSHAs
	branchPickerDefaultBranch = git.DetectDefaultBranch
)

// branchForge is the repository the branch picker will query.
type branchForge struct {
	forgeType forge.Type
	repoSlug  string
	host      string
}

func resolveBranchForge() (branchForge, error) {
	remoteURL, err := git.OriginRemoteURL()
	if err != nil {
		return branchForge{}, err
	}

	forgeType, repoSlug, host, err := forge.ResolveFromRemote(remoteURL)
	if err != nil {
		return branchForge{}, err
	}

	cliTool := forge.CLITool(forgeType)
	if _, err := exec.LookPath(cliTool); err != nil {
		return branchForge{}, fmt.Errorf("%s is required for branch listing with %s repos. Install it from %s",
			cliTool, forgeType, cliInstallURL(forgeType))
	}
	return branchForge{forgeType: forgeType, repoSlug: repoSlug, host: host}, nil
}

// branchSelection is the branch the user picked, with the PR metadata that was
// gathered for the picker so the summary can name the branch's open review.
type branchSelection struct {
	branch forge.BranchInfo
	prMap  map[string]forge.PRInfo
}

// pickBranchForForge runs the picker that suits the forge. GitHub streams rows
// as they are enriched; GitLab combines its NDJSON branch stream with the MR
// list before rendering because the two arrive independently.
func pickBranchForForge(cmd *cobra.Command, query string) (branchSelection, error) {
	forgeInfo, err := resolveBranchForge()
	if err != nil {
		return branchSelection{}, err
	}
	if forgeInfo.forgeType == forge.GitHub {
		return pickGitHubBranch(cmd, forgeInfo.repoSlug, query)
	}

	data, err := loadBranchPickerDataForForge(cmd, forgeInfo.forgeType, forgeInfo.repoSlug, forgeInfo.host)
	if err != nil {
		return branchSelection{}, err
	}
	branch, err := pickBranch(cmd, data.branches, query, data.prMap)
	if err != nil {
		return branchSelection{}, err
	}
	return branchSelection{branch: branch, prMap: data.prMap}, nil
}

func loadBranchPickerDataForForge(cmd *cobra.Command, forgeType forge.Type, repoSlug, host string) (branchPickerData, error) {
	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", "Fetching remote branches..."))
	fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", "Checking open MRs/PRs..."))
	type branchListResult struct {
		branches []forge.BranchInfo
		err      error
	}
	type prListResult struct {
		prs []forge.PRInfo
		err error
	}
	branchResults := make(chan branchListResult, 1)
	prResults := make(chan prListResult, 1)
	ctx := commandContext(cmd)
	go func() {
		branches, err := branchPickerBranchList(ctx, forgeType, repoSlug, host)
		branchResults <- branchListResult{branches: branches, err: err}
	}()
	go func() {
		prs, err := branchPickerPRList(ctx, forgeType, repoSlug, host)
		prResults <- prListResult{prs: prs, err: err}
	}()

	branchResult := <-branchResults
	if branchResult.err != nil {
		<-prResults
		return branchPickerData{}, fmt.Errorf("failed to list remote branches: %w", branchResult.err)
	}

	defaultBranch, _ := branchPickerDefaultBranch()
	branchNames := make([]string, 0, len(branchResult.branches))
	for _, branch := range branchResult.branches {
		branchNames = append(branchNames, branch.Name)
	}
	localBranches, err := branchPickerBranchSHAs(branchNames)
	if err != nil {
		<-prResults
		return branchPickerData{}, err
	}

	data := branchPickerData{prMap: make(map[string]forge.PRInfo)}
	for _, branch := range branchResult.branches {
		if branch.Name != defaultBranch {
			if _, exists := localBranches[branch.Name]; !exists {
				data.branches = append(data.branches, branch)
			}
		}
	}
	prResult := <-prResults
	if len(data.branches) == 0 {
		return branchPickerData{}, fmt.Errorf("no remote branches available (all already exist locally or only default branch found)")
	}

	if prResult.err != nil {
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("could not fetch MRs/PRs: %v", prResult.err)))
		return data, nil
	}
	for _, pr := range prResult.prs {
		data.prMap[pr.Branch] = pr
	}
	return data, nil
}

// branchPickerResults streams the whole wtb picker payload without starting
// fzf, returning the number of available branches and how long the first row
// took.
func branchPickerResults(cmd *cobra.Command) (int, time.Duration, error) {
	if !git.IsInsideRepo() {
		return 0, 0, fmt.Errorf("not inside a git repository")
	}
	writer := newPickerResultsWriter()
	if _, err := git.MainWorktreeRoot(); err != nil {
		return 0, 0, err
	}
	forgeInfo, err := resolveBranchForge()
	if err != nil {
		return 0, 0, err
	}
	render := commandRenderer(cmd)

	if forgeInfo.forgeType == forge.GitHub {
		stream, err := newBranchStream(render, githubBranchSources())
		if err != nil {
			return 0, 0, err
		}
		count, err := streamPickerRows(commandContext(cmd), writer, render.BranchHeader(), stream.produce(forgeInfo.repoSlug))
		if err != nil {
			return 0, 0, err
		}
		return count, writer.firstRow(), nil
	}

	data, err := loadBranchPickerDataForForge(cmd, forgeInfo.forgeType, forgeInfo.repoSlug, forgeInfo.host)
	if err != nil {
		return 0, 0, err
	}
	if _, err := streamPickerRows(commandContext(cmd), writer, render.BranchHeader(), streamBranchRows(render, data.branches, data.prMap)); err != nil {
		return 0, 0, err
	}
	return len(data.branches), writer.firstRow(), nil
}

// pickBranch opens an fzf picker populated with an already-fetched list of
// remote branches and returns the selected branch. GitHub uses
// pickGitHubBranch, which streams instead. If query is provided, it pre-filters the list.
// If query is an exact match, it auto-selects without showing the picker.
// prMap maps branch names to their associated PR/MR info (may be nil).
func pickBranch(cmd *cobra.Command, branches []forge.BranchInfo, query string, prMap map[string]forge.PRInfo) (forge.BranchInfo, error) {
	// If query is an exact match, skip fzf.
	if query != "" {
		for _, b := range branches {
			if b.Name == query {
				return b, nil
			}
		}
	}
	if !canInteract(cmd) {
		return forge.BranchInfo{}, fmt.Errorf("interactive selection is unavailable; pass an exact branch name")
	}

	if _, err := exec.LookPath("fzf"); err != nil {
		if query != "" {
			return forge.BranchInfo{}, fmt.Errorf("no exact match for %q and fzf is not installed for interactive selection", query)
		}
		return forge.BranchInfo{}, fmt.Errorf("fzf is required to pick a remote branch; pass an exact branch name or install fzf")
	}

	render := commandRenderer(cmd)
	index, _, err := runStreamingPicker(cmd, pickerRequest{
		label:  " remote branches ",
		prompt: "branch > ",
		query:  query,
		header: render.BranchHeader(),
	}, streamBranchRows(render, branches, prMap))
	if err != nil {
		return forge.BranchInfo{}, err
	}
	return branches[index], nil
}

// streamBranchRows emits one picker row per remote branch from a list that has
// already been fetched.
//
// This is the GitLab path. Its MR column comes from a separate query for every
// open MR, so no row can be written until that query lands. GitHub instead
// asks each branch for its own PR and streams rows as they are enriched; see
// branchStream.
func streamBranchRows(render ui.Renderer, branches []forge.BranchInfo, prMap map[string]forge.PRInfo) pickerProducer {
	return func(_ context.Context, emit func(string) error) error {
		for _, branch := range branches {
			mrNumber := 0
			if pr, ok := prMap[branch.Name]; ok {
				mrNumber = pr.Number
			}
			if err := emit(render.BranchRow(branch.Name, branch.Date, mrNumber)); err != nil {
				return err
			}
		}
		return nil
	}
}
