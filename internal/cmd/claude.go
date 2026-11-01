package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/shoutcape/treeman/internal/agentworktree"
	"github.com/shoutcape/treeman/internal/fsutil"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/shoutcape/treeman/internal/validate"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

// claudeAgent is the agent name stored in ownership records.
const claudeAgent = "claude"

// claudePRName matches the worktree name Claude Code gives a pull request
// (`claude --worktree "#123"`).
var claudePRName = regexp.MustCompile(`^pr-([0-9]+)$`)

// claudeHookInput is the part of Claude Code's hook input TreeMan reads.
// WorktreeCreate sends name and cwd; WorktreeRemove sends worktree_path.
type claudeHookInput struct {
	SessionID    string `json:"session_id"`
	Cwd          string `json:"cwd"`
	Name         string `json:"name"`
	WorktreePath string `json:"worktree_path"`
}

func newClaudeCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "claude",
		Short: "Create Claude Code worktrees with TreeMan",
		Long: `Integrate TreeMan with Claude Code's WorktreeCreate, WorktreeRemove, and
PermissionRequest hooks.

Run "treeman claude install" once to register the hooks in Claude Code's user
settings. Claude Code then creates worktrees for --worktree, subagents, and
background sessions with TreeMan's paths, branch naming, and setup, and enters
TreeMan worktrees without asking. "treeman claude uninstall" removes the hooks.`,
	}
	command.AddCommand(newClaudeWorktreeCreateCmd())
	command.AddCommand(newClaudeWorktreeRemoveCmd())
	command.AddCommand(newClaudePermissionRequestCmd())
	command.AddCommand(newClaudeInstallCmd())
	command.AddCommand(newClaudeUninstallCmd())
	return command
}

func newClaudeWorktreeCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "worktree-create",
		Short: "Create a worktree for Claude Code's WorktreeCreate hook",
		Long: `Read Claude Code's WorktreeCreate hook input on stdin and create a worktree.

The name "pr-<n>" checks out PR or MR <n> like "treeman review <n>". Any other
name becomes the branch feature/<name>, without a doubled feature- prefix.
Asking again for a name returns the worktree TreeMan created for it.

Only the worktree path is printed to stdout; everything else goes to stderr.
The command never prompts. Post-create hooks run only when a saved approval
matches them.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClaudeHookOutput(cmd, func(stdout io.Writer) error {
				input, err := readClaudeHookInput(cmd.InOrStdin())
				if err != nil {
					return err
				}
				path, err := runClaudeWorktreeCreate(cmd, input)
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(stdout, path)
				return err
			})
		},
	}
}

func newClaudeWorktreeRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "worktree-remove",
		Short: "Remove a worktree for Claude Code's WorktreeRemove hook",
		Long: `Read Claude Code's WorktreeRemove hook input on stdin and remove the worktree.

A worktree TreeMan created for Claude Code is removed even with uncommitted
changes; any other worktree is removed only when clean. The branch is deleted
only when TreeMan created it for Claude Code and it is merged into the default
branch or the main worktree's HEAD, including by a squash or rebase merge.
Every other branch is kept.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClaudeHookOutput(cmd, func(io.Writer) error {
				input, err := readClaudeHookInput(cmd.InOrStdin())
				if err != nil {
					return err
				}
				return runClaudeWorktreeRemove(cmd, input)
			})
		},
	}
}

func readClaudeHookInput(r io.Reader) (claudeHookInput, error) {
	var input claudeHookInput
	data, err := io.ReadAll(r)
	if err != nil {
		return input, fmt.Errorf("read hook input: %w", err)
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return input, fmt.Errorf("read hook input: %w", err)
	}
	return input, nil
}

