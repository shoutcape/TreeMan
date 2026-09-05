# Command Reference

Run `treeman --help` for current command help. TreeMan sends status and warnings to stderr. Commands that select, create, or delete a worktree report a destination directory for the caller's shell.

Shell integration uses that destination to change the current interactive shell directory. Native commands never change the caller directory. Read [How a destination reaches your shell](#how-a-destination-reaches-your-shell).

`create`, `branch`, `review`, and `switch` accept `--exec` (`-x`). Read [Run a command in the worktree](#run-a-command-in-the-worktree).

## Commands

| Native command | Shell wrapper | Purpose |
| --- | --- | --- |
| `treeman create <branch>` | `wt` | Create a branch and worktree |
| `treeman branch [query]` | `wtb` | Add a remote branch worktree |
| `treeman review [number]` | `wtpr`, `wtmr` | Add a PR or MR worktree |
| `treeman switch [query]` | `wts` | Select a worktree path |
| `treeman list [--json]` | `wtl` | List worktrees and their status |
| `treeman benchmark [command]` | None | Measure command execution time |
| `treeman clean [--dry-run] [--yes]` | `wtc` | Remove clean worktrees merged into the default branch |
| `treeman delete [query]` | `wtd` | Delete a linked worktree and branch |
| `treeman shell` | None | Install and manage shell integration |
| `treeman doctor` | None | Check repository readiness and configuration |
| `treeman preflight` | None | Report setup compatibility before creation |
| `treeman theme` | None | Select a terminal color theme |
| `treeman version` | None | Print build data |

## Run a Command in the Worktree

`create`, `branch`, `review`, and `switch` accept `--exec <command>`, with short form `-x <command>`.

TreeMan runs the command in the ready worktree. The system shell runs the command string, so quoting, arguments, and operators work as they do in `.treeman.toml` hooks.

```text
treeman create feature/login -x claude
treeman review 42 -x nvim
treeman switch feature/login -x lazygit
```

`--exec` replaces the TreeMan process with the command. Therefore:

- The command owns the terminal and can be interactive.
- The command reports its own exit status to your shell.
- TreeMan reports no destination. Status output before the handover stays on stderr.
- Shell integration does not change your directory. When the command exits, your shell stays where it was.

Shell integration needs no `--exec` handling of its own. It never captures TreeMan's output, so a launched command inherits the terminal whatever the flags were.

A post-create hook is not an alternative. TreeMan runs hooks to completion and captures their output, which an interactive command cannot use.

`switch --exec` runs the command in the selected worktree, also when that worktree is the current directory.

TreeMan fails before it creates a worktree when `--exec` has no command.

## `create`

```text
treeman create <branch-name> [-x <command>] [--skip-env] [--skip-database] [--skip-deps] [--skip-hooks]
```

Create a local branch from the fetched default branch. TreeMan reads `refs/remotes/origin/HEAD` to detect that branch. If that ref is absent, TreeMan looks for `main` or `master` on `origin`. The command fails when it finds neither name.

The command fails when the branch or target directory exists. It creates `.worktrees/<branch-slug>`. The branch slug changes `/` to `-`.

If a different branch holds that path, TreeMan adds a slug suffix and creates `.worktrees/<branch-slug>-<slug-suffix>`. Existing worktrees keep their paths.

TreeMan then updates `.gitignore`, copies `.env*` files, loads configuration, sets up a database, installs dependencies, and runs hooks. These later actions are warning-only.

Dependency installation supports `pnpm-lock.yaml`, `yarn.lock`, `package-lock.json`, `go.mod`, and `Cargo.toml`. A `package.json` that declares `"packageManager": "yarn@<version>"` takes priority and runs `corepack yarn install`; otherwise `yarn.lock` runs `yarn install`. Corepack must already be available on `PATH`. TreeMan does not enable it or modify global package-manager configuration.

Use any `--skip-*` flag to omit its named optional setup action. TreeMan lists requested skips in the final summary.

Use `-x <command>` to run a command in the new worktree instead of printing its path. Read [Run a command in the worktree](#run-a-command-in-the-worktree).

## `preflight`

```text
treeman preflight
```

Report whether environment file copy, dependency installation, database setup, and post-create hooks can run for the current repository. The report reads repository files, checks required dependency installers, and checks the configured PostgreSQL container without creating a worktree, branch, or database, or running hooks. For Corepack-managed Yarn projects, it reports `corepack yarn install` and warns when `corepack` is unavailable. Nested modules are reported but remain skipped; use trusted post-create hooks for their setup.

## `branch`

```text
treeman branch [query] [-x <command>] [--skip-env] [--skip-database] [--skip-deps] [--skip-hooks]
```

`branch` has alias `wtb`. With an exact branch name, it fetches the branch directly without `fzf` or a forge CLI. Otherwise, it gets all remote branches from the detected forge and uses `fzf`. For GitHub, TreeMan obtains ordered branch batches from paginated REST responses through a bounded concurrent window, and it fills the MR/PR column by asking each branch for its own open PR in concurrent batches. It streams rows into `fzf` as those batches land, after seeding the picker with a preview of the first branches, so results appear before the full list has been fetched. Asking per branch also keeps a fork's PR from being reported against a same-named branch in the repository. GitLab branch records are read from `glab` as NDJSON before being combined with MR results.

TreeMan stops a branch or review list at 5000 items or at 50 pages. Read [known limitations](../known-limitations.md).

TreeMan excludes the default branch and local branches. It does not exclude protected branches.

After selection, TreeMan fetches the branch, creates a local branch, and tries to set upstream tracking. It then runs the create post-actions.

Use `-x <command>` to run a command in the new worktree instead of printing its path. Read [Run a command in the worktree](#run-a-command-in-the-worktree).

## `review`

```text
treeman review [pr-number] [-x <command>] [--skip-env] [--skip-database] [--skip-deps] [--skip-hooks]
```

`review` has aliases `wtpr` and `wtmr`. TreeMan detects GitHub or GitLab from `origin`.

Give a positive numeric PR or MR number. Without a number, TreeMan uses `fzf` to select an open PR or MR. TreeMan opens `fzf` immediately and streams GitHub batches or GitLab NDJSON records as they arrive. You can filter and select before all results arrive. Closing the picker stops the requests that were still fetching the rest.

TreeMan fetches the review head into a new worktree. It runs environment, database, dependency, and hook actions.

Use `-x <command>` to run a command in the review worktree instead of printing its path. Read [Run a command in the worktree](#run-a-command-in-the-worktree).

## `switch`

```text
treeman switch [query] [-x <command>]
```

`switch` has alias `wts`. An exact branch name or worktree path selects the matching worktree without `fzf`. Other input requires `fzf`.

A path query resolves its symlinks before the comparison. Therefore a symlinked path selects the same worktree as its real path.

It reports the selected path as the shell's destination. It returns success without a destination when you cancel selection or select the current directory. A symlinked current directory counts as the current worktree. Shell integration changes directory when it receives a path.

With `-x`, the command runs in the selected worktree and no path is printed. Read [Run a command in the worktree](#run-a-command-in-the-worktree).

## `delete`

```text
treeman delete [query]
treeman delete --path <path> --branch <branch> [--yes] [--force]
```

`delete` has alias `wtd`. Interactive deletion requires `fzf`. It excludes the main worktree.

`--path` and `--branch` use direct mode. Direct mode requires both flags. `--yes` and `-y` skip confirmation.

TreeMan verifies that `--path` names a linked worktree and that it has the supplied branch. It protects the main worktree and detected default branch. Deletion loads the worktree's durable database ownership record, removes the worktree and branch, marks the owned database pending, then drops it before the command returns. It never derives deletion authority from `.env`.

`delete` does not check whether the branch is merged. You named the treebranch and confirmed it, and what protects work that is not recoverable is the worktree being clean and the branch's commits existing somewhere else. `treeman clean` is the merge-aware command: it deletes only what it can prove is merged, using forge evidence for squash and rebase merges that local history cannot show. Deciding merge status here would only duplicate that with a weaker local check.

Every refusal is decided before the prompt, so a deletion that cannot proceed says so instead of asking you to confirm it first. What the prompt then reports is what `--force` waived and nothing else: the branch line is annotated with how many of its commits exist nowhere else, and a `Discards:` line names uncommitted and untracked changes. A deletion that loses nothing says nothing, because there is nothing left to warn about.

Answering the prompt re-runs the checks against the state as it is now, and a branch whose tip moved while the prompt was open is refused rather than deleted under an answer given before that work existed. `--yes` skips the prompt, and with it both the annotation and the second pass.

The path is local in every repository that can name its default branch locally, which is the ordinary case: Git writes `origin/HEAD` on clone and refreshes it on fetch. Only when that ref is unreadable does default-branch detection fall back to asking `origin` directly, and it falls back again to the main worktree's own branch when it cannot. Nothing else in `delete` contacts a remote -- the unreachable-commit count reads local refs only.

TreeMan refuses deletion when the worktree has staged, modified, or untracked files. It also refuses when the branch holds commits that no remote-tracking ref and not the default branch can reach: deleting a branch drops its reflog along with the worktree's, so those commits have nothing left pointing at them. Committing work does not make it safe; pushing it does. Use `--force` only when you intend to lose that work -- it means that and nothing else. `--yes` skips the confirmation prompt but does not bypass safety checks.

The count is `git rev-list --count <branch> --not --remotes <default branch>`, and every part of it is local. A branch pushed to any remote passes, whether or not it merged and whether or not it tracks that remote. A branch merged into the default branch passes, so a repository with no remote at all does not have to force every deletion. A squash-merged branch whose remote branch was already pruned does not pass, because nothing local can tell those commits from work that was never pushed -- run `treeman clean`, which has the forge evidence this check cannot get, or `--force` if you know the merge landed.

`clean` is exempt: it deletes an exact commit it verified as merged with the forge, and that evidence outranks this check.

A locked worktree is never removed, and `--force` does not override the lock: `git worktree lock` is how you mark a worktree whose directory is legitimately absent sometimes, such as one on removable media. Unlock it first.

A worktree whose directory was removed outside TreeMan is still deleted: the registration is unregistered and the branch goes through the same compare-and-delete as any other deletion, rather than leaving both behind with nothing able to remove them. Its recorded database cannot be resolved without the directory, so any drop it had already recorded is left to the pending-cleanup retry.

Removal stages the worktree directory in the repository's Git common directory at `treeman/trash/` and hands unlinking to a background process, so the command can succeed after Git unregisters the worktree and deletes its branch without waiting for a large dependency tree to be unlinked. Git deletion success therefore does not mean the staged files have already disappeared. TreeMan retries queued file cleanup only during a subsequent worktree removal. Preflight checks identity before confirmation; after staging, TreeMan validates the captured directory against Git's registration and checks its contents before unregistering it. A worktree on a different filesystem cannot be staged by rename, so removal is refused without deleting it.

If a process stops before marking a staged directory ready for cleanup, or TreeMan cannot restore it after validation or Git removal fails, the directory remains protected for manual recovery. A later removal of the associated worktree is refused with the retained staged location, even if its original directory is absent. Recover it manually from that location after resolving any path conflict. Removing the Git registration does not make unresolved captured data disposable; only explicitly ready jobs (and legacy metadata-free trash entries) are retried automatically.

When the deleted worktree is the current directory, TreeMan reports the main worktree path so shell integration can change directory safely.

## `list`

```text
treeman list [--json]
```

List repository worktrees with branch, path, main, current, dirty, stale, and merged state. TreeMan checks the current default-branch tip on the remote. It fetches the branch only when local tracking data is missing or different. `YES` in `MERGED` means the tip is an ancestor of the refreshed default branch. It can also mean the exact tip was the head of a merged PR or MR. For GitHub, it can mean the tip is reachable from a merged PR head, or that it descends from one. A tip that descends from a merged head is informational only, because it carries commits the merge never saw. `clean` removes a deleted branch after a squash or rebase merge only when forge evidence accounts for every commit on the tip. If TreeMan cannot establish fresh default-branch state, merged state is blank. If forge verification is unavailable, only forge-confirmed results are blank. Normal Git ancestry results still show `YES`. TreeMan shows a warning when it detects a forge but cannot query it. A stale worktree has a missing or non-directory path and must be pruned with `git worktree prune`. `--json` writes objects for scripts and agents. Each object contains `path`, `branch`, `main`, `current`, `dirty`, `detached`, `merged`, and `stale` fields.

`wtl` is a shell shortcut for `treeman list`.

## `benchmark`

```text
treeman benchmark [list | branch <remote-branch> | review <pr-number> | delete | branch-results | review-results] [--runs <count>] [--warmup <count>] [--skip-env] [--skip-database] [--skip-deps] [--skip-hooks]
```

Measure execution time for a supported command. The default command is `list`. `branch` requires an exact remote branch name and measures the exact remote-branch lookup, fetch, and worktree creation. `review` requires an explicit GitHub PR or GitLab MR number and also measures forge resolution. Both skip project setup: environment files, dependencies, databases, and hooks. After every warmup and timed iteration, TreeMan force-removes the worktree and exact local branch it created.

`delete` takes no branch argument. Before every warmup and measured iteration, TreeMan creates the fixed disposable branch `treeman-benchmark-delete` in a temporary clone and performs normal project setup, including environment copying, branch database creation, dependency installation, and hooks. That clone is made from the repository's own main worktree rather than from `origin`, so the target also runs in a repository with no remote, or none reachable, and needs no network round trip. Preparation is not timed. Timing starts immediately before direct, confirmed deletion and stops when deletion returns; worktree and branch verification is also outside the timer. Setup failures abort the benchmark instead of measuring an incomplete worktree, and name the flag that skips the failed step. Normal deletion safety checks remain enabled and database cleanup is included.

The `--skip-*` flags turn individual setup actions off for `delete`, the same actions `treeman create` skips with the same flag names. Use them where a step cannot run on this machine, such as a dependency install that needs credentials or a database whose container is not running. Every other target runs no project setup and rejects the flags. `delete` reports the composition of each prepared worktree as `prepared: environment ..., dependencies ..., database ..., hooks ...`, and repeats it whenever it changes, because what a deletion costs is decided by what setup put in the worktree. The same line reports untracked setup output the project does not ignore: clearing it is what keeps the timed deletion the same non-forced deletion a user runs, and it leaves that deletion less to remove than the project itself would.

`branch-results` and `review-results` measure the complete data payload used by interactive `wtb` and `wtmr`, respectively. The payload includes streamed forge batches or records, filtering and association where applicable, and picker-row rendering. They stop before launching `fzf`, make no repository changes, report the number of available results, and flag changes during timed runs. Both also report producer row-ready latency: the time from forge detection until the producer hands its first rendered row to the picker writer. This is not `fzf` paint latency. Warmup runs do not affect the results. TreeMan suppresses command output and reports mean, standard deviation, minimum, and maximum times.

## `clean`

```text
treeman clean [--dry-run] [--yes]
```

`clean` fetches the detected default branch, then removes linked worktrees only when all of these conditions are true: the worktree is not main, it has no changes, and its branch is merged into the fetched `origin/<default-branch>`. Branches merged via squash or rebase, whose remote branch was deleted after merge, also qualify when `gh` or `glab` reports a merged PR/MR targeting that default branch whose source branch and head commit equal the local branch and tip. This includes GitHub fork PRs. Without that confirmation, TreeMan retains the branch. It does not remove detached worktrees or the default branch. A locked worktree is never offered: Git refuses to remove one and `--force` does not waive it, so `clean` reports the omission with the lock reason instead of promising a removal it would have to refuse. If a candidate is refused during execution while nothing was removed for it, `clean` reports that candidate, continues with the rest, and exits unsuccessfully. A removal that already unregistered a worktree still stops the run, because the state it left behind needs attention before more deletions run. TreeMan removes worktrees and branches before batching drops of their recorded owned databases, and retries pending drops from earlier deletion runs. When any candidate has recorded database ownership, the preview abbreviates paths to their final two components, mentions databases, and adds a `DATABASE` column with `✓` for owned databases. Cleanup results include a `DATABASE` column only when at least one removed worktree has a recorded database. If it removes the current worktree, it prints the main worktree path so shell integration returns there safely. Use `--dry-run` to print candidates without deleting them. Use `--yes` or `-y` to skip confirmation.

## `shell`

```text
treeman shell install [--shell <shell>] [--config <file>] [--path <directory>]
treeman shell uninstall [--shell <shell>] [--config <file>] [--all]
treeman shell status [--shell <shell>] [--config <file>]
treeman shell init <bash|zsh|fish>
```

Run `treeman shell install` once after TreeMan is on `PATH`. It detects the current Bash, Zsh, or Fish configuration file and writes a marked, idempotent integration block. Use `--shell` or `--config` to choose a different target. `--path` also adds a binary directory to the managed block and is used by the release installer.

The integration defines a `treeman` adapter and the `wt`, `wtb`, `wtpr`, `wtmr`, `wts`, `wtl`, `wtc`, and `wtd` shortcuts. Both command styles change the shell directory after commands that return a worktree path. Other native commands pass through unchanged.

`treeman shell init` prints the sourceable integration without modifying files. `treeman init <shell>` remains available for existing startup files.

## `doctor`

```text
treeman doctor
```

Check Git repository, forge CLI, optional configuration and database setup, `fzf`, Docker, and shell integration readiness. The diagnostic report, including hints and its summary, is written to stderr; `doctor` writes no stdout output.

`doctor` exits non-zero after rendering the full report when any diagnostic fails. Warnings and informational diagnostics do not cause a non-zero exit.

Docker readiness uses a read-only daemon connectivity check. It does not inspect containers or connect to PostgreSQL. A missing Docker executable or unavailable daemon is a warning.

Shell integration is checked only when `SHELL` identifies active `bash` or `zsh`. TreeMan recognizes its managed block and legacy `treeman init` entries. Missing or unsupported `SHELL` values are informational and are not treated as bash.

## `version`

```text
treeman version
```

This command prints version, commit, and build date when build data exists.

## Terminal Behavior

TreeMan detects terminal capabilities separately for each input and output stream. Status messages and warnings use stderr. Commands that create, select, or delete a worktree report a destination path for shell integration and scripts.

### How a Destination Reaches Your Shell

A shell cannot change its own parent's directory, so TreeMan reports where to go and the shell wrapper performs the `cd`.

When `TREEMAN_CD_FILE` names a file, TreeMan writes the destination there. Shell integration sets that variable, which is why it never runs TreeMan inside command substitution: a captured TreeMan could not hand its terminal to `--exec`, and the wrapper would have to parse TreeMan's own flags to know when to skip the capture.

The variable belongs to the one TreeMan the wrapper started. TreeMan removes it from its own environment at startup, so nothing it runs afterwards inherits it -- a hook, a dependency install, and a command given to `--exec` all see it unset. A TreeMan started from inside any of those is therefore a bare run: it prints its path to stdout and cannot steer the shell that is waiting outside.

Without the variable -- a bare run, a script, a pipe -- the destination goes to stdout, which is the original contract.

Color and rich terminal UI are enabled only when the relevant output stream is a terminal. Redirected output is plain. Set `NO_COLOR` to disable color. Set `TERM=dumb` to disable color and interactive selection.

Interactive selection and confirmation require both stdin and stderr to be terminals. TreeMan disables them when `CI` is set. When a picker is unavailable, pass an exact branch, worktree path, PR or MR number, or direct deletion flags instead. Redirecting stdout does not prevent shell integration from receiving a selected path.

## Picker Rules

Interactive `branch`, `review`, `switch`, and `delete` commands use `fzf`. Cancellation is a successful no-op for `switch` and `delete`. Cancellation is an error for `branch` and `review`.
