package cmd

import (
	"fmt"

	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
)

// Worktree provisioning: creating a worktree, then running its setup steps.
//
// Every command that makes a worktree goes through provisionWorktree, so the
// rules that live in the order of the steps are kept in one place:
//
//   - a branch that would be created must not already exist locally;
//   - hook consent and the destination are settled before the network is
//     touched, so an unusable destination costs no fetch;
//   - afterAdd runs once the worktree exists and before setup, which can be
//     slow;
//   - the warm-start offer comes after the summary.
//
// Callers choose where the branch comes from (a worktreeSource), then print
// their own banner and deliver the worktree.

// fetchPolicy says what a new branch does when fetching its start point fails.
type fetchPolicy int

const (
	// fetchStrict stops creation when the fetch fails.
	fetchStrict fetchPolicy = iota
	// fetchFallBackToCached warns and starts from the cached origin default
	// branch, or from HEAD when there is none. A failing Claude Code hook
	// blocks the session, so agent creation prefers a stale start to none.
	fetchFallBackToCached
)

type sourceKind int

const (
	sourceNewFromDefault sourceKind = iota
	sourceTrackRemote
	sourcePullRequestHead
	sourceExisting
)

// worktreeSource is where a provisioned worktree's branch comes from. The set
// is closed: each kind knows how to fetch, add, and track its branch.
type worktreeSource struct {
	kind   sourceKind
	branch string
	fetch  fetchPolicy
	// number and fetchRef name the PR or MR for sourcePullRequestHead. The
	// caller computes fetchRef, so provisioning does not depend on forges.
	number   int
	fetchRef string
}

// newBranchFromDefault creates branch from the latest default branch.
func newBranchFromDefault(branch string, fetch fetchPolicy) worktreeSource {
	return worktreeSource{kind: sourceNewFromDefault, branch: branch, fetch: fetch}
}

// trackRemoteBranch creates a local branch tracking origin's branch.
func trackRemoteBranch(branch string) worktreeSource {
	return worktreeSource{kind: sourceTrackRemote, branch: branch}
}

// pullRequestHead creates branch at the head of PR or MR number, fetched
// through fetchRef, and tracks origin's branch when origin has it.
func pullRequestHead(branch string, number int, fetchRef string) worktreeSource {
	return worktreeSource{kind: sourcePullRequestHead, branch: branch, number: number, fetchRef: fetchRef}
}

// existingBranch adds a worktree for a local branch without fetching.
func existingBranch(branch string) worktreeSource {
	return worktreeSource{kind: sourceExisting, branch: branch}
}

// provisionRequest describes one worktree to provision.
type provisionRequest struct {
	mainRoot string
	// parentDir overrides the configured worktree directory; empty uses
	// project configuration.
	parentDir string
	source    worktreeSource
	options   creationSetupOptions
	// afterAdd runs once the worktree exists and before setup. Its error is
	// printed as a warning: the worktree is already there to use.
	afterAdd func(git.CreatedWorktree) error
}

// addedWorktree is a worktree that exists but has not been set up.
type addedWorktree struct {
	paths    creationPaths
	worktree git.CreatedWorktree
}

// provisionedWorktree is a worktree after provisioning.
type provisionedWorktree struct {
	addedWorktree
	summary setupSummary
}

// provisionWorktree creates the worktree the request describes and sets it up.
// An error means no worktree was created; setup problems are warnings in the
// summary.
func provisionWorktree(cmd *cobra.Command, req provisionRequest) (provisionedWorktree, error) {
	added, err := addWorktree(cmd, req)
	if err != nil {
		return provisionedWorktree{}, err
	}
	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	if req.afterAdd != nil {
		if err := req.afterAdd(added.worktree); err != nil {
			fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", err.Error()))
		}
	}
	summary := setupCreatedWorktree(out, render, added.paths, added.worktree)
	fmt.Fprintln(out)
	printSetupSummary(out, render, summary)
	// Agents create worktrees where no one can answer a prompt.
	if !req.options.savedApprovalOnly {
		offerWarmStarts(cmd, added.paths.projectPaths, req.options.skipDeps)
	}
	return provisionedWorktree{addedWorktree: added, summary: summary}, nil
}