// withClaudeHookOutput keeps standard output for the one line Claude Code
// reads from it. Claude Code takes the last line of stdout as the worktree
// path, so a dependency installer or hook that writes to an inherited stdout
// would hand it a wrong path. While fn runs, file descriptor 1 points at
// stderr, and only fn's own writer reaches the original stdout.
//
// It also leaves the controlling terminal, so nothing run from here can open
// /dev/tty to ask a question Claude Code cannot answer.
//
// Leaving the terminal is best effort: setsid fails for a process that already
// leads a process group, as one started directly by an interactive shell does.
// Claude Code starts hooks through a shell of its own, and the prompt
// variables set by preventPrompts cover the case where it fails.
func withClaudeHookOutput(cmd *cobra.Command, fn func(stdout io.Writer) error) error {
	if cmd.OutOrStdout() != os.Stdout || cmd.ErrOrStderr() != os.Stderr {
		return fn(cmd.OutOrStdout())
	}
	_, _ = syscall.Setsid()
	// The copy is close-on-exec: a child that inherited it could keep Claude
	// Code waiting on the pipe, or write something other than the path to it.
	saved, err := unix.FcntlInt(1, unix.F_DUPFD_CLOEXEC, 3)
	if err != nil {
		return fn(os.Stdout)
	}
	stdout := os.NewFile(uintptr(saved), "stdout")
	defer stdout.Close()
	if err := unix.Dup2(2, 1); err != nil {
		return fn(stdout)
	}
	defer unix.Dup2(saved, 1)
	return fn(stdout)
}

// preventPrompts makes Git, SSH, and the forge CLIs fail rather than ask for
// credentials. A configured SSH command, such as one naming a per-repository
// key, is kept and only told not to prompt.
//
// GIT_SSH names a program that Git runs in place of ssh, and GIT_SSH_COMMAND
// would override it, so a user's GIT_SSH is left as it is. Only an OpenSSH
// command is given BatchMode, because other programs do not take -o.
func preventPrompts(repo git.Repo) {
	os.Setenv("GIT_TERMINAL_PROMPT", "0")
	os.Setenv("GH_PROMPT_DISABLED", "1")
	os.Setenv("GCM_INTERACTIVE", "never")
	// Without BatchMode, the lost terminal and GIT_TERMINAL_PROMPT=0 still
	// keep a GIT_SSH program from prompting in most setups.
	if os.Getenv("GIT_SSH_COMMAND") == "" && os.Getenv("GIT_SSH") != "" {
		return
	}
	sshCommand := os.Getenv("GIT_SSH_COMMAND")
	if sshCommand == "" {
		sshCommand, _ = repo.ConfigValue("core.sshCommand")
	}
	if sshCommand == "" {
		sshCommand = "ssh"
	}
	fields := strings.Fields(sshCommand)
	if len(fields) > 0 && filepath.Base(fields[0]) == "ssh" && !strings.Contains(sshCommand, "BatchMode") {
		sshCommand += " -o BatchMode=yes"
	}
	os.Setenv("GIT_SSH_COMMAND", sshCommand)
}

// claudeProjectDir is the directory a hook resolves its repository from:
// CLAUDE_PROJECT_DIR, the directory the session started in, so a `cd` during
// the session cannot point the hook at another repository. Without it, the
// hook input's cwd is used, then the current directory.
func claudeProjectDir(input claudeHookInput) string {
	if dir := os.Getenv("CLAUDE_PROJECT_DIR"); dir != "" {
		return dir
	}
	if input.Cwd != "" {
		return input.Cwd
	}
	return "."
}

// claudeBranchName maps a Claude Code worktree name to a TreeMan branch.
func claudeBranchName(name string) (string, error) {
	slug := strings.TrimPrefix(name, "feature-")
	if slug == "" {
		return "", fmt.Errorf("worktree name %q leaves no branch name", name)
	}
	branch := "feature/" + slug
	if err := validate.BranchName(branch); err != nil {
		return "", fmt.Errorf("worktree name %q: %w", name, err)
	}
	return branch, nil
}

// claudeRepository is the repository a hook runs against.
type claudeRepository struct {
	git git.Repo
	// parentDir overrides the configured worktree directory for bare
	// repositories, whose default would be inside the Git directory.
	parentDir string
	store     *agentworktree.Store
}

func openClaudeRepository(dir string, forCreation bool) (claudeRepository, error) {
	if _, err := os.Stat(dir); err != nil {
		return claudeRepository{}, fmt.Errorf("cannot enter %q: %w", dir, err)
	}
	opened, err := git.Open(dir)
	if errors.Is(err, git.ErrNotRepository) {
		return claudeRepository{}, fmt.Errorf("%q is not inside a git repository", dir)
	}
	if err != nil {
		return claudeRepository{}, err
	}
	repo := claudeRepository{git: opened, store: agentworktree.Open(opened.CommonDir())}
	if forCreation && samePath(opened.MainRoot(), opened.CommonDir()) {
		repo.parentDir, err = bareWorktreeParent(opened)
		if err != nil {
			return claudeRepository{}, err
		}
	}
	return repo, nil
}

