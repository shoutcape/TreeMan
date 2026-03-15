package cli

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/shoutcape/TreeMan/internal/forge"
	"github.com/shoutcape/TreeMan/internal/git"
	"github.com/shoutcape/TreeMan/internal/ui"
	"github.com/spf13/cobra"
)

var worktreeReviewCmd = &cobra.Command{
	Use:   "review [number]",
	Short: "Create a review worktree from a PR/MR",
	Long: `Create a worktree for reviewing a pull request (GitHub) or merge request (GitLab).

If no PR/MR number is given, opens an interactive fzf picker showing open PRs/MRs.
The forge type (GitHub/GitLab) is auto-detected from the origin remote URL.

Prints the new worktree path to stdout on success.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWorktreeReview,
}

func runWorktreeReview(cmd *cobra.Command, args []string) error {
	if !git.IsInsideRepo() {
		return fmt.Errorf("not inside a git repository")
	}

	f, err := forge.Detect()
	if err != nil {
		return err
	}

	if err := f.CheckCLI(); err != nil {
		return err
	}

	var prNumber int

	if len(args) == 0 {
		// Interactive picker mode
		prNumber, err = pickPRNumber(f)
		if err != nil {
			return err
		}
	} else {
		prNumber, err = forge.ValidatePRNumber(args[0])
		if err != nil {
			return fmt.Errorf("%w\nUsage: treeman worktree review [pr-number]", err)
		}
	}

	meta, err := f.FetchPRMetadata(prNumber)
	if err != nil {
		return fmt.Errorf("failed to resolve PR/MR #%d with %s. Make sure the PR/MR exists and that origin points at a repo you can access", prNumber, f.CLITool())
	}

	if meta.Number == 0 || meta.HeadRef == "" {
		return fmt.Errorf("incomplete PR/MR metadata returned by %s", f.CLITool())
	}

	mainRoot, err := git.MainRoot()
	if err != nil {
		return fmt.Errorf("finding main worktree: %w", err)
	}

	worktreePath := git.WorktreePathForBranch(mainRoot, meta.HeadRef)

	// Guard against existing branch
	if git.LocalBranchExists(meta.HeadRef) {
		existing := git.FindWorktreeForBranch(meta.HeadRef)
		if existing != "" {
			return fmt.Errorf("branch '%s' already has a worktree at '%s'", meta.HeadRef, existing)
		}
		return fmt.Errorf("PR/MR head branch '%s' already exists locally", meta.HeadRef)
	}

	// Guard against existing directory
	if _, err := os.Stat(worktreePath); err == nil {
		return fmt.Errorf("directory '%s' already exists for branch '%s'", worktreePath, meta.HeadRef)
	}

	// Fetch the PR/MR ref
	fetchRef := f.FetchRef(meta.Number)
	fmt.Fprintf(os.Stderr, "Fetching PR/MR #%d from origin...\n", meta.Number)
	if err := git.Fetch("origin", fetchRef); err != nil {
		return err
	}

	// Create the worktree
	fmt.Fprintf(os.Stderr, "Creating review worktree at %s (branch: %s)...\n", worktreePath, meta.HeadRef)
	if err := git.WorktreeAdd(worktreePath, meta.HeadRef, "FETCH_HEAD"); err != nil {
		return err
	}

	git.CopyEnvFiles(mainRoot, worktreePath)

	fmt.Fprintf(os.Stderr, "Detecting dependencies...\n")
	if err := git.InstallDeps(worktreePath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: dependency installation failed: %v\n", err)
	}

	// Print review summary to stderr
	fmt.Fprintf(os.Stderr, "\nReview worktree ready:\n")
	fmt.Fprintf(os.Stderr, "  PR/MR:  #%d\n", meta.Number)
	fmt.Fprintf(os.Stderr, "  Title:  %s\n", meta.Title)
	fmt.Fprintf(os.Stderr, "  Branch: %s\n", meta.HeadRef)
	fmt.Fprintf(os.Stderr, "  Path:   %s\n", worktreePath)

	// Print path to stdout for shell wrapper to cd into
	fmt.Println(worktreePath)
	return nil
}

// pickPRNumber opens an fzf picker to select an open PR/MR.
func pickPRNumber(f forge.Forge) (int, error) {
	summaries, err := f.ListOpenPRs()
	if err != nil {
		return 0, fmt.Errorf("failed to list open PRs/MRs: %w", err)
	}

	if len(summaries) == 0 {
		return 0, fmt.Errorf("no open PRs/MRs found")
	}

	displayLines := forge.FormatPRPickerDisplay(summaries)

	selection, err := ui.Fzf(displayLines, ui.FzfOptions{
		BorderLabel: " open prs / mrs ",
		Prompt:      "review > ",
		Ansi:        true,
		ExtraArgs:   []string{"--header-lines=1"},
	})
	if err != nil {
		return 0, err
	}

	if selection == "" {
		return 0, fmt.Errorf("no PR/MR selected")
	}

	// Strip ANSI codes and extract the PR number from "#NNN" at the start
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	clean := ansiRegex.ReplaceAllString(selection, "")
	clean = strings.TrimSpace(clean)

	// First field should be "#NNN"
	fields := strings.Fields(clean)
	if len(fields) == 0 {
		return 0, fmt.Errorf("could not parse PR/MR number from selection")
	}

	numStr := strings.TrimPrefix(fields[0], "#")
	return forge.ValidatePRNumber(numStr)
}
