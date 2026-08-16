# Command Reference

Run `treeman --help` for current command help. TreeMan sends status and warnings to stderr. Commands that select or create worktrees can send a path to stdout. Use `--color auto`, `--color always`, or `--color never` to control ANSI color. `auto` uses color only on a terminal. A non-empty `NO_COLOR` disables color for all modes.

Shell wrappers use stdout to change the current shell directory. Native commands never change the caller directory.

## Commands

| Native command | Shell wrapper | Purpose |
| --- | --- | --- |
| `treeman create <branch>` | `wt` | Create a branch and worktree |
| `treeman branch [query]` | `wtb` | Add a remote branch worktree |
| `treeman review [number]` | `wtpr`, `wtmr` | Add a PR or MR worktree |
| `treeman switch [query]` | `wts` | Select a worktree path |
| `treeman list [--json]` | `wtl` | List worktrees and their status |
| `treeman clean [--dry-run]` | `wtc` | Remove clean worktrees merged into the default branch |
| `treeman delete [query]` | `wtd` | Delete a linked worktree and branch |
| `treeman init <shell>` | None | Print shell wrappers |
| `treeman version` | None | Print build data |

## `create`

```text
treeman create <branch-name>
```

Create a local branch from the fetched default branch. TreeMan supports only `main` and `master` as default branch names.

The command fails when the branch or target directory exists. It creates `.worktrees/<branch-slug>`. The branch slug changes `/` to `-`.

TreeMan then updates `.gitignore`, copies `.env*` files, loads configuration, sets up a database, installs dependencies, and runs hooks. These later actions are warning-only.

## `branch`

```text
treeman branch [query]
```

`branch` has alias `wtb`. With an exact branch name, it fetches the branch directly without `fzf` or a forge CLI. Otherwise, it gets all paginated remote branches from the detected forge and uses `fzf`.

TreeMan excludes the default branch and local branches. It does not exclude protected branches.

After selection, TreeMan fetches the branch, creates a local branch, and tries to set upstream tracking. It then runs the create post-actions.

## `review`

```text
treeman review [pr-number]
```

`review` has aliases `wtpr` and `wtmr`. TreeMan detects GitHub or GitLab from `origin`.

Give a numeric PR or MR number. Without a number, TreeMan uses `fzf` to select an open PR or MR.

TreeMan fetches the review head into a new worktree. It runs environment, database, dependency, and hook actions.

`0` passes current numeric validation. Use a positive PR or MR number.

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

TreeMan verifies that `--path` names a linked worktree and that it has the supplied branch. It protects the main worktree and detected default branch. Deletion cleans a branch database, removes the worktree, and deletes the branch before the command returns.

TreeMan refuses deletion when the worktree has staged, modified, or untracked files. Use `--force` only when you intend to remove those files. `--yes` skips the confirmation prompt but does not bypass safety checks.

When the deleted worktree is the current directory, TreeMan prints the main worktree path to stdout so the `wtd` shell wrapper can change directory safely.

## `list`

```text
treeman list [--json]
```

List the repository worktrees with branch, path, main, current, dirty, and merged state. `M` marks the main worktree and `▶` marks the current worktree. `CLEAN` means no changes, `DIRTY` means changes, and `DETACHED` means no branch. TreeMan fetches the default branch before checking merge state. A `YES` value in the `MERGED` column means the local branch is merged into the default branch on `origin`. If `origin` is unavailable, merged state is left blank. `--json` writes an array of objects with `path`, `branch`, `main`, `current`, `dirty`, `detached`, and `merged` fields for scripts and agents.

`wtl` is a shell shortcut for `treeman list`.

## `clean`

```text
treeman clean [--dry-run]
```

`clean` fetches the detected default branch, then removes linked worktrees only when all of these conditions are true: the worktree is not main, it has no changes, and its branch is merged into `origin/main` or `origin/master`. It does not remove detached worktrees or the default branch. If it removes the current worktree, it prints the main worktree path so the `wtc` shell wrapper returns there safely. Use `--dry-run` to print candidate paths without deleting them.

## `init`

```text
treeman init bash
treeman init zsh
treeman init fish
```

This command prints `wt`, `wtb`, `wtpr`, `wtmr`, `wts`, `wtl`, `wtc`, `wtd`, and `lg` shell functions. For Bash or Zsh, add its output through `eval` in your shell startup file.

For Fish, add its output with `treeman init fish | source`.

`lg` starts lazygit. It changes the directory when lazygit writes a new-directory file.

## `version`

```text
treeman version
```

This command prints version, commit, and build date when build data exists.

## Picker Rules

Interactive `branch`, `review`, `switch`, and `delete` commands use `fzf`. Cancellation is a successful no-op for `switch` and `delete`. Cancellation is an error for `branch` and `review`.