// bareWorktreeParent chooses where a bare repository's worktrees go when its
// configured directory would put them inside the Git directory. A bare
// repository kept as `project/.bare` next to a `project/.git` file gets
// `project/.worktrees`; any other gets a sibling `<name>-worktrees` directory
// of its own.
func bareWorktreeParent(repo git.Repo) (string, error) {
	commonDir := repo.CommonDir()
	project, err := loadProjectPaths(repo)
	if err != nil {
		return "", err
	}
	canonicalCommon, err := fsutil.CanonicalPath(commonDir)
	if err != nil {
		return "", err
	}
	canonicalParent, err := fsutil.CanonicalPath(project.parentDir)
	if err != nil {
		return "", err
	}
	if !fsutil.Contains(canonicalCommon, canonicalParent) {
		return "", nil
	}
	parent := filepath.Dir(commonDir)
	if _, err := os.Lstat(filepath.Join(parent, ".git")); err == nil {
		if linked, err := git.CommonDir(parent); err == nil && samePath(linked, commonDir) {
			return filepath.Join(parent, ".worktrees"), nil
		}
	}
	return strings.TrimSuffix(filepath.Clean(commonDir), ".git") + "-worktrees", nil
}

func runClaudeWorktreeCreate(cmd *cobra.Command, input claudeHookInput) (string, error) {
	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	if input.Name == "" {
		return "", fmt.Errorf("hook input has no worktree name")
	}
	repo, err := openClaudeRepository(claudeProjectDir(input), true)
	if err != nil {
		return "", err
	}
	preventPrompts(repo.git)

	if err := repo.dropRecordsForMissingBranches(); err != nil {
		return "", err
	}
	records, err := repo.store.List()
	if err != nil {
		return "", err
	}
	prMatch := claudePRName.FindStringSubmatch(input.Name)
	branch := ""
	if prMatch == nil {
		if branch, err = claudeBranchName(input.Name); err != nil {
			return "", err
		}
	}
	record, hasRecord := findClaudeRecord(records, input.Name, branch)
	if hasRecord && record.WorktreePath != "" {
		path, reusable, err := repo.reusableWorktree(record)
		if err != nil {
			return "", err
		}
		if reusable {
			fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", fmt.Sprintf("Reusing worktree for %s: %s", record.Branch, path)))
			return path, nil
		}
		record.WorktreePath = ""
	}

	options := creationSetupOptions{savedApprovalOnly: true}
	if prMatch != nil {
		// A PR worktree removed earlier keeps its branch and a record of
		// it. TreeMan review refuses an existing branch, so re-attach it.
		if hasRecord && record.WorktreePath == "" {
			if gone, err := repo.git.BranchMissing(record.Branch); err != nil {
				return "", err
			} else if !gone {
				fmt.Fprintln(cmd.ErrOrStderr(), commandRenderer(cmd).Status(ui.ToneWarning, "!", fmt.Sprintf("re-attaching the local branch %s as it was; it may be behind the PR head, so pull it to update", record.Branch)))
				return repo.attachBranch(cmd, input, record.Branch, record, options)
			}
		}
		return repo.createReview(cmd, input, prMatch[1], options)
	}

	entries, err := repo.git.WorktreeList()
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.Branch != branch {
			continue
		}
		if samePath(entry.Path, repo.git.MainRoot()) {
			return "", fmt.Errorf("branch %q is checked out in the main worktree; pick another name", branch)
		}
		return "", fmt.Errorf("branch %q is already checked out at %q, which TreeMan did not create for this Claude Code worktree name; pick another name", branch, entry.Path)
	}

	if !repo.git.BranchExists(branch) {
		return repo.createBranch(cmd, input, branch, options)
	}
	owner := agentworktree.Record{}
	if hasRecord && record.Branch == branch {
		owner = record
	}
	return repo.attachBranch(cmd, input, branch, owner, options)
}

