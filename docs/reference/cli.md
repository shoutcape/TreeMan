# Command Reference

Run `treeman --help` for current command help. TreeMan sends status and warnings to stderr. Commands that select or create worktrees can send a path to stdout.

Shell integration uses stdout destinations to change the current interactive shell directory. Native commands never change the caller directory.

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

## `create`

```text
treeman create <branch-name> [--skip-env] [--skip-database] [--skip-deps] [--skip-hooks]
```

Create a local branch from the fetched default branch. TreeMan supports only `main` and `master` as default branch names.

The command fails when the branch or target directory exists. It creates `.worktrees/<branch-slug>`. The branch slug changes `/` to `-`.

TreeMan then updates `.gitignore`, copies `.env*` files, loads configuration, sets up a database, installs dependencies, and runs hooks. These later actions are warning-only.

Use any `--skip-*` flag to omit its named optional setup action. TreeMan lists requested skips in the final summary.

## `preflight`

```text
treeman preflight
```

Report whether environment file copy, dependency installation, database setup, and post-create hooks can run for the current repository. The report reads repository files, checks required dependency installers, and checks the configured PostgreSQL container without creating a worktree, branch, or database, or running hooks. Nested modules are reported but remain skipped; use trusted post-create hooks for their setup.

## `branch`

```text
treeman branch [query] [--skip-env] [--skip-database] [--skip-deps] [--skip-hooks]
```

`branch` has alias `wtb`. With an exact branch name, it fetches the branch directly without `fzf` or a forge CLI. Otherwise, it gets all paginated remote branches from the detected forge and uses `fzf`.

TreeMan excludes the default branch and local branches. It does not exclude protected branches.

After selection, TreeMan fetches the branch, creates a local branch, and tries to set upstream tracking. It then runs the create post-actions.

## `review`

```text
treeman review [pr-number] [--skip-env] [--skip-database] [--skip-deps] [--skip-hooks]
```

`review` has aliases `wtpr` and `wtmr`. TreeMan detects GitHub or GitLab from `origin`.

Give a positive numeric PR or MR number. Without a number, TreeMan uses `fzf` to select an open PR or MR.

TreeMan fetches the review head into a new worktree. It runs environment, database, dependency, and hook actions.

## `switch`

```text
treeman switch [query]
```

`switch` has alias `wts`. An exact branch name or worktree path selects the matching worktree without `fzf`. Other input requires `fzf`.

It prints the selected path to stdout. It returns success without output when you cancel selection or select the current directory. Shell integration changes directory when it receives a path.

## `delete`

```text
treeman delete [query]
treeman delete --path <path> --branch <branch> [--yes] [--force]
```

`delete` has alias `wtd`. Interactive deletion requires `fzf`. It excludes the main worktree.

`--path` and `--branch` use direct mode. Direct mode requires both flags. `--yes` and `-y` skip confirmation.

TreeMan verifies that `--path` names a linked worktree and that it has the supplied branch. It protects the main worktree and detected default branch. Deletion loads the worktree's durable database ownership record, removes the worktree and branch, marks the owned database pending, then drops it before the command returns. It never derives deletion authority from `.env`.

TreeMan refuses deletion when the worktree has staged, modified, or untracked files. Use `--force` only when you intend to remove those files. `--yes` skips the confirmation prompt but does not bypass safety checks.

When the deleted worktree is the current directory, TreeMan prints the main worktree path to stdout so shell integration can change directory safely.

## `list`

```text
treeman list [--json]
```

List repository worktrees with branch, path, main, current, dirty, stale, and merged state. TreeMan checks the current default-branch tip on the remote. It fetches the branch only when local tracking data is missing or different. `YES` in `MERGED` means the tip is an ancestor of the refreshed default branch. It can also mean the exact tip was the head of a merged PR or MR. For GitHub, it can mean the tip descends from a merged PR head. This last case is informational. `clean` requires exact forge evidence before it removes a deleted branch after squash or rebase merge. If TreeMan cannot establish fresh default-branch state, merged state is blank. If forge verification is unavailable, only forge-confirmed results are blank. Normal Git ancestry results still show `YES`. TreeMan shows a warning when it detects a forge but cannot query it. A stale worktree has a missing or non-directory path and must be pruned with `git worktree prune`. `--json` writes objects for scripts and agents. Each object contains `path`, `branch`, `main`, `current`, `dirty`, `detached`, `merged`, and `stale` fields.

`wtl` is a shell shortcut for `treeman list`.

## `benchmark`

```text
treeman benchmark [list | branch <remote-branch> | review <pr-number> | branch-results | review-results] [--runs <count>] [--warmup <count>]
```

Measure execution time for a supported command. The default command is `list`. `branch` requires an exact remote branch name and measures the exact remote-branch lookup, fetch, and worktree creation. `review` requires an explicit GitHub PR or GitLab MR number and also measures forge resolution. Both skip project setup: environment files, dependencies, databases, and hooks. After every warmup and timed iteration, TreeMan force-removes the worktree and exact local branch it created. `branch-results` and `review-results` measure the complete data payload used by interactive `wtb` and `wtmr`, respectively. The payload includes paginated forge API retrieval, filtering and association where applicable, and picker-row rendering. They stop before launching `fzf`, make no repository changes, report the number of available results, and flag changes during timed runs. Warmup runs do not affect the results. TreeMan suppresses command output and reports mean, standard deviation, minimum, and maximum times.

## `clean`

```text
treeman clean [--dry-run] [--yes]
```

`clean` fetches the detected default branch, then removes linked worktrees only when all of these conditions are true: the worktree is not main, it has no changes, and its branch is merged into the fetched `origin/<default-branch>`. Branches merged via squash or rebase, whose remote branch was deleted after merge, also qualify when `gh` or `glab` reports a merged PR/MR targeting that default branch whose source branch and head commit equal the local branch and tip. This includes GitHub fork PRs. Without that confirmation, TreeMan retains the branch. It does not remove detached worktrees or the default branch. TreeMan removes worktrees and branches before batching drops of their recorded owned databases, and retries pending drops from earlier deletion runs. When any candidate has recorded database ownership, the preview abbreviates paths to their final two components, mentions databases, and adds a `DATABASE` column with `✓` for owned databases. Cleanup results include a `DATABASE` column only when at least one removed worktree has a recorded database. If it removes the current worktree, it prints the main worktree path so shell integration returns there safely. Use `--dry-run` to print candidates without deleting them. Use `--yes` or `-y` to skip confirmation.

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

TreeMan detects terminal capabilities separately for each input and output stream. Status messages and warnings use stderr. Commands that create, select, or delete a worktree can write a path to stdout for shell integration and scripts.

Color and rich terminal UI are enabled only when the relevant output stream is a terminal. Redirected output is plain. Set `NO_COLOR` to disable color. Set `TERM=dumb` to disable color and interactive selection.

Interactive selection and confirmation require both stdin and stderr to be terminals. TreeMan disables them when `CI` is set. When a picker is unavailable, pass an exact branch, worktree path, PR or MR number, or direct deletion flags instead. Redirecting stdout does not prevent shell integration from receiving a selected path.

## Picker Rules

Interactive `branch`, `review`, `switch`, and `delete` commands use `fzf`. Cancellation is a successful no-op for `switch` and `delete`. Cancellation is an error for `branch` and `review`.