// reprovisionWorktree runs setup again in an existing worktree, for
// `treeman setup`. It never creates, removes, or switches a worktree or a
// branch.
func reprovisionWorktree(cmd *cobra.Command, target setupTarget, options rerunSetupOptions) error {
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
	err = withSetupLock(target, func() error {
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
	if err == nil {
		offerWarmStarts(cmd, project, options.skipDeps)
	}
	return err
}

// addWorktree runs provisioning up to the add: guard, consent and destination,
// fetch, add, and tracking. Benchmarks time this part on its own.
func addWorktree(cmd *cobra.Command, req provisionRequest) (addedWorktree, error) {
	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	source := req.source
	branch := source.branch
	progress := func(message string) { fmt.Fprintln(out, render.Status(ui.ToneInfo, "→", message)) }
	warn := func(message string) { fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", message)) }

	// A strict new branch settles its default branch before consent, as
	// create always has: detection may ask the remote, and a repository
	// without a default branch has nothing to offer.
	var defaultBranch string
	if source.kind == sourceNewFromDefault && source.fetch == fetchStrict {
		var err error
		if defaultBranch, err = git.DetectDefaultBranch(); err != nil {
			return addedWorktree{}, err
		}
	}

	if err := source.guard(); err != nil {
		return addedWorktree{}, err
	}

	// Settle where the worktree goes before the network is touched. A
	// destination that cannot be used is not worth a fetch, and the
	// configuration that chooses it is the one setup will work from.
	// Existing worktrees keep their path; a branch whose slug collides with
	// another branch's worktree gets a suffixed path.
	paths, err := prepareApprovedCreationPaths(cmd, req.mainRoot, branch, req.parentDir, req.options)
	if err != nil {
		return addedWorktree{}, err
	}

	var created git.CreatedWorktree
	switch source.kind {
	case sourceNewFromDefault:
		startPoint := "origin/" + defaultBranch
		if source.fetch == fetchStrict {
			progress(fmt.Sprintf("Fetching latest %s from origin...", defaultBranch))
			if err := git.Fetch(defaultBranch); err != nil {
				return addedWorktree{}, err
			}
		} else if startPoint, err = fallbackStartPoint(progress, warn); err != nil {
			return addedWorktree{}, err
		}
		progress(fmt.Sprintf("Creating worktree at %s (branch: %s)...", paths.path, branch))
		created, err = git.CreatePlannedWorktree(paths.plan(branch), branch, startPoint)

	case sourceTrackRemote:
		progress(fmt.Sprintf("Fetching branch %s from origin...", branch))
		if err := git.FetchRemoteBranch(branch); err != nil {
			return addedWorktree{}, fmt.Errorf("failed to fetch branch %q: %w", branch, err)
		}
		progress(fmt.Sprintf("Creating worktree at %s (branch: %s)...", paths.path, branch))
		created, err = git.CreatePlannedWorktree(paths.plan(branch), branch, "origin/"+branch)
		if err == nil {
			// Set upstream so git pull/push work.
			if err := git.SetUpstreamInDir(created.Path, branch); err != nil {
				warn(fmt.Sprintf("could not set upstream for %q: %v", branch, err))
			}
		}

	case sourcePullRequestHead:
		progress(fmt.Sprintf("Fetching PR/MR #%d from origin...", source.number))
		sha, fetchErr := git.FetchCommit(source.fetchRef)
		if fetchErr != nil {
			return addedWorktree{}, fetchErr
		}
		progress(fmt.Sprintf("Creating review worktree at %s (branch: %s)...", paths.path, branch))
		created, err = git.CreatePlannedWorktree(paths.plan(branch), branch, sha)
		if err == nil {
			// Fetch the branch by name so origin/<branch> exists, then set
			// upstream so git pull/push work without explicit remote args.
			// Non-fatal: fork PRs may not have the branch on origin.
			if err := git.Fetch(branch); err != nil {
				warn(fmt.Sprintf("could not fetch branch %q (upstream not set): %v", branch, err))
			} else if err := git.SetUpstreamInDir(created.Path, branch); err != nil {
				warn(fmt.Sprintf("could not set upstream for %q: %v", branch, err))
			}
		}

	case sourceExisting:
		progress(fmt.Sprintf("Attaching existing branch %s at %s...", branch, paths.path))
		created, err = git.CreatePlannedWorktreeForExistingBranch(paths.plan(branch), branch)

	default:
		return addedWorktree{}, fmt.Errorf("unknown worktree source %d", source.kind)
	}
	if err != nil {
		return addedWorktree{}, err
	}
	return addedWorktree{paths: paths, worktree: created}, nil
}

// guard refuses a source whose branch would be created over a local branch.
func (s worktreeSource) guard() error {
	switch s.kind {
	case sourceNewFromDefault:
		if git.BranchExists(s.branch) {
			return fmt.Errorf("branch %q already exists locally", s.branch)
		}
	case sourcePullRequestHead:
		if git.BranchExists(s.branch) {
			if existing, _ := git.FindWorktreeForBranch(s.branch); existing != "" {
				return fmt.Errorf("branch %q already has a worktree at %q", s.branch, existing)
			}
			return fmt.Errorf("PR/MR head branch %q already exists locally", s.branch)
		}
	}
	return nil
}

// fallbackStartPoint finds a start point for fetchFallBackToCached: the
// freshly fetched origin default branch, else the cached one, else HEAD.
func fallbackStartPoint(progress, warn func(string)) (string, error) {
	defaultBranch, known := git.LocalDefaultBranch()
	if !known {
		if _, err := git.OriginRemoteURL(); err == nil {
			if branch, err := git.DetectDefaultBranch(); err == nil {
				defaultBranch, known = branch, true
			} else {
				warn(fmt.Sprintf("could not detect the default branch: %v", err))
			}
		}
	}
	if known {
		progress(fmt.Sprintf("Fetching latest %s from origin...", defaultBranch))
		fetchErr := git.Fetch(defaultBranch)
		_, cached, err := git.RemoteTrackingBranchSHA(defaultBranch)
		if err != nil {
			return "", err
		}
		if fetchErr == nil && cached {
			return "origin/" + defaultBranch, nil
		}
		if cached {
			warn(fmt.Sprintf("could not fetch %s (%v); starting from the cached origin/%s", defaultBranch, fetchErr, defaultBranch))
			return "origin/" + defaultBranch, nil
		}
		if fetchErr != nil {
			warn(fmt.Sprintf("could not fetch %s: %v", defaultBranch, fetchErr))
		}
	}
	if _, ok, err := git.ResolveCommit("HEAD"); err != nil {
		return "", err
	} else if !ok {
		return "", fmt.Errorf("the repository has no commits to start a branch from")
	}
	warn("no origin default branch is available; starting from HEAD")
	return "HEAD", nil
}
