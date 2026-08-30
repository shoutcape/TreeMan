package cmd

import (
	"errors"
	"fmt"
	"io"
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
can cd into it.`,
		Aliases: []string{"wtb"},
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var query string
			if len(args) > 0 {
				query = args[0]
			}
			return runBranchWithSetup(cmd, query, setupOptions)
		},
	}
	addCreationSetupFlags(cmd, &setupOptions)

	return cmd
}

func runBranch(cmd *cobra.Command, query string) error {
	return runBranchWithSetup(cmd, query, creationSetupOptions{})
}

func runBranchWithSetup(cmd *cobra.Command, query string, setupOptions creationSetupOptions) error {
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
	fmt.Fprintln(cmd.OutOrStdout(), created.worktree.Path)
	return nil
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
		data, err := loadBranchPickerData(cmd)
		if err != nil {
			return branchWorktreeCreation{}, err
		}
		prMap = data.prMap
		selected, err = pickBranch(cmd, data.branches, query, prMap)
		if err != nil {
			if errors.Is(err, errPickerCancelled) {
				fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "Cancelled."))
				return branchWorktreeCreation{}, nil
			}
			return branchWorktreeCreation{}, err
		}
	}

	branch := selected.Name
	worktreePath := worktree.PathForBranch(mainRoot, branch)

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

// loadBranchPickerData fetches and prepares every branch that wtb can present.
func loadBranchPickerData(cmd *cobra.Command) (branchPickerData, error) {
	remoteURL, err := git.OriginRemoteURL()
	if err != nil {
		return branchPickerData{}, err
	}

	forgeType, repoSlug, host, err := forge.ResolveFromRemote(remoteURL)
	if err != nil {
		return branchPickerData{}, err
	}

	cliTool := forge.CLITool(forgeType)
	if _, err := exec.LookPath(cliTool); err != nil {
		return branchPickerData{}, fmt.Errorf("%s is required for branch listing with %s repos. Install it from %s",
			cliTool, forgeType, cliInstallURL(forgeType))
	}
	return loadBranchPickerDataForForge(cmd, forgeType, repoSlug, host)
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
	go func() {
		branches, err := branchPickerBranchList(forgeType, repoSlug, host)
		branchResults <- branchListResult{branches: branches, err: err}
	}()
	go func() {
		prs, err := branchPickerPRList(forgeType, repoSlug, host)
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
	if _, err := git.MainWorktreeRoot(); err != nil {
		return 0, 0, err
	}
	// Timed from before the fetch: the branch picker cannot emit a row until
	// the concurrent PR/MR lookup that fills its MR column has finished.
	writer := newFirstRowWriter(io.Discard, time.Now)
	data, err := loadBranchPickerData(cmd)
	if err != nil {
		return 0, 0, err
	}
	render := commandRenderer(cmd)
	if _, err := streamPickerRows(writer, render.BranchHeader(), streamBranchRows(render, data.branches, data.prMap)); err != nil {
		return 0, 0, err
	}
	return len(data.branches), writer.firstRow(), nil
}

// pickBranch opens an fzf picker populated with remote branches and returns
// the selected branch. If query is provided, it pre-filters the list.
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

// streamBranchRows emits one picker row per remote branch.
//
// Unlike the review picker these rows cannot be streamed straight from the API:
// the MR/PR column comes from a second, concurrent query, and a row written
// before that query lands would claim a branch has no open review.
func streamBranchRows(render ui.Renderer, branches []forge.BranchInfo, prMap map[string]forge.PRInfo) func(emit func(string) error) error {
	return func(emit func(string) error) error {
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
