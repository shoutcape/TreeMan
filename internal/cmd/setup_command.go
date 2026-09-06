package cmd

import (
	"fmt"

	"github.com/shoutcape/treeman/internal/git"
	"github.com/spf13/cobra"
)

// rerunSetupOptions is what the caller asked one setup run to do.
type rerunSetupOptions struct {
	refreshEnv bool
	rerunHooks bool
	trustHooks bool

	skipEnv      bool
	skipDatabase bool
	skipDeps     bool
	skipHooks    bool
}

// setupTarget captures the Git identity selected for one setup run.
type setupTarget struct {
	git.WorktreeEntry
	commonDir  string
	worktreeID string
}

func newSetupCmd() *cobra.Command {
	var options rerunSetupOptions
	cmd := &cobra.Command{
		Use:   "setup [branch-or-path]",
		Short: "Rerun project setup in an existing worktree",
		Long: `Rerun project setup in an existing linked worktree.

Without an argument, setup repairs the worktree containing the current
directory. An argument selects a worktree by exact branch name or by path.

Setup never creates, removes, or recreates a worktree or a branch, and never
changes which branch a worktree has checked out.

Existing .env* files are preserved. Use --refresh-env to adopt the main
worktree's versions; the database name TreeMan owns is kept even then. A
database TreeMan already owns is verified and reused, never recreated.

Post-create hooks are skipped unless you pass --rerun-hooks, which requests
them but does not authorize them. Authorization comes from a saved approval or
from --trust-hooks, which authorizes this invocation only.

Individual setup steps are best effort: one that fails is reported as a
warning and the others still run.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			return runSetup(cmd, query, options)
		},
	}
	addRerunSetupFlags(cmd, &options)
	return cmd
}

func addRerunSetupFlags(cmd *cobra.Command, options *rerunSetupOptions) {
	cmd.Flags().BoolVar(&options.refreshEnv, "refresh-env", false, "Replace existing .env* files with the main worktree's")
	cmd.Flags().BoolVar(&options.rerunHooks, "rerun-hooks", false, "Request that post-create hooks run again")
	cmd.Flags().BoolVar(&options.trustHooks, "trust-hooks", false, "Trust post-create hooks for this invocation")
	cmd.Flags().BoolVar(&options.skipEnv, "skip-env", false, "Skip copying .env* files")
	cmd.Flags().BoolVar(&options.skipDatabase, "skip-database", false, "Skip branch database setup")
	cmd.Flags().BoolVar(&options.skipDeps, "skip-deps", false, "Skip dependency installation")
	cmd.Flags().BoolVar(&options.skipHooks, "skip-hooks", false, "Skip post-create hooks")

	// Registered once every flag exists, because cobra resolves a group's
	// members at declaration time.
	cmd.MarkFlagsMutuallyExclusive("refresh-env", "skip-env")
	cmd.MarkFlagsMutuallyExclusive("rerun-hooks", "skip-hooks")
	cmd.MarkFlagsMutuallyExclusive("trust-hooks", "skip-hooks")
}

// validate rejects contradictory requests before any work starts.
func (o rerunSetupOptions) validate() error {
	// Cobra has mutual exclusion but no "requires" relation, so this one is
	// checked here. Authorizing hooks must not be what asks them to run: a
	// user who passes only --trust-hooks has said what is allowed, not what
	// should happen.
	if o.trustHooks && !o.rerunHooks {
		return fmt.Errorf("--trust-hooks authorizes hooks but does not request them; add --rerun-hooks to run them")
	}
	return nil
}

// setupOptions maps the request onto the shared pipeline's options. Hooks off
// by default lives here rather than in the shared struct, so creation keeps
// its own meaning for skipHooks.
func (o rerunSetupOptions) setupOptions() creationSetupOptions {
	return creationSetupOptions{
		skipEnv:      o.skipEnv,
		skipDatabase: o.skipDatabase,
		skipDeps:     o.skipDeps,
		skipHooks:    o.skipHooks || !o.rerunHooks,
		trustHooks:   o.trustHooks,
	}
}

// hooksSkippedText distinguishes explicit skipping from the rerun default.
func (o rerunSetupOptions) hooksSkippedText() string {
	if o.skipHooks || o.rerunHooks {
		return "skipped (requested)"
	}
	return "skipped (pass --rerun-hooks)"
}

func runSetup(cmd *cobra.Command, query string, options rerunSetupOptions) error {
	if err := options.validate(); err != nil {
		return err
	}
	target, err := resolveSetupTarget(query)
	if err != nil {
		return err
	}
	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return err
	}
	// From the main worktree, never the selected branch: a branch may have
	// edited its own copy of the configuration.
	project, err := loadProjectPaths(mainRoot)
	if err != nil {
		return err
	}
	approval, err := approveSetupHooks(cmd, project, options)
	if err != nil {
		return err
	}

	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	return withSetupLock(target, func() error {
		// The target was chosen before the lock. Confirm it still describes
		// the same worktree before starting; external Git changes do not
		// participate in TreeMan's setup lock.
		if err := revalidateSetupTarget(target); err != nil {
			return err
		}
		policy := envPreserve
		if options.refreshEnv {
			policy = envRefresh
		}
		summary := runWorktreeSetup(out, render, worktreeSetup{
			mainRoot:      project.mainRoot,
			worktreePath:  target.Path,
			branch:        target.Branch,
			worktreeDir:   project.parentDir,
			projectConfig: project.config,
			hooks:         approval,
			options:       options.setupOptions(),
			environment:   policy,
			hooksSkipped:  options.hooksSkippedText(),
		})
		printSetupSummary(out, render, summary)
		return nil
	})
}

// approveSetupHooks resolves consent before the lock is taken, so a prompt
// never waits with another run's lock held.
func approveSetupHooks(cmd *cobra.Command, project projectPaths, options rerunSetupOptions) (hookApproval, error) {
	commands := project.config.PostCreateHooks()
	if !options.rerunHooks || options.skipHooks || len(commands) == 0 {
		return hookApproval{}, nil
	}
	return approveHooks(cmd, project, commands, options.trustHooks,
		"Hooks run again in an existing worktree, which may already contain your work")
}

// resolveSetupTarget selects the existing linked worktree to repair.
//
// Selection is exact. There is no picker and no fuzzy match: setup runs a
// project's own commands and can install into whatever it selects, so a near
// miss must be an error rather than a guess.
func resolveSetupTarget(query string) (setupTarget, error) {
	entries, err := git.WorktreeList()
	if err != nil {
		return setupTarget{}, err
	}

	pathOnly := query == ""
	if pathOnly {
		root, err := git.CurrentWorktreeRoot()
		if err != nil {
			return setupTarget{}, err
		}
		// --show-toplevel already answers from a subdirectory, so running
		// setup from anywhere inside a worktree selects that worktree.
		query = root
	}

	var matches []git.WorktreeEntry
	for _, entry := range entries {
		match := samePath(entry.Path, query)
		if !pathOnly {
			match = match || entry.Branch == query
		}
		if match {
			matches = append(matches, entry)
		}
	}
	if len(matches) == 0 {
		if pathOnly {
			return setupTarget{}, fmt.Errorf("%q is not a registered worktree", query)
		}
		return setupTarget{}, fmt.Errorf("no worktree matches %q by exact branch name or path", query)
	}
	if !pathOnly && len(matches) > 1 {
		return setupTarget{}, fmt.Errorf("%q matches more than one worktree (%s and %s); name one by its path", query, matches[0].Path, matches[1].Path)
	}

	if err := validateSetupTarget(matches[0]); err != nil {
		return setupTarget{}, err
	}
	commonDir, err := git.CommonDir(matches[0].Path)
	if err != nil {
		return setupTarget{}, err
	}
	commonDir = git.CanonicalPath(commonDir)
	worktreeID, err := git.WorktreeID(matches[0].Path)
	if err != nil {
		return setupTarget{}, err
	}
	return setupTarget{WorktreeEntry: matches[0], commonDir: commonDir, worktreeID: worktreeID}, nil
}

// validateSetupTarget rejects every target setup cannot repair. A detached
// worktree has no branch to own a database, and the main worktree is the
// source setup copies from, not a destination it writes to.
func validateSetupTarget(entry git.WorktreeEntry) error {
	if entry.Branch == "" {
		return fmt.Errorf("worktree %q has a detached HEAD; setup needs a branch", entry.Path)
	}
	// Staleness first: every question after this one runs Git inside the
	// directory, and a missing directory should say so plainly.
	state, err := git.InspectWorktree(entry.Path)
	if err != nil {
		return err
	}
	if state == git.WorktreeStateStale {
		return fmt.Errorf("worktree %q is registered but its directory is missing", entry.Path)
	}
	// WorktreeID both proves this is a registered linked worktree and rejects
	// the main worktree, which has no linked administration directory.
	if _, err := git.WorktreeID(entry.Path); err != nil {
		return fmt.Errorf("cannot set up %q: %w", entry.Path, err)
	}
	return nil
}

// revalidateSetupTarget confirms under the lock that the registration, path,
// branch, and Git identity chosen outside it still describe the same worktree.
func revalidateSetupTarget(target setupTarget) error {
	entries, err := git.WorktreeList()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !samePath(entry.Path, target.Path) {
			continue
		}
		if entry.Branch != target.Branch {
			return fmt.Errorf("worktree %q changed branch from %q to %q before setup started", target.Path, target.Branch, entry.Branch)
		}
		if err := validateSetupTarget(entry); err != nil {
			return err
		}
		commonDir, err := git.CommonDir(entry.Path)
		if err != nil {
			return err
		}
		commonDir = git.CanonicalPath(commonDir)
		if commonDir != target.commonDir {
			return fmt.Errorf("worktree %q changed Git common directory before setup started", target.Path)
		}
		worktreeID, err := git.WorktreeID(entry.Path)
		if err != nil {
			return err
		}
		if worktreeID != target.worktreeID {
			return fmt.Errorf("worktree %q changed Git worktree ID from %q to %q before setup started", target.Path, target.worktreeID, worktreeID)
		}
		return nil
	}
	return fmt.Errorf("worktree %q is no longer registered", target.Path)
}
