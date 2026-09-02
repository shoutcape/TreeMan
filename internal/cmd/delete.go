package cmd

import (
	"bufio"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shoutcape/treeman/internal/database"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
)

var (
	removeWorktree    = git.WorktreeRemove
	deleteBranchAtSHA = git.DeleteBranchAtSHA
	newCleanupBatch   = func() databaseCleanupBatch { return database.NewCleanupBatch() }
)

type databaseCleanupPreparer interface {
	Prepare(string) (func() error, string, error)
}

type databaseCleanupBatch interface {
	databaseCleanupPreparer
	Flush() error
}

type databaseCleanupStatus uint8

const (
	databaseCleanupUnavailable databaseCleanupStatus = iota
	databaseCleanupAbsent
	databaseCleanupPending
)

type databaseCleanupOutcome struct {
	status   databaseCleanupStatus
	database string
}

type deleteWorktreeOutcome struct {
	database        databaseCleanupOutcome
	currentWorktree bool
}

func newDeleteCmd() *cobra.Command {
	var flagPath string
	var flagBranch string
	var flagYes bool
	var flagForce bool

	cmd := &cobra.Command{
		Use:     "delete [query]",
		Short:   "Delete a worktree and its branch via fzf",
		Aliases: []string{"wtd"},
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagPath != "" || flagBranch != "" {
				return runDeleteDirect(cmd, flagPath, flagBranch, flagYes, flagForce)
			}
			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			return runDelete(cmd, query, flagYes, flagForce)
		},
	}

	cmd.Flags().StringVar(&flagPath, "path", "", "Worktree path to delete (skips fzf picker)")
	cmd.Flags().StringVar(&flagBranch, "branch", "", "Branch to delete (skips fzf picker)")
	cmd.Flags().BoolVarP(&flagYes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVarP(&flagForce, "force", "f", false, "Delete a worktree with uncommitted changes, or a branch whose commits exist nowhere else")
	return cmd
}

func runDeleteDirect(cmd *cobra.Command, path, branch string, skipConfirm, force bool) error {
	if path == "" || branch == "" {
		return fmt.Errorf("--path and --branch are both required in non-interactive mode")
	}
	if !git.IsInsideRepo() {
		return fmt.Errorf("not inside a git repository")
	}
	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return err
	}
	if !skipConfirm {
		printDeleteConfirmation(cmd, mainRoot, path, branch)
		confirmed, err := confirmYN(cmd, "Are you sure? [y/N] ")
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneMuted, "○", "Cancelled."))
			return nil
		}
	}
	return deleteWorktree(cmd, path, branch, mainRoot, force)
}

func runDelete(cmd *cobra.Command, query string, skipConfirm, force bool) error {
	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	if !git.IsInsideRepo() {
		return fmt.Errorf("not inside a git repository")
	}
	entries, err := git.WorktreeList()
	if err != nil {
		return err
	}
	if len(entries) <= 1 {
		fmt.Fprintln(out, "Only one worktree exists -- nothing to delete.")
		return nil
	}
	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return err
	}

	var displayLines, paths, branches []string
	for _, entry := range entries {
		if samePath(entry.Path, mainRoot) {
			continue
		}
		displayLines = append(displayLines, pickerRow(render.WorktreeRow(entry.Path, entry.Branch), len(paths)))
		paths = append(paths, entry.Path)
		branches = append(branches, entry.Branch)
	}
	if len(displayLines) == 0 {
		fmt.Fprintln(out, "No deletable worktrees -- only the main worktree exists.")
		return nil
	}
	if !canInteract(cmd) {
		return fmt.Errorf("interactive selection is unavailable; pass --path and --branch")
	}
	if _, err := exec.LookPath("fzf"); err != nil {
		return fmt.Errorf("fzf is required for delete. Install it from https://github.com/junegunn/fzf")
	}

	args := pickerArgs(sessionFor(cmd).errorOutput.Color, " delete worktree ", "delete > ")
	if query != "" {
		args = append(args, "--query", query)
	}
	fzfCmd := exec.Command("fzf", args...)
	fzfCmd.Stdin = strings.NewReader(strings.Join(displayLines, "\n"))
	fzfCmd.Stderr = out
	selectionOutput, err := fzfCmd.Output()
	if err != nil {
		if pickerCancelled(err) {
			fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "Cancelled."))
			return nil
		}
		return fmt.Errorf("fzf failed while selecting a worktree: %w", err)
	}
	selection := strings.TrimSpace(string(selectionOutput))
	if selection == "" {
		fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "Cancelled."))
		return nil
	}
	idx := pickerSelectionIndex(selection, len(paths))
	if idx < 0 {
		return fmt.Errorf("could not map fzf selection to a worktree")
	}
	if !skipConfirm {
		printDeleteConfirmation(cmd, mainRoot, paths[idx], branches[idx])
		confirmed, err := confirmYN(cmd, "Are you sure? [y/N] ")
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", "Cancelled."))
			return nil
		}
	}
	return deleteWorktree(cmd, paths[idx], branches[idx], mainRoot, force)
}

