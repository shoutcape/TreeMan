package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/shoutcape/treeman/internal/database"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
)

var (
	removeWorktreeAndBranch = git.RemoveWorktreeAndBranch
	newCleanupBatch         = func() databaseCleanupBatch { return database.NewCleanupBatch() }
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
	cleanupJob      string
	cleanupStarted  bool
}

func newDeleteCmd() *cobra.Command {
	var flagPath string
	var flagBranch string
	var flagYes bool
	var flagForce bool

	cmd := &cobra.Command{
		Use:     "delete [query]",
		Short:   "Delete a worktree and its branch via fzf",
		Aliases: commandAliases("delete"),
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
	plan, err := planDeletion(path, branch, mainRoot, deletionGuards{force: force}, "")
	if err != nil {
		return err
	}
	if !skipConfirm {
		confirmedPlan, confirmed, err := confirmDeletion(cmd, plan)
		if err != nil || !confirmed {
			return err
		}
		plan = confirmedPlan
	}
	return runDeletionPlan(cmd, plan)
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
	plan, err := planDeletion(paths[idx], branches[idx], mainRoot, deletionGuards{force: force}, "")
	if err != nil {
		return err
	}
	if !skipConfirm {
		confirmedPlan, confirmed, err := confirmDeletion(cmd, plan)
		if err != nil || !confirmed {
			return err
		}
		plan = confirmedPlan
	}
	return runDeletionPlan(cmd, plan)
}

// deletionGuards says what the caller has already established about a branch,
// and therefore which refusals still apply. It is what --force and a forge
// verification each waive, named rather than inferred from another field.
type deletionGuards struct {
	// force is the user waiving their own work: uncommitted changes, and
	// commits that exist nowhere else.
	force bool
	// mergeVerified is a caller that proved this exact commit merged. `clean`
	// asks the forge, which sees the squash merge local history cannot, so its
	// evidence outranks the unreachable-commit guard and nothing else.
	mergeVerified bool
}

// deletionPlan is a deletion that has passed every refusal. It carries the
// facts the prompt reports and the removal acts on, so the loss the user
// confirms is the loss the guards weighed rather than a second estimate of it.
type deletionPlan struct {
	entry            git.WorktreeEntry
	mainRoot         string
	branchSHA        string
	defaultBranch    string
	directoryMissing bool
	// dirty and unreachable are what force waived. They are zero on any plan
	// that did not need waiving, which is what makes them safe to report.
	dirty       bool
	unreachable int
	guards      deletionGuards
}

// planDeletion decides whether a worktree and its branch may be removed, and
// returns the facts that decision rested on. Preflight refusals happen before
// confirmation; execution revalidates mutable state under the mutation lock.
// An expected SHA pins the exact commit whose merge was
// verified; a branch that has moved since is no longer the branch it verified.
func planDeletion(dest, branch, mainRoot string, guards deletionGuards, expectedSHA string) (deletionPlan, error) {
	entries, err := git.WorktreeList()
	if err != nil {
		return deletionPlan{}, repositoryUnavailable{err}
	}
	entry, branchSHA, err := git.ValidateWorktreeRemoval(mainRoot, entries, dest, branch, expectedSHA)
	if err != nil {
		return deletionPlan{}, err
	}
	defaultBranch, err := resolveDefaultBranch(entries, mainRoot)
	if err != nil {
		return deletionPlan{}, repositoryUnavailable{fmt.Errorf("cannot delete branch %q because the default branch could not be detected: %w", branch, err)}
	}
	if branch == defaultBranch {
		return deletionPlan{}, fmt.Errorf("cannot delete the default branch %q", branch)
	}
	plan := deletionPlan{
		entry:            entry,
		mainRoot:         mainRoot,
		branchSHA:        branchSHA,
		defaultBranch:    defaultBranch,
		directoryMissing: git.WorktreeDirectoryMissing(entry.Path),
		guards:           guards,
	}
	// A registration whose directory is gone has no working tree to protect
	// and nothing to identify, so both checks below are skipped rather than
	// failed on. `git worktree remove` still unregisters it and the branch
	// still goes through the same compare-and-delete, so this reaches the same
	// end state as a normal deletion instead of leaving behind a registration
	// and a branch that nothing can remove.
	if !plan.directoryMissing {
		// Identity before contents. Reading the working tree of a directory
		// that is no longer this worktree reports a foreign repository's
		// changes as this one's, and answers them with --force -- the one flag
		// that would carry the removal through.
		if err := git.EnsureHoldsWorktree(mainRoot, entry.Path); err != nil {
			return deletionPlan{}, err
		}
		state, err := git.InspectWorktree(entry.Path)
		if err != nil {
			return deletionPlan{}, err
		}
		plan.dirty = state == git.WorktreeStateDirty
		if plan.dirty && !guards.force {
			return deletionPlan{}, fmt.Errorf("worktree %q has uncommitted or untracked changes; use --force to delete it", entry.Path)
		}
	}
	// Committing work does not make it safe. Deleting a branch drops its
	// reflog along with the worktree's, so a commit that no remote and not the
	// default branch can reach has nothing left pointing at it -- the same
	// unrecoverable loss the dirty check refuses, one commit later. It is
	// checked whether or not the directory survives, because the loss is the
	// branch's, not the directory's.
	if !guards.mergeVerified {
		plan.unreachable, err = git.UnpushedCommitCount(mainRoot, branch, defaultBranch)
		if err != nil {
			return deletionPlan{}, fmt.Errorf("cannot tell whether branch %q has commits that exist nowhere else: %w\nRecovery: inspect the branch, then use --force to delete it anyway", branch, err)
		}
		if plan.unreachable > 0 && !guards.force {
			return deletionPlan{}, fmt.Errorf("branch %q has %d %s on no remote and not on %s; push the branch or use --force to delete it", branch, plan.unreachable, commitsWord(plan.unreachable), defaultBranch)
		}
	}
	return plan, nil
}

// replanAfterPrompt re-runs the guards once the user has answered. The answer
// was given against the state at plan time, and a prompt is human-sized: the
// removal acts on the worktree as it is now, and work committed while we
// waited stops the deletion rather than disappearing into it.
func replanAfterPrompt(plan deletionPlan) (deletionPlan, error) {
	fresh, err := planDeletion(plan.entry.Path, plan.entry.Branch, plan.mainRoot, plan.guards, "")
	if err != nil {
		return deletionPlan{}, err
	}
	if fresh.branchSHA != plan.branchSHA {
		return deletionPlan{}, fmt.Errorf("branch %q moved while the prompt was open (was %s, now %s); rerun the deletion to see what changed", plan.entry.Branch, plan.branchSHA, fresh.branchSHA)
	}
	return fresh, nil
}

// deleteVerifiedWorktree removes a worktree whose branch a caller proved
// merged, sharing that caller's cleanup batch. The classifier authorized one
// specific tip, so a branch that has moved since is no longer the branch it
// verified, and without a tip there is no verification to speak of.
func deleteVerifiedWorktree(cmd *cobra.Command, dest, branch, mainRoot, expectedSHA string, batch databaseCleanupPreparer) (deleteWorktreeOutcome, error) {
	if expectedSHA == "" {
		return deleteWorktreeOutcome{}, fmt.Errorf("cannot delete verified branch %q without an expected SHA", branch)
	}
	plan, err := planDeletion(dest, branch, mainRoot, deletionGuards{mergeVerified: true}, expectedSHA)
	if err != nil {
		return deleteWorktreeOutcome{}, removalRefused{err}
	}
	return plan.execute(cmd, batch)
}

// runDeletionPlan executes a plan with a cleanup batch of its own and reports
// what it removed.
func runDeletionPlan(cmd *cobra.Command, plan deletionPlan) error {
	batch := newCleanupBatch()
	outcome, err := plan.execute(cmd, batch)
	if err != nil {
		return err
	}
	if err := batch.Flush(); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneWarning, "!", fmt.Sprintf("database cleanup failed: %v", err)))
	}
	return reportDeletedWorktree(cmd, plan.entry.Branch, plan.mainRoot, outcome)
}

