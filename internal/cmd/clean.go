package cmd

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/merge"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
)

func newCleanCmd() *cobra.Command {
	var dryRun bool
	var skipConfirm bool
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove clean worktrees with branches merged into the default branch",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClean(cmd, dryRun, skipConfirm)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show worktrees that would be removed")
	cmd.Flags().BoolVarP(&skipConfirm, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func runClean(cmd *cobra.Command, dryRun, skipConfirm bool) error {
	return runCleanWithClassifier(cmd, merge.NewClassifier(), dryRun, skipConfirm)
}

type cleanCandidate struct {
	entry       git.WorktreeEntry
	verifiedSHA string
}

type cleanSelection struct {
	candidates  []cleanCandidate
	diagnostics []merge.Diagnostic
}

const dirtyCheckWorkers = 4

func runCleanWithClassifier(cmd *cobra.Command, classifier merge.ClassifierFunc, dryRun, skipConfirm bool) error {
	render := commandRenderer(cmd)
	defaultBranch, err := git.DetectDefaultBranch()
	if err != nil {
		return err
	}
	entries, err := git.WorktreeList()
	if err != nil {
		return err
	}
	mainRoot, err := mainWorktreeRoot(entries)
	if err != nil {
		return err
	}

	out := cmd.ErrOrStderr()
	preview, err := selectCleanCandidates(classifier, defaultBranch, mainRoot, entries)
	if err != nil {
		return err
	}
	writeMergeDiagnostics(out, render, preview.diagnostics)
	if len(preview.candidates) == 0 {
		if dryRun {
			fmt.Fprintln(cmd.ErrOrStderr(), render.Status(ui.ToneInfo, "→", "Would remove 0 merged, clean worktree(s)."))
			return nil
		}
		fmt.Fprintln(cmd.ErrOrStderr(), render.Status(ui.ToneSuccess, "✓", "Removed 0 merged, clean worktree(s)."))
		return nil
	}
	candidates := preview.candidates

	// Remove the current worktree last so its process working directory remains valid.
	currentRoot, err := git.CurrentWorktreeRoot()
	if err != nil {
		return err
	}
	for i := range candidates {
		if samePath(candidates[i].entry.Path, currentRoot) {
			candidates = append(append(candidates[:i:i], candidates[i+1:]...), candidates[i])
			break
		}
	}

	if len(candidates) > 0 {
		branchWidth := len("BRANCH")
		for _, candidate := range candidates {
			branchWidth = max(branchWidth, len(candidate.entry.Branch))
		}

		// Stdout is reserved for the main worktree path when the current
		// worktree is removed, allowing the shell wrapper to navigate there.
		fmt.Fprintln(out, render.Title("Cleanup candidates"))
		fmt.Fprintln(out, render.Muted("Merged, clean worktrees and branches to remove"))
		fmt.Fprintln(out)
		fmt.Fprintf(out, "  %s  %s\n", render.Header(fmt.Sprintf("%-*s", branchWidth, "BRANCH")), render.Header("WORKTREE"))
		for _, candidate := range candidates {
			fmt.Fprintf(out, "  %s  %s\n", render.Branch(fmt.Sprintf("%-*s", branchWidth, candidate.entry.Branch)), render.Path(candidate.entry.Path))
		}
		fmt.Fprintln(out)
	}
	if dryRun {
		fmt.Fprintln(cmd.ErrOrStderr(), render.Status(ui.ToneInfo, "→", fmt.Sprintf("Would remove %d merged, clean worktree(s).", len(candidates))))
		return nil
	}
	if len(candidates) > 0 && !skipConfirm {
		confirmed, err := confirmYN(cmd, "Remove these worktrees and branches? [y/N] ")
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(cmd.ErrOrStderr(), render.Status(ui.ToneMuted, "○", "Cancelled."))
			return nil
		}
	}
	if len(candidates) > 0 {
		revalidated, err := selectCleanCandidates(classifier, defaultBranch, mainRoot, cleanCandidateEntries(candidates))
		if err != nil {
			return err
		}
		writeMergeDiagnostics(out, render, revalidated.diagnostics)
		candidates = revalidated.candidates
	}

	removed := 0
	for _, candidate := range candidates {
		// Candidates are verified merges: ancestors of the freshly fetched
		// default branch or forge-confirmed squash/rebase merges.
		if err := deleteWorktreeAtSHA(cmd, candidate.entry.Path, candidate.entry.Branch, mainRoot, false, true, candidate.verifiedSHA); err != nil {
			return err
		}
		removed++
	}
	fmt.Fprintln(cmd.ErrOrStderr(), render.Status(ui.ToneSuccess, "✓", fmt.Sprintf("Removed %d merged, clean worktree(s).", removed)))
	return nil
}