func deleteWorktree(cmd *cobra.Command, dest, branch, mainRoot string, force bool) error {
	return deleteWorktreeAtSHA(cmd, dest, branch, mainRoot, force, "")
}

// deleteVerifiedWorktree preserves the exact-SHA cleanup helper used by the
// merge classifier while routing it through database-aware deletion. The
// classifier authorized one specific tip, so a branch that has moved since is
// no longer the branch it verified.
func deleteVerifiedWorktree(cmd *cobra.Command, dest, branch, mainRoot string, force bool, expectedSHA string) error {
	if expectedSHA == "" {
		return fmt.Errorf("cannot delete verified branch %q without an expected SHA", branch)
	}
	return deleteWorktreeAtSHA(cmd, dest, branch, mainRoot, force, expectedSHA)
}

// deleteWorktreeAtSHA removes a worktree and its branch. An expected SHA makes
// cleanup conditional on the exact commit whose merge was verified. Every
// deletion snapshots its branch SHA and compare-and-deletes that exact ref.
func deleteWorktreeAtSHA(cmd *cobra.Command, dest, branch, mainRoot string, force bool, expectedSHA string) error {
	batch := newCleanupBatch()
	outcome, err := deleteWorktreeAtSHAWithDatabase(cmd, dest, branch, mainRoot, force, expectedSHA, batch)
	if err != nil {
		return err
	}
	if err := batch.Flush(); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneWarning, "!", fmt.Sprintf("database cleanup failed: %v", err)))
	}
	reportDeletedWorktree(cmd, branch, mainRoot, outcome)
	return nil
}

func reportDeletedWorktree(cmd *cobra.Command, branch, mainRoot string, outcome deleteWorktreeOutcome) {
	fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneSuccess, "✓", "Deleted worktree and branch: "+branch))
	if outcome.currentWorktree {
		fmt.Fprintln(cmd.OutOrStdout(), mainRoot)
	}
}