// createBranch creates branch and its worktree. TreeMan owns the branch.
func (repo claudeRepository) createBranch(cmd *cobra.Command, input claudeHookInput, branch string, options creationSetupOptions) (string, error) {
	// A failing hook blocks the session, so a failed fetch falls back to the
	// cached default branch rather than stopping.
	return repo.provision(cmd, newBranchFromDefault(branch, fetchFallBackToCached), options, func(created git.CreatedWorktree) agentworktree.Record {
		return agentworktree.Record{
			Agent:         claudeAgent,
			Name:          input.Name,
			Branch:        branch,
			BranchCreated: true,
			CreatedSHA:    created.SHA,
			CreatedAt:     time.Now().UTC(),
			SessionID:     input.SessionID,
		}
	})
}

// attachBranch adds a worktree for an existing branch. Ownership carries over
// only from a record of that branch that TreeMan created, and only while it
// is still that branch; owner is the zero record when there is none.
func (repo claudeRepository) attachBranch(cmd *cobra.Command, input claudeHookInput, branch string, owner agentworktree.Record, options creationSetupOptions) (string, error) {
	return repo.provision(cmd, existingBranch(branch), options, func(git.CreatedWorktree) agentworktree.Record {
		next := agentworktree.Record{Agent: claudeAgent, Name: input.Name, Branch: branch, CreatedAt: time.Now().UTC(), SessionID: input.SessionID}
		if owner.Branch == branch && agentBranchOwned(repo.git, owner) {
			next.BranchCreated = true
			next.CreatedSHA = owner.CreatedSHA
			next.CreatedAt = owner.CreatedAt
		}
		return next
	})
}

// findClaudeRecord returns the record for a worktree name. Two names can map
// to one branch ("x" and "feature-x"), so a record for the same branch under
// another name counts too.
func findClaudeRecord(records []agentworktree.Record, name, branch string) (agentworktree.Record, bool) {
	if record, ok := agentworktree.Find(records, claudeAgent, name); ok && (branch == "" || record.Branch == branch) {
		return record, true
	}
	if branch == "" {
		return agentworktree.Record{}, false
	}
	for _, record := range records {
		if record.Agent == claudeAgent && record.Branch == branch {
			return record, true
		}
	}
	return agentworktree.Record{}, false
}

// createReview checks out a PR or MR the way `treeman review` does. The
// branch belongs to the PR, not to TreeMan, so only the worktree is recorded.
func (repo claudeRepository) createReview(cmd *cobra.Command, input claudeHookInput, number string, options creationSetupOptions) (string, error) {
	selected, err := selectReviewForWorktree(cmd, repo.git, number)
	if err != nil {
		return "", err
	}
	return repo.provisionAndRecord(cmd, selected.request(repo.parentDir, options), func(created git.CreatedWorktree) agentworktree.Record {
		return agentworktree.Record{
			Agent:     claudeAgent,
			Name:      input.Name,
			Branch:    created.Branch,
			CreatedAt: time.Now().UTC(),
			SessionID: input.SessionID,
		}
	})
}

// provision provisions a worktree for source in the repository.
func (repo claudeRepository) provision(cmd *cobra.Command, source worktreeSource, options creationSetupOptions, record func(git.CreatedWorktree) agentworktree.Record) (string, error) {
	return repo.provisionAndRecord(cmd, provisionRequest{
		repo:      repo.git,
		parentDir: repo.parentDir,
		source:    source,
		options:   options,
	}, record)
}

// provisionAndRecord provisions req and records the worktree before setup, so a
// hook that times out during a long install still leaves a worktree TreeMan
// knows it owns.
func (repo claudeRepository) provisionAndRecord(cmd *cobra.Command, req provisionRequest, record func(git.CreatedWorktree) agentworktree.Record) (string, error) {
	req.afterAdd = func(created git.CreatedWorktree) error {
		next := record(created)
		next.WorktreePath = git.CanonicalPath(created.Path)
		err := markClaudeWorktree(next.WorktreePath, next.Name)
		if err == nil {
			err = repo.store.Put(next)
		}
		if err != nil {
			return fmt.Errorf("could not record the worktree as created for Claude Code; worktree-remove will treat it as yours: %w", err)
		}
		return nil
	}
	provisioned, err := provisionWorktree(cmd, req)
	if err != nil {
		return "", err
	}
	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	path := git.CanonicalPath(provisioned.worktree.Path)
	fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Worktree ready:"))
	fmt.Fprintf(out, "  Branch: %s\n", render.Branch(provisioned.worktree.Branch))
	fmt.Fprintf(out, "  Path:   %s\n", render.Path(path))
	return path, nil
}

