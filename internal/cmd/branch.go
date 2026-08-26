package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/envfile"
	"github.com/shoutcape/treeman/internal/forge"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/hooks"
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
	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	if !git.IsInsideRepo() {
		return fmt.Errorf("not inside a git repository")
	}

	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return err
	}

	var selected forge.BranchInfo
	prMap := make(map[string]forge.PRInfo)
	if query != "" && git.RemoteBranchExists(query) {
		// An exact branch name can be fetched by git without forge discovery or fzf.
		selected.Name = query
	} else {
		// Detect forge from origin remote.
		remoteURL, err := git.OriginRemoteURL()
		if err != nil {
			return err
		}

		forgeType, repoSlug, host, err := forge.ResolveFromRemote(remoteURL)
		if err != nil {
			return err
		}

		cliTool := forge.CLITool(forgeType)
		if _, err := exec.LookPath(cliTool); err != nil {
			return fmt.Errorf("%s is required for branch listing with %s repos. Install it from %s",
				cliTool, forgeType, cliInstallURL(forgeType))
		}

		fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", "Fetching remote branches..."))
		allBranches, err := forge.BranchList(forgeType, repoSlug, host)
		if err != nil {
			return fmt.Errorf("failed to list remote branches: %w", err)
		}

		defaultBranch, _ := git.DetectDefaultBranch()
		var branches []forge.BranchInfo
		for _, b := range allBranches {
			if b.Name != defaultBranch && !git.BranchExists(b.Name) {
				branches = append(branches, b)
			}
		}
		if len(branches) == 0 {
			return fmt.Errorf("no remote branches available (all already exist locally or only default branch found)")
		}

		fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", "Checking open MRs/PRs..."))
		prs, err := forge.PRList(forgeType, repoSlug, host)
		if err != nil {
			fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("could not fetch MRs/PRs: %v", err)))
		} else {
			for _, pr := range prs {
				prMap[pr.Branch] = pr
			}
		}

		selected, err = pickBranch(cmd, branches, query, prMap)
		if err != nil {
			if errors.Is(err, errPickerCancelled) {
				fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "Cancelled."))
				return nil
			}
			return err
		}
	}

	branch := selected.Name
	worktreePath := worktree.PathForBranch(mainRoot, branch)

	// Guard: directory must not exist.
	if _, err := os.Stat(worktreePath); err == nil {
		return fmt.Errorf("directory %q already exists for branch %q", worktreePath, branch)
	}

	// Fetch the branch from origin.
	fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", fmt.Sprintf("Fetching branch %s from origin...", branch)))
	if err := git.Fetch(branch); err != nil {
		return fmt.Errorf("failed to fetch branch %q: %w", branch, err)
	}

	// Create the worktree tracking the remote branch.
	fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", fmt.Sprintf("Creating worktree at %s (branch: %s)...", worktreePath, branch)))
	if err := git.WorktreeAddExisting(worktreePath, branch); err != nil {
		return err
	}

	// Set upstream so git pull/push work.
	if err := git.SetUpstreamInDir(worktreePath, branch); err != nil {
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("could not set upstream for %q: %v", branch, err)))
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
		setupCreatedDatabase(out, render, cfgResult.Config, worktreePath, branch)
	}

	// Install dependencies.
	dependencySetup := setupDependencies(out, render, worktreePath, setupOptions.skipDeps)

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

	// Print summary to stderr.
	fmt.Fprintln(out, "")
	printWorktreeReadiness(out, render, dependencySetup.incomplete, "Worktree")
	fmt.Fprintf(out, "  Branch: %s\n", render.Branch(render.Fit(branch, 10)))
	if pr, ok := prMap[branch]; ok {
		fmt.Fprintf(out, "  MR/PR:  #%d - %s\n", pr.Number, pr.Title)
	}
	fmt.Fprintf(out, "  Path:   %s\n", render.Path(render.Fit(worktreePath, 10)))
	setupOptions.printSkipped(out, render)

	// Print path to stdout so the shell wrapper can cd into it.
	fmt.Fprintln(cmd.OutOrStdout(), worktreePath)

	return nil
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

	// Build display lines.
	var sb strings.Builder
	render := commandRenderer(cmd)
	sb.WriteString(render.BranchHeader())
	sb.WriteByte('\n')
	for i, b := range branches {
		mrNumber := 0
		if pr, ok := prMap[b.Name]; ok {
			mrNumber = pr.Number
		}
		sb.WriteString(pickerRow(render.BranchRow(b.Name, b.Date, mrNumber), i))
		sb.WriteByte('\n')
	}

	// Pipe to fzf.
	fzfArgs := append(pickerArgs(sessionFor(cmd).errorOutput.Color, " remote branches ", "branch > "), "--header-lines=1")
	if query != "" {
		fzfArgs = append(fzfArgs, "--query", query)
	}

	fzfCmd := exec.Command("fzf", fzfArgs...)
	fzfCmd.Stdin = strings.NewReader(sb.String())
	fzfCmd.Stderr = cmd.ErrOrStderr()

	out, err := fzfCmd.Output()
	if err != nil {
		if pickerCancelled(err) {
			return forge.BranchInfo{}, errPickerCancelled
		}
		return forge.BranchInfo{}, fmt.Errorf("fzf failed while selecting a branch: %w", err)
	}

	selection := strings.TrimSpace(string(out))
	if selection == "" {
		return forge.BranchInfo{}, errPickerCancelled
	}

	index := pickerSelectionIndex(selection, len(branches))
	if index < 0 {
		return forge.BranchInfo{}, fmt.Errorf("could not map fzf selection to a branch")
	}
	return branches[index], nil
}