// deleteWorktreeAtSHAWithDatabase lets clean share one cleanup batch across
// worktrees while keeping ownership transitions inside the database package.
func deleteWorktreeAtSHAWithDatabase(cmd *cobra.Command, dest, branch, mainRoot string, force bool, expectedSHA string, batch databaseCleanupPreparer) (deleteWorktreeOutcome, error) {
	entries, err := git.WorktreeList()
	if err != nil {
		return deleteWorktreeOutcome{}, err
	}
	entry, err := worktreeEntryAt(entries, dest)
	if err != nil {
		return deleteWorktreeOutcome{}, err
	}
	if samePath(entry.Path, mainRoot) {
		return deleteWorktreeOutcome{}, fmt.Errorf("cannot delete the main worktree")
	}
	if entry.Branch != branch {
		return deleteWorktreeOutcome{}, fmt.Errorf("worktree %q is checked out on branch %q, not %q", entry.Path, entry.Branch, branch)
	}
	// A lock says "do not remove this", and --force is the user waiving their
	// own uncommitted changes, never a claim about the lock. Git refuses a
	// locked worktree too, so the guard is here for the message rather than
	// the outcome -- and it has to precede the staleness check below, because
	// a legitimately absent directory (removable media, an unmounted share) is
	// the case the lock exists for and must not be read as an abandoned one.
	if entry.Locked {
		reason := ""
		if entry.LockReason != "" {
			reason = ": " + entry.LockReason
		}
		return deleteWorktreeOutcome{}, fmt.Errorf("worktree %q is locked%s; run `git -C %q worktree unlock %q` first", entry.Path, reason, mainRoot, entry.Path)
	}
	defaultBranch, err := resolveDefaultBranch(entries, mainRoot)
	if err != nil {
		return deleteWorktreeOutcome{}, fmt.Errorf("cannot delete branch %q because the default branch could not be detected: %w", branch, err)
	}
	if branch == defaultBranch {
		return deleteWorktreeOutcome{}, fmt.Errorf("cannot delete the default branch %q", branch)
	}
	branchSHA, err := git.BranchSHA(branch)
	if err != nil {
		return deleteWorktreeOutcome{}, fmt.Errorf("cannot remove worktree %q because branch %q could not be resolved: %w", entry.Path, branch, err)
	}
	if expectedSHA != "" && branchSHA != expectedSHA {
		return deleteWorktreeOutcome{}, fmt.Errorf("cannot remove worktree %q: branch %q moved after merge verification (expected %s, found %s)", entry.Path, branch, expectedSHA, branchSHA)
	}
	// A registration whose directory is gone has no working tree to protect
	// and nothing to identify, so both checks below are skipped rather than
	// failed on. `git worktree remove` still unregisters it and the branch
	// still goes through the same compare-and-delete, so this reaches the same
	// end state as a normal deletion instead of leaving behind a registration
	// and a branch that nothing can remove.
	directoryMissing := git.WorktreeDirectoryMissing(entry.Path)
	if !directoryMissing {
		// Identity before contents. Reading the working tree of a directory
		// that is no longer this worktree reports a foreign repository's
		// changes as this one's, and answers them with --force -- the one flag
		// that would carry the removal through.
		if err := git.EnsureHoldsWorktree(mainRoot, entry.Path); err != nil {
			return deleteWorktreeOutcome{}, err
		}
		state, err := git.InspectWorktree(entry.Path)
		if err != nil {
			return deleteWorktreeOutcome{}, err
		}
		if state == git.WorktreeStateDirty && !force {
			return deleteWorktreeOutcome{}, fmt.Errorf("worktree %q has uncommitted or untracked changes; use --force to delete it", entry.Path)
		}
	}
	// Committing work does not make it safe. Deleting a branch drops its
	// reflog along with the worktree's, so a commit that no remote and not the
	// default branch can reach has nothing left pointing at it -- the same
	// unrecoverable loss the dirty check refuses, one commit later. It is
	// checked whether or not the directory survives, because the loss is the
	// branch's, not the directory's.
	//
	// An expected SHA is a caller that already proved this exact commit merged
	// -- `clean` asks the forge, which sees the squash merge local history
	// cannot. Its evidence outranks this one, so the gate steps aside for it
	// rather than sending users back to --force for the ordinary case.
	if !force && expectedSHA == "" {
		unpushed, err := git.UnpushedCommitCount(mainRoot, branch, defaultBranch)
		if err != nil {
			return deleteWorktreeOutcome{}, fmt.Errorf("cannot tell whether branch %q has commits that exist nowhere else: %w; use --force to delete it anyway", branch, err)
		}
		if unpushed > 0 {
			return deleteWorktreeOutcome{}, fmt.Errorf("branch %q has %d %s on no remote and not on %s; push the branch or use --force to delete it", branch, unpushed, commitsWord(unpushed), defaultBranch)
		}
	}
	currentRoot, err := git.CurrentWorktreeRoot()
	if err != nil {
		return deleteWorktreeOutcome{}, err
	}

	// Snapshot the database target while the worktree environment still exists.
	// Cleanup itself runs only after Git deletion succeeds.
	var commitDatabaseCleanup func() error
	cleanupOutcome := databaseCleanupOutcome{status: databaseCleanupUnavailable}
	databaseName := ""
	if batch == nil {
		batch = newCleanupBatch()
	}
	// A worktree whose directory is gone cannot be asked which database it
	// owned -- the lookup resolves the record through the worktree's own Git
	// directory -- so there is nothing to prepare, and reporting that as a
	// failure would make an expected cleanup look broken. A drop it had
	// already recorded is still collected by the pending-cleanup retry.
	if directoryMissing {
		cleanupOutcome.status = databaseCleanupAbsent
	} else if commitDatabaseCleanup, databaseName, err = batch.Prepare(entry.Path); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneWarning, "!", fmt.Sprintf("database cleanup failed: %v", err)))
	} else if commitDatabaseCleanup == nil {
		cleanupOutcome.status = databaseCleanupAbsent
	} else {
		cleanupOutcome.status = databaseCleanupPending
		cleanupOutcome.database = databaseName
	}
	if err := removeWorktree(mainRoot, entry.Path, force); err != nil {
		return deleteWorktreeOutcome{}, deleteWorktreeFailure(err, "none", fmt.Sprintf("worktree %q, branch %q", entry.Path, branch), fmt.Sprintf("resolve the error, then retry: treeman delete --path %q --branch %q --yes%s", entry.Path, branch, forceFlag(force)))
	}
	if err := deleteBranchAtSHA(mainRoot, branch, branchSHA); err != nil {
		return deleteWorktreeOutcome{}, deleteWorktreeFailure(
			fmt.Errorf("branch %q was preserved after deletion checks: %w", branch, err),
			fmt.Sprintf("removed worktree %q", entry.Path),
			fmt.Sprintf("branch %q", branch),
			fmt.Sprintf("inspect branch %q, then delete it manually if appropriate: git -C %q branch -D %q", branch, mainRoot, branch),
		)
	}
	if commitDatabaseCleanup != nil {
		if err := commitDatabaseCleanup(); err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneWarning, "!", fmt.Sprintf("database cleanup could not be recorded for retry: %v", err)))
			cleanupOutcome.status = databaseCleanupUnavailable
		}
	}
	return deleteWorktreeOutcome{database: cleanupOutcome, currentWorktree: samePath(currentRoot, entry.Path)}, nil
}