// reusableWorktree reports whether the worktree a record names is still the
// one TreeMan created: registered at that path, on that branch, and present
// on disk. A registration whose directory is gone is pruned so the name can
// be created again.
func (repo claudeRepository) reusableWorktree(record agentworktree.Record) (string, bool, error) {
	entries, err := repo.git.WorktreeList()
	if err != nil {
		return "", false, err
	}
	entry, err := worktreeEntryAt(entries, record.WorktreePath)
	if err != nil || entry.Branch != record.Branch || samePath(entry.Path, repo.git.MainRoot()) {
		return "", false, nil
	}
	if git.WorktreeDirectoryMissing(entry.Path) {
		if err := repo.git.PruneWorktrees(); err != nil {
			return "", false, err
		}
		return "", false, nil
	}
	if err := repo.git.EnsureHoldsWorktree(entry.Path); err != nil {
		return "", false, nil
	}
	if !claudeWorktreeMarked(entry.Path, record.Name) {
		return "", false, nil
	}
	return git.CanonicalPath(entry.Path), true, nil
}

// claudeWorktreeMarker is a file in a worktree's Git registration directory
// that names the Claude Code worktree it was created for. Git deletes the
// directory with the registration, so a worktree registered later at the same
// path -- by `git worktree add`, say -- has no marker and is never taken for
// the one the record describes.
const claudeWorktreeMarker = "treeman-claude-worktree"

func claudeWorktreeMarkerPath(path string) (string, error) {
	commonDir, err := git.CommonDir(path)
	if err != nil {
		return "", err
	}
	id, err := git.WorktreeID(path)
	if err != nil {
		return "", err
	}
	return filepath.Join(commonDir, "worktrees", id, claudeWorktreeMarker), nil
}

func markClaudeWorktree(path, name string) error {
	marker, err := claudeWorktreeMarkerPath(path)
	if err != nil {
		return err
	}
	return fsutil.AtomicWriteFile(marker, []byte(name+"\n"), 0o600)
}

func claudeWorktreeMarked(path, name string) bool {
	marker, err := claudeWorktreeMarkerPath(path)
	if err != nil {
		return false
	}
	data, err := os.ReadFile(marker)
	return err == nil && strings.TrimSpace(string(data)) == name
}

// dropRecordsForMissingBranches forgets records whose branch is gone, however
// it went, so a later branch of the same name starts with no claim on it.
func (repo claudeRepository) dropRecordsForMissingBranches() error {
	records, err := repo.store.List()
	if err != nil || len(records) == 0 {
		return err
	}
	// A branch that cannot be checked is kept: dropping a live record would
	// disown a worktree TreeMan created.
	missing := func(branch string) (bool, error) {
		return repo.git.BranchMissing(branch)
	}
	stale := false
	for _, record := range records {
		gone, err := missing(record.Branch)
		if err != nil {
			return err
		}
		if gone {
			stale = true
			break
		}
	}
	if !stale {
		return nil
	}
	return repo.store.Update(func(records []agentworktree.Record) ([]agentworktree.Record, error) {
		kept := records[:0:0]
		for _, record := range records {
			gone, err := missing(record.Branch)
			if err != nil {
				return nil, err
			}
			if !gone {
				kept = append(kept, record)
			}
		}
		return kept, nil
	})
}

// agentBranchOwned reports whether record's branch is still the one TreeMan
// created. A branch deleted outside TreeMan and re-created under the same
// name starts a new reflog, so its oldest entry is not the creation TreeMan
// recorded. A branch with no reflog -- the default in bare repositories --
// cannot be told apart, and is not claimed.
func agentBranchOwned(repo git.Repo, record agentworktree.Record) bool {
	if !record.BranchCreated || record.CreatedSHA == "" {
		return false
	}
	sha, born, ok, err := repo.BranchBirth(record.Branch)
	if err != nil || !ok {
		return false
	}
	return sha == record.CreatedSHA && born <= record.CreatedAt.Unix()
}

