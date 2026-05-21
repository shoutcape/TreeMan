package cmd

import (
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
	"github.com/shoutcape/treeman/internal/terminal"
	_ "github.com/shoutcape/treeman/internal/terminal/ghostty"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/shoutcape/treeman/internal/worktree"
	"github.com/spf13/cobra"
)

func newBranchCmd() *cobra.Command {
	var flagNoOpen bool

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
			return runBranch(cmd, query, flagNoOpen)
		},
	}

	cmd.Flags().BoolVarP(&flagNoOpen, "no-open", "n", false, "Skip opening a terminal tab/pane")

	return cmd
}

func runBranch(cmd *cobra.Command, query string, noOpen bool) error {
	if !git.IsInsideRepo() {
		return fmt.Errorf("not inside a git repository")
	}

	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return err
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
		return fmt.Errorf("%s is required for branch listing with %s repos. Install it from %s",
			cliTool, forgeType, cliInstallURL(forgeType))
	}

	// For GitLab we also need jq.
	if forgeType == forge.GitLab {
		if _, err := exec.LookPath("jq"); err != nil {
			return fmt.Errorf("jq is required for branch listing with GitLab repos. Install it from https://jqlang.github.io/jq/")
		}
	}

	// Fetch branches from forge API.
	fmt.Fprintln(os.Stderr, "Fetching remote branches...")
	allBranches, err := forge.BranchList(forgeType, repoSlug, host)
	if err != nil {
		return fmt.Errorf("failed to list remote branches: %w", err)
	}

	// Detect default branch to exclude it.
	defaultBranch, _ := git.DetectDefaultBranch()

	// Filter out default branch, protected branches, and locally existing ones.
	var branches []forge.BranchInfo
	for _, b := range allBranches {
		if b.Name == defaultBranch {
			continue
		}
		if git.BranchExists(b.Name) {
			continue
		}
		branches = append(branches, b)
	}

	if len(branches) == 0 {
		return fmt.Errorf("no remote branches available (all already exist locally or only default branch found)")
	}

	// Fetch open MRs/PRs and build a map by branch name.
	fmt.Fprintln(os.Stderr, "Checking open MRs/PRs...")
	prMap := make(map[string]forge.PRInfo)
	prs, err := forge.PRList(forgeType, repoSlug, host)
	if err != nil {
		// Non-fatal: show branches without MR info.
		fmt.Fprintf(os.Stderr, "Warning: could not fetch MRs/PRs: %v\n", err)
	} else {
		for _, pr := range prs {
			prMap[pr.Branch] = pr
		}
	}

	// Pick a branch.
	selected, err := pickBranch(branches, query, prMap)
	if err != nil {
		return err
	}

	branch := selected.Name
	worktreePath := worktree.PathForBranch(mainRoot, branch)

	// Guard: directory must not exist.
	if _, err := os.Stat(worktreePath); err == nil {
		return fmt.Errorf("directory %q already exists for branch %q", worktreePath, branch)
	}

	// Fetch the branch from origin.
	fmt.Fprintf(os.Stderr, "Fetching branch %s from origin...\n", branch)
	if err := git.Fetch(branch); err != nil {
		return fmt.Errorf("failed to fetch branch %q: %w", branch, err)
	}

	// Create the worktree tracking the remote branch.
	fmt.Fprintf(os.Stderr, "Creating worktree at %s (branch: %s)...\n", worktreePath, branch)
	if err := git.WorktreeAddExisting(worktreePath, branch); err != nil {
		return err
	}

	// Set upstream so git pull/push work.
	if err := git.SetUpstreamInDir(worktreePath, branch); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not set upstream for %q: %v\n", branch, err)
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
		fmt.Fprintln(os.Stderr, "Detected Python project -- skipping auto-install (activate your venv manually).")
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

	// Print summary to stderr.
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Worktree ready:")
	fmt.Fprintf(os.Stderr, "  Branch: %s\n", branch)
	if pr, ok := prMap[branch]; ok {
		fmt.Fprintf(os.Stderr, "  MR/PR:  #%d - %s\n", pr.Number, pr.Title)
	}
	fmt.Fprintf(os.Stderr, "  Path:   %s\n", worktreePath)

	// Print path to stdout so the shell wrapper can cd into it.
	// Skip when a terminal was opened -- the user is already there.
	if !terminalOpened {
		fmt.Fprintln(cmd.OutOrStdout(), worktreePath)
	}

	return nil
}

// pickBranch opens an fzf picker populated with remote branches and returns
// the selected branch. If query is provided, it pre-filters the list.
// If query is an exact match, it auto-selects without showing the picker.
// prMap maps branch names to their associated PR/MR info (may be nil).
func pickBranch(branches []forge.BranchInfo, query string, prMap map[string]forge.PRInfo) (forge.BranchInfo, error) {
	// If query is an exact match, skip fzf.
	if query != "" {
		for _, b := range branches {
			if b.Name == query {
				return b, nil
			}
		}
	}

	if _, err := exec.LookPath("fzf"); err != nil {
		if query != "" {
			return forge.BranchInfo{}, fmt.Errorf("no exact match for %q and fzf is not installed for interactive selection", query)
		}
		return forge.BranchInfo{}, fmt.Errorf("fzf is required to pick a remote branch; pass an exact branch name or install fzf")
	}

	// Build display lines.
	var sb strings.Builder
	sb.WriteString(ui.BranchHeader())
	sb.WriteByte('\n')
	for _, b := range branches {
		mrNumber := 0
		if pr, ok := prMap[b.Name]; ok {
			mrNumber = pr.Number
		}
		sb.WriteString(ui.BranchRow(b.Name, b.Date, mrNumber))
		sb.WriteByte('\n')
	}

	// Pipe to fzf.
	fzfArgs := []string{
		"--ansi",
		"--border-label", " remote branches ",
		"--header-lines=1",
		"--prompt=branch > ",
		"--select-1",
		"--exit-0",
	}
	if query != "" {
		fzfArgs = append(fzfArgs, "--query", query)
	}

	fzfCmd := exec.Command("fzf", fzfArgs...)
	fzfCmd.Stdin = strings.NewReader(sb.String())
	fzfCmd.Stderr = os.Stderr

	out, err := fzfCmd.Output()
	if err != nil {
		return forge.BranchInfo{}, fmt.Errorf("no branch selected")
	}

	selection := strings.TrimSpace(string(out))
	if selection == "" {
		return forge.BranchInfo{}, fmt.Errorf("no branch selected")
	}

	// Strip ANSI codes and extract the branch name (first field).
	plain := ui.StripANSI(selection)
	branchName := strings.TrimSpace(strings.Fields(plain)[0])

	// Find the matching BranchInfo.
	for _, b := range branches {
		if b.Name == branchName {
			return b, nil
		}
	}

	return forge.BranchInfo{}, fmt.Errorf("could not match selection %q to a branch", branchName)
}