func deleteWorktreeFailure(err error, completed, remaining, recovery string) error {
	return fmt.Errorf("%w\nCompleted: %s.\nRemaining: %s.\nRecovery: %s", err, completed, remaining, recovery)
}

func forceFlag(force bool) string {
	if force {
		return " --force"
	}
	return ""
}

func findWorktree(path string) (git.WorktreeEntry, error) {
	entries, err := git.WorktreeList()
	if err != nil {
		return git.WorktreeEntry{}, err
	}
	return worktreeEntryAt(entries, path)
}

func worktreeEntryAt(entries []git.WorktreeEntry, path string) (git.WorktreeEntry, error) {
	for _, entry := range entries {
		if samePath(entry.Path, path) {
			return entry, nil
		}
	}
	return git.WorktreeEntry{}, fmt.Errorf("path %q is not a linked worktree", path)
}

// resolveDefaultBranch names the branch the default-branch guard protects.
// origin/HEAD answers locally in every clone, and since Git 2.49 every fetch
// creates it too, so the remote is almost never consulted. When it cannot be
// read at all -- an older Git in a repository that was never cloned, or a
// remote that is not named origin -- the main worktree's own branch is the
// local answer, and it is free: the worktree list is already in hand. Falling
// back matters because the guard is not worth refusing every deletion over;
// before this, a repository whose remote is named anything but origin could
// not delete a worktree at all.
func resolveDefaultBranch(entries []git.WorktreeEntry, mainRoot string) (string, error) {
	branch, detectErr := git.DetectDefaultBranch()
	if detectErr == nil {
		return branch, nil
	}
	if branch, ok := mainWorktreeBranch(entries, mainRoot); ok {
		return branch, nil
	}
	return "", detectErr
}

// localDefaultBranch is resolveDefaultBranch without the remote round trip, for
// callers that only decorate output.
func localDefaultBranch(entries []git.WorktreeEntry, mainRoot string) (string, bool) {
	if branch, ok := git.LocalDefaultBranch(); ok {
		return branch, true
	}
	return mainWorktreeBranch(entries, mainRoot)
}

func mainWorktreeBranch(entries []git.WorktreeEntry, mainRoot string) (string, bool) {
	for _, entry := range entries {
		if samePath(entry.Path, mainRoot) && entry.Branch != "" {
			return entry.Branch, true
		}
	}
	return "", false
}

// samePath reports whether two paths name the same directory. Symlinked or
// relative paths reach one directory through different raw text, therefore
// TreeMan compares canonical paths.
func samePath(a, b string) bool {
	return canonicalPath(a) == canonicalPath(b)
}

// canonicalPath makes path absolute and resolves its symlinks. A path that the
// operating system cannot resolve keeps its cleaned absolute form, therefore a
// missing directory does not cause a failure.
func canonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(abs)
}

func printDeleteConfirmation(cmd *cobra.Command, mainRoot, path, branch string) {
	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", "About to delete:"))
	fmt.Fprintf(out, "  Worktree: %s\n", render.Path(path))
	fmt.Fprintf(out, "  Branch:   %s%s\n\n", render.Branch(branch), describeUnmergedCommits(mainRoot, branch))
}

// describeUnmergedCommits annotates the branch line with the work that would
// stop being reachable. It is decoration, so every failure to work it out --
// no default branch, an unreadable ref -- returns an empty string instead of
// an error: nothing here decides whether the deletion may proceed, and a
// prompt that cannot render a count is still a usable prompt.
func describeUnmergedCommits(mainRoot, branch string) string {
	entries, err := git.WorktreeList()
	if err != nil {
		return ""
	}
	defaultBranch, ok := localDefaultBranch(entries, mainRoot)
	if !ok || defaultBranch == branch {
		return ""
	}
	count, err := git.UnmergedCommitCount(mainRoot, branch, defaultBranch)
	if err != nil || count == 0 {
		return ""
	}
	return fmt.Sprintf("  (%d %s not on %s)", count, commitsWord(count), defaultBranch)
}

func commitsWord(count int) string {
	if count == 1 {
		return "commit"
	}
	return "commits"
}

func confirmYN(cmd *cobra.Command, prompt string) (bool, error) {
	if !canInteract(cmd) {
		return false, fmt.Errorf("confirmation required; rerun with --yes")
	}
	fmt.Fprint(cmd.ErrOrStderr(), prompt)
	scanner := bufio.NewScanner(cmd.InOrStdin())
	if scanner.Scan() {
		return strings.EqualFold(strings.TrimSpace(scanner.Text()), "y"), nil
	}
	return false, nil
}