func selectCleanCandidates(classifier merge.ClassifierFunc, defaultBranch, mainRoot string, entries []git.WorktreeEntry) (cleanSelection, error) {
	eligible := make([]git.WorktreeEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Branch == "" || entry.Branch == defaultBranch || samePath(entry.Path, mainRoot) {
			continue
		}
		eligible = append(eligible, entry)
	}
	if len(eligible) == 0 {
		return cleanSelection{}, nil
	}

	dirtyResult := worktreeDirtyStates(eligible)
	dirty := <-dirtyResult
	if dirty.err != nil {
		return cleanSelection{}, dirty.err
	}
	cleanEntries := make([]git.WorktreeEntry, 0, len(eligible))
	for index, entry := range eligible {
		if !dirty.states[index] {
			cleanEntries = append(cleanEntries, entry)
		}
	}
	if len(cleanEntries) == 0 {
		return cleanSelection{}, nil
	}
	branches := make([]string, len(cleanEntries))
	for index, entry := range cleanEntries {
		branches[index] = entry.Branch
	}
	result, err := classifier(defaultBranch, branches)
	if err != nil {
		return cleanSelection{}, err
	}
	if err := validateCleanCandidates(branches, result.Cleanable); err != nil {
		return cleanSelection{}, err
	}
	cleanable := make(map[string]merge.Candidate, len(result.Cleanable))
	for _, candidate := range result.Cleanable {
		cleanable[candidate.Branch] = candidate
	}
	selection := cleanSelection{diagnostics: result.Diagnostics}
	for _, entry := range cleanEntries {
		candidate, ok := cleanable[entry.Branch]
		if ok {
			selection.candidates = append(selection.candidates, cleanCandidate{entry: entry, verifiedSHA: candidate.SHA})
		}
	}
	return selection, nil
}

// validateCleanCandidates ensures a classifier result cannot authorize deletion
// without identifying the exact requested branch tip.
func validateCleanCandidates(branches []string, candidates []merge.Candidate) error {
	requested := make(map[string]struct{}, len(branches))
	for _, branch := range branches {
		requested[branch] = struct{}{}
	}
	seen := make(map[string]struct{}, len(candidates))
	branchesToValidate := make([]string, len(candidates))
	for index, candidate := range candidates {
		branchesToValidate[index] = candidate.Branch
	}
	tips, err := git.BranchSHAs(branchesToValidate)
	if err != nil {
		return fmt.Errorf("could not resolve classifier branch tips: %w", err)
	}
	for _, candidate := range candidates {
		branch := candidate.Branch
		if _, duplicate := seen[branch]; duplicate {
			return fmt.Errorf("classifier returned duplicate cleanable branch %q", branch)
		}
		seen[branch] = struct{}{}
		if branch == "" {
			return fmt.Errorf("classifier returned cleanable branch without a name")
		}
		if candidate.SHA == "" {
			return fmt.Errorf("classifier returned cleanable branch %q without a SHA", branch)
		}
		if _, ok := requested[branch]; !ok {
			return fmt.Errorf("classifier returned unknown cleanable branch %q", branch)
		}
		if tips[branch] != candidate.SHA {
			return fmt.Errorf("classifier returned stale SHA for branch %q", branch)
		}
	}
	return nil
}

func mainWorktreeRoot(entries []git.WorktreeEntry) (string, error) {
	if len(entries) == 0 || entries[0].Path == "" {
		return "", fmt.Errorf("could not determine main worktree root")
	}
	return entries[0].Path, nil
}

func worktreeDirtyStates(entries []git.WorktreeEntry) <-chan struct {
	states []bool
	err    error
} {
	result := make(chan struct {
		states []bool
		err    error
	}, 1)
	go func() {
		states := make([]bool, len(entries))
		workers := min(dirtyCheckWorkers, len(entries))
		var next atomic.Int64
		var firstErr error
		var errMu sync.Mutex
		var wait sync.WaitGroup
		for range workers {
			wait.Add(1)
			go func() {
				defer wait.Done()
				for {
					index := int(next.Add(1) - 1)
					if index >= len(entries) {
						return
					}
					dirty, err := git.WorktreeDirty(entries[index].Path)
					if err != nil {
						errMu.Lock()
						if firstErr == nil {
							firstErr = err
						}
						errMu.Unlock()
						continue
					}
					states[index] = dirty
				}
			}()
		}
		wait.Wait()
		result <- struct {
			states []bool
			err    error
		}{states: states, err: firstErr}
	}()
	return result
}

func cleanCandidateEntries(candidates []cleanCandidate) []git.WorktreeEntry {
	entries := make([]git.WorktreeEntry, len(candidates))
	for index, candidate := range candidates {
		entries[index] = candidate.entry
	}
	return entries
}

func writeMergeDiagnostics(out interface{ Write([]byte) (int, error) }, render ui.Renderer, diagnostics []merge.Diagnostic) {
	for _, diagnostic := range diagnostics {
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", diagnostic.String()))
	}
}