func reportDeletedWorktree(cmd *cobra.Command, branch, mainRoot string, outcome deleteWorktreeOutcome) error {
	fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneSuccess, "✓", "Deleted worktree and branch: "+branch))
	// Queued is not the same as running: a detach that failed leaves files
	// behind for a later removal to retry, and the warning above already said
	// so. Announcing background progress here as well would contradict it.
	if outcome.cleanupStarted && git.CleanupPending(outcome.cleanupJob) {
		fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneMuted, "○", "File cleanup continues in the background."))
	}
	if !outcome.currentWorktree {
		return nil
	}
	// The caller's shell is standing in a worktree that no longer exists, so
	// send it back to the main worktree.
	return reportDestination(cmd, mainRoot)
}

// execute carries out a plan: the removal itself, the branch's exact-SHA
// deletion, and the database the worktree owned. Removal revalidates the plan
// under the mutation lock and can refuse if the state has changed.
func (plan deletionPlan) execute(cmd *cobra.Command, batch databaseCleanupPreparer) (deleteWorktreeOutcome, error) {
	entry := plan.entry
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
	if plan.directoryMissing {
		cleanupOutcome.status = databaseCleanupAbsent
	} else if commitDatabaseCleanup, databaseName, err = batch.Prepare(entry.Path); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneWarning, "!", fmt.Sprintf("database cleanup failed: %v", err)))
	} else if commitDatabaseCleanup == nil {
		cleanupOutcome.status = databaseCleanupAbsent
	} else {
		cleanupOutcome.status = databaseCleanupPending
		cleanupOutcome.database = databaseName
	}
	removal, err := removeWorktreeAndBranch(plan.mainRoot, entry.Path, entry.Branch, plan.branchSHA, plan.guards.force)
	if removal.CleanupError != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneWarning, "!", fmt.Sprintf("pending file cleanup needs retry: %v", removal.CleanupError)))
	}
	if err != nil {
		return deleteWorktreeOutcome{}, plan.removalFailure(err)
	}
	if commitDatabaseCleanup != nil {
		if err := commitDatabaseCleanup(); err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneWarning, "!", fmt.Sprintf("database cleanup could not be recorded for retry: %v", err)))
			cleanupOutcome.status = databaseCleanupUnavailable
		}
	}
	return deleteWorktreeOutcome{
		database:        cleanupOutcome,
		currentWorktree: samePath(currentRoot, entry.Path),
		cleanupJob:      removal.CleanupJob,
		cleanupStarted:  removal.CleanupStarted,
	}, nil
}

