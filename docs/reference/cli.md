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
| `treeman clean [--dry-run]` | `wtc` | Remove clean worktrees merged into the default branch |
| `treeman delete [query]` | `wtd` | Delete a linked worktree and branch |
| `treeman init <shell>` | None | Print shell wrappers |
| `treeman doctor` | None | Check repository readiness and configuration |
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

`branch` has alias `wtb`. It gets remote branches from the detected forge. An exact query selects a branch without `fzf`. Other queries use `fzf`.

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

`switch` has alias `wts`. It requires `fzf`.

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

List the repository worktrees with branch, path, main, current, dirty, and merged state. A `YES` value in the `MERGED` column means the local branch is merged into the default branch on `origin`. If `origin` is unavailable, merged state is left blank. `--json` writes an array of objects with `path`, `branch`, `main`, `current`, `dirty`, `detached`, and `merged` fields for scripts and agents.

`wtl` is a shell shortcut for `treeman list`.

## `clean`

```text
treeman clean [--dry-run]
```

`clean` fetches the detected default branch, then removes linked worktrees only when all of these conditions are true: the worktree is not main or current, it has no changes, and its branch is merged into `origin/main` or `origin/master`. It does not remove detached worktrees or the default branch. Use `--dry-run` to print candidate paths without deleting them.

## `init`

```text
treeman init bash
treeman init zsh
```

This command prints `wt`, `wtb`, `wtpr`, `wtmr`, `wts`, `wtl`, `wtc`, `wtd`, and `lg` shell functions. Add its output through `eval` in your shell startup file.

`lg` starts lazygit. It changes the directory when lazygit writes a new-directory file.

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

## Picker Rules

Interactive `branch`, `review`, `switch`, and `delete` commands use `fzf`. Cancellation is a successful no-op for `switch` and `delete`. Cancellation is an error for `branch` and `review`.
