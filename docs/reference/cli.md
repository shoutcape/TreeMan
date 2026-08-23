# Command Reference

Run `treeman --help` for current command help. TreeMan sends status and warnings to stderr. Commands that select or create worktrees can send a path to stdout.

Shell wrappers use stdout to change the current shell directory. Native commands never change the caller directory.

## Commands

| Native command | Shell wrapper | Purpose |
| --- | --- | --- |
| `treeman create <branch>` | `wt` | Create a branch and worktree |
| `treeman branch [query]` | `wtb` | Add a remote branch worktree |
| `treeman review [number]` | `wtpr`, `wtmr` | Add a PR or MR worktree |
| `treeman switch [query]` | `wts` | Select a worktree path |
| `treeman list [--json]` | `wtl` | List worktrees and their status |
| `treeman clean [--dry-run] [--yes]` | `wtc` | Remove clean worktrees merged into the default branch |
| `treeman delete [query]` | `wtd` | Delete a linked worktree and branch |
| `treeman init <shell>` | None | Print shell wrappers |
| `treeman doctor` | None | Check repository readiness and configuration |
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

It prints the selected path to stdout. It returns success without output when you cancel selection or select the current directory. The `wts` wrapper changes directory when it receives a path.

## `delete`

```text
treeman delete [query]
treeman delete --path <path> --branch <branch> [--yes] [--force]
```

`delete` has alias `wtd`. Interactive deletion requires `fzf`. It excludes the main worktree.

`--path` and `--branch` use direct mode. Direct mode requires both flags. `--yes` and `-y` skip confirmation.

TreeMan verifies that `--path` names a linked worktree and that it has the supplied branch. It protects the main worktree and detected default branch. Deletion prepares branch database cleanup, removes the worktree and branch, then drops the prepared database before the command returns.

TreeMan refuses deletion when the worktree has staged, modified, or untracked files. Use `--force` only when you intend to remove those files. `--yes` skips the confirmation prompt but does not bypass safety checks.

When the deleted worktree is the current directory, TreeMan prints the main worktree path to stdout so the `wtd` shell wrapper can change directory safely.

## `list`

```text
treeman list [--json]
```

List the repository worktrees with branch, path, main, current, dirty, and merged state. TreeMan fetches the default branch before checking merge state. A `YES` value in the `MERGED` column means that the branch tip is an ancestor of the fetched default branch on `origin`, or that the remote branch is gone and GitHub or GitLab confirms a squash or rebase merge with the same source branch, target branch, and head commit. If TreeMan cannot detect or fetch the default branch, merged state is left blank. If forge verification is unavailable, only forge-confirmed results are left blank; normal Git ancestry results still show `YES`. TreeMan shows a warning when it detects a forge but cannot query it. `--json` writes an array of objects with `path`, `branch`, `main`, `current`, `dirty`, `detached`, and `merged` fields for scripts and agents.

`wtl` is a shell shortcut for `treeman list`.

## `clean`

```text
treeman clean [--dry-run] [--yes]
```

`clean` fetches the detected default branch, then removes linked worktrees only when all of these conditions are true: the worktree is not main, it has no changes, and its branch is merged into the fetched `origin/<default-branch>`. Branches merged via squash or rebase, whose remote branch was deleted after merge, also qualify when `gh` or `glab` reports a merged PR/MR targeting that default branch whose source branch and head commit equal the local branch and tip. This includes GitHub fork PRs. Without that confirmation, TreeMan retains the branch. It does not remove detached worktrees or the default branch. TreeMan removes the worktree and branch before it drops a prepared branch database. If it removes the current worktree, it prints the main worktree path so the `wtc` shell wrapper returns there safely. Use `--dry-run` to print candidate paths without deleting them. Use `--yes` or `-y` to skip confirmation.

## `init`

```text
treeman init bash
treeman init zsh
treeman init fish
```

This command prints `wt`, `wtb`, `wtpr`, `wtmr`, `wts`, `wtl`, `wtc`, and `wtd` shell functions. For Bash or Zsh, add its output through `eval` in your shell startup file.

For Fish, add its output with `treeman init fish | source`.

## `doctor`

```text
treeman doctor
```

Check Git repository, forge CLI, optional configuration and database setup, `fzf`, Docker, and shell integration readiness. The diagnostic report, including hints and its summary, is written to stderr; `doctor` writes no stdout output.

`doctor` exits non-zero after rendering the full report when any diagnostic fails. Warnings and informational diagnostics do not cause a non-zero exit.

Docker readiness uses a read-only daemon connectivity check. It does not inspect containers or connect to PostgreSQL. A missing Docker executable or unavailable daemon is a warning.

Shell integration is checked only when `SHELL` identifies active `bash` or `zsh`. TreeMan recognizes an enabled startup-file entry such as `eval "$(treeman init zsh)"`, including ordinary whitespace and quote variants. Missing or unsupported `SHELL` values are informational and are not treated as bash.

## `version`

```text
treeman version
```

This command prints version, commit, and build date when build data exists.

## Terminal Behavior

TreeMan detects terminal capabilities separately for each input and output stream. Status messages and warnings use stderr. Commands that create, select, or delete a worktree can write a path to stdout for shell wrappers and scripts.

Color and rich terminal UI are enabled only when the relevant output stream is a terminal. Redirected output is plain. Set `NO_COLOR` to disable color. Set `TERM=dumb` to disable color and interactive selection.

Interactive selection and confirmation require both stdin and stderr to be terminals. TreeMan disables them when `CI` is set. When a picker is unavailable, pass an exact branch, worktree path, PR or MR number, or direct deletion flags instead. Redirecting stdout does not prevent shell wrappers from receiving a selected path.

## Picker Rules

Interactive `branch`, `review`, `switch`, and `delete` commands use `fzf`. Cancellation is a successful no-op for `switch` and `delete`. Cancellation is an error for `branch` and `review`.