// agentBranchMerged reports whether tip is merged into the default branch,
// local or on origin, or into the main worktree's HEAD: either contained in
// it, or merged by a squash or rebase so that merging it again would change
// nothing. It reads only local refs.
func agentBranchMerged(repo git.Repo, tip string) (bool, error) {
	var targets []string
	if branch, ok := repo.LocalDefaultBranch(); ok {
		targets = append(targets, "refs/remotes/origin/"+branch, "refs/heads/"+branch)
	}
	targets = append(targets, "HEAD")
	for _, target := range targets {
		sha, ok, err := repo.ResolveCommit(target)
		if err != nil {
			return false, err
		}
		if !ok {
			continue
		}
		merged, err := repo.IsAncestor(tip, sha)
		if err != nil {
			return false, err
		}
		if merged {
			return true, nil
		}
		// A failed content comparison, such as unrelated histories, only
		// withholds the deletion: the branch is kept.
		if same, err := repo.ChangesMerged(sha, tip); err == nil && same {
			return true, nil
		}
	}
	return false, nil
}

func runClaudeWorktreeRemove(cmd *cobra.Command, input claudeHookInput) error {
	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	if input.WorktreePath == "" {
		return fmt.Errorf("hook input has no worktree_path")
	}
	if _, err := os.Lstat(input.WorktreePath); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	repo, err := openClaudeRepository(input.WorktreePath, false)
	if err != nil {
		return err
	}
	// Work from the main worktree: the worktree being removed is moved away
	// while Git still has to run.
	repo.git, err = git.Open(repo.git.MainRoot())
	if err != nil {
		return err
	}
	preventPrompts(repo.git)

	entry, err := findWorktree(repo.git, input.WorktreePath)
	if err != nil {
		return err
	}
	if samePath(entry.Path, repo.git.MainRoot()) {
		return fmt.Errorf("refusing to remove the main worktree %q", entry.Path)
	}
	records, err := repo.store.List()
	if err != nil {
		return err
	}
	record, recorded := agentworktree.FindByPath(records, entry.Path, samePath)
	recorded = recorded && record.Agent == claudeAgent && record.Branch == entry.Branch &&
		claudeWorktreeMarked(entry.Path, record.Name)

	guards := deletionGuards{force: recorded, keepBranch: true}
	tip := ""
	kept := ""
	switch {
	case entry.Branch == "":
	case !recorded:
		kept = "TreeMan did not create this worktree for Claude Code"
	case !agentBranchOwned(repo.git, record):
		kept = "TreeMan did not create this branch"
	default:
		tip, err = repo.git.BranchSHA(entry.Branch)
		if err != nil {
			return err
		}
		merged, err := agentBranchMerged(repo.git, tip)
		if err != nil {
			return err
		}
		// The default-branch guard needs the default branch, and asking the
		// remote for it is network work this hook should not wait on. The
		// main worktree's branch is the local answer planDeletion falls back
		// to as well.
		defaultBranch, known := repo.git.LocalDefaultBranch()
		if !known {
			if entries, err := repo.git.WorktreeList(); err == nil {
				defaultBranch, known = mainWorktreeBranch(entries, repo.git.MainRoot())
			}
		}
		switch {
		case !merged:
			kept = "it is not merged into the default branch or HEAD"
		case !known:
			kept = "the default branch is unknown"
		default:
			guards.keepBranch = false
			guards.mergeVerified = true
			guards.defaultBranch = defaultBranch
		}
	}

	plan, err := planDeletion(entry.Path, entry.Branch, repo.git, guards, tip)
	if err != nil && !guards.keepBranch && recorded {
		// Deleting the branch is the optional half. Remove the worktree and
		// keep the branch rather than fail a removal Claude Code asked for.
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("could not delete branch %s: %s", entry.Branch, strings.SplitN(err.Error(), "\n", 2)[0])))
		kept = "it could not be deleted"
		guards = deletionGuards{force: true, keepBranch: true}
		plan, err = planDeletion(entry.Path, entry.Branch, repo.git, guards, tip)
	}
	if err != nil {
		if !recorded {
			return fmt.Errorf("%w\nTreeMan did not create this worktree for Claude Code, so it removes it only when it is clean", err)
		}
		return err
	}
	batch := newCleanupBatch()
	if _, err := plan.execute(cmd, batch); err != nil {
		return err
	}
	if err := batch.Flush(); err != nil {
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("database cleanup failed: %v", err)))
	}
	fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Removed worktree "+entry.Path))
	switch {
	case !guards.keepBranch:
		fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Deleted merged branch "+entry.Branch))
	case entry.Branch != "":
		fmt.Fprintln(out, render.Status(ui.ToneMuted, "○", fmt.Sprintf("Kept branch %s: %s", entry.Branch, kept)))
	}
	return nil
}