// removalFailure turns the removal's own account of what it left behind into
// the error a batch acts on. Which durable transitions completed cannot answer
// that on its own: a worktree that is still registered because its captured
// directory could not be put back is not a worktree that was merely refused,
// and both report the same unregistration. So the scope decides, and only the
// scope that claims nothing changed lets a batch continue: an unrecognized one
// and an unclassified failure both claim nothing at all, and stop the run.
func (plan deletionPlan) removalFailure(err error) error {
	entry := plan.entry
	remaining := fmt.Sprintf("worktree %q, branch %q", entry.Path, entry.Branch)
	retry := fmt.Sprintf("resolve the error, then retry: treeman delete --path %q --branch %q --yes%s", entry.Path, entry.Branch, forceFlag(plan.guards.force))
	// A failure that classified nothing supports no claim about what survived
	// it, so its report names what to look at rather than what is still there.
	unknown := deleteWorktreeFailure(err, "unknown",
		fmt.Sprintf("unknown; inspect worktree %q and branch %q", entry.Path, entry.Branch),
		fmt.Sprintf("confirm what survived, then retry: treeman delete --path %q --branch %q --yes%s", entry.Path, entry.Branch, forceFlag(plan.guards.force)))
	var failure *git.RemovalError
	if !errors.As(err, &failure) {
		return unknown
	}
	switch failure.Scope {
	case git.RemovalScopeCandidate:
		return removalRefused{deleteWorktreeFailure(err, "none", remaining, retry)}
	case git.RemovalScopeRepository:
		return repositoryUnavailable{deleteWorktreeFailure(err, "none", remaining, retry)}
	case git.RemovalScopeCaptureRetained:
		// Git kept both resources, but the working tree is no longer where the
		// registration says it is. Restoring it is a move TreeMan already tried
		// and could not make -- most likely because something occupies the
		// path -- so the report names both locations and leaves the move to
		// someone who can see what is in the way.
		return deleteWorktreeFailure(
			fmt.Errorf("worktree %q was captured for removal and could not be restored: %w", entry.Path, err),
			fmt.Sprintf("moved the worktree directory for %q into the cleanup queue at %q", entry.Path, failure.Capture),
			fmt.Sprintf("worktree %q registered while its directory sits at %q, branch %q", entry.Path, failure.Capture, entry.Branch),
			fmt.Sprintf("inspect %q and whatever now occupies %q, restore the directory there once the path is free, then retry: treeman delete --path %q --branch %q --yes%s", failure.Capture, entry.Path, entry.Path, entry.Branch, forceFlag(plan.guards.force)),
		)
	case git.RemovalScopeBranchRetained:
		return deleteWorktreeFailure(
			fmt.Errorf("branch %q was preserved after worktree removal: %w", entry.Branch, err),
			fmt.Sprintf("removed worktree %q", entry.Path),
			fmt.Sprintf("branch %q", entry.Branch),
			fmt.Sprintf("inspect branch %q, then delete it manually if appropriate: git -C %q branch -D %q", entry.Branch, plan.mainRoot, entry.Branch),
		)
	default:
		return unknown
	}
}

// removalRefused marks a deletion the removal itself reported as a decision
// about one worktree, having left every Git resource in place and nothing
// captured. A batch can skip such a candidate and keep going, whereas a removal
// that got further -- or that could not read what every candidate needs -- must
// stop the run so the user can act on the state it left behind.
type removalRefused struct{ err error }

func (r removalRefused) Error() string { return r.err.Error() }

func (r removalRefused) Unwrap() error { return r.err }

// repositoryUnavailable marks a failure to read repository-wide state rather
// than a decision about one worktree. Nothing was removed, but the next
// candidate would fail the same way, so a batch must stop instead of
// reporting the repository's problem once per worktree as if each had been
// individually refused.
type repositoryUnavailable struct{ err error }

func (r repositoryUnavailable) Error() string { return r.err.Error() }

func (r repositoryUnavailable) Unwrap() error { return r.err }

// refusedRemoval reports whether err is a refusal that changed nothing and
// says nothing about the candidates still to come. It reads the classification
// the removal supplied rather than re-deriving one.
func refusedRemoval(err error) bool {
	var unavailable repositoryUnavailable
	if errors.As(err, &unavailable) {
		return false
	}
	// Planning carries the same classification: a registration directory that
	// cannot be read is unreadable for every candidate, not a decision about
	// the one that happened to reach it first.
	var failure *git.RemovalError
	if errors.As(err, &failure) && failure.Scope != git.RemovalScopeCandidate {
		return false
	}
	var refused removalRefused
	return errors.As(err, &refused)
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
//
// It shares git.CanonicalPath with the removal itself deliberately. These two
// answers are compared against each other -- planning decides which worktree
// the removal then acts on -- so two implementations that disagree about a
// path would disagree about which worktree that is.
func samePath(a, b string) bool {
	return git.CanonicalPath(a) == git.CanonicalPath(b)
}

// confirmDeletion puts the plan to the user and, once they answer, re-runs it
// against the state as it is now, returning the plan the removal should act
// on. It reports what --force waived, because that is the only work a plan
// that got this far can still destroy.
func confirmDeletion(cmd *cobra.Command, plan deletionPlan) (deletionPlan, bool, error) {
	printDeleteConfirmation(cmd, plan)
	confirmed, err := confirmYN(cmd, "Are you sure? [y/N] ", errConfirmationRequired)
	if err != nil {
		return deletionPlan{}, false, err
	}
	if !confirmed {
		fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneMuted, "○", "Cancelled."))
		return deletionPlan{}, false, nil
	}
	fresh, err := replanAfterPrompt(plan)
	if err != nil {
		return deletionPlan{}, false, err
	}
	return fresh, true, nil
}

func printDeleteConfirmation(cmd *cobra.Command, plan deletionPlan) {
	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", "About to delete:"))
	fmt.Fprintf(out, "  Worktree: %s\n", render.Path(plan.entry.Path))
	fmt.Fprintf(out, "  Branch:   %s%s\n", render.Branch(plan.entry.Branch), describeUnreachableCommits(plan))
	if plan.dirty {
		fmt.Fprintln(out, "  Discards: uncommitted and untracked changes")
	}
	fmt.Fprintln(out)
}

// describeUnreachableCommits annotates the branch with the work the deletion
// ends: commits no remote and not the default branch can reach, which lose the
// last thing pointing at them when the branch and its reflog go. A plan only
// carries a count when --force waived it, so the prompt warns about work that
// is genuinely about to be destroyed and stays quiet about work that is not.
// It reads as the refusal reads, because it is the same count: what the prompt
// reports is what the guard weighed, word for word.
func describeUnreachableCommits(plan deletionPlan) string {
	if plan.unreachable == 0 {
		return ""
	}
	return fmt.Sprintf("  (%d %s on no remote and not on %s)", plan.unreachable, commitsWord(plan.unreachable), plan.defaultBranch)
}

func commitsWord(count int) string {
	if count == 1 {
		return "commit"
	}
	return "commits"
}

// errConfirmationRequired is what a command whose confirmation can be waived
// with --yes reports when it has no way to ask. A caller that offers different
// guidance passes its own.
var errConfirmationRequired = errors.New("confirmation required; rerun with --yes")

// confirmYN puts a yes/no question to the user and reports their answer.
// A session that cannot be prompted gets unavailable, which names the flag
// that decides the question without asking; a clean end of input is a no, and
// only a read that actually failed is an error.
func confirmYN(cmd *cobra.Command, prompt string, unavailable error) (bool, error) {
	if !canInteract(cmd) {
		return false, unavailable
	}
	fmt.Fprint(cmd.ErrOrStderr(), prompt)
	scanner := bufio.NewScanner(cmd.InOrStdin())
	if scanner.Scan() {
		answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
		return answer == "y" || answer == "yes", nil
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	return false, nil
}
