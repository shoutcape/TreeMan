---
name: treeman
description: Use when an agent needs an isolated Git worktree for feature work, a remote branch, or a pull request or merge request review. Use TreeMan commands instead of creating branches in the current worktree or calling git worktree directly.
---

# TreeMan

Use TreeMan to make and manage Git worktrees.

## Requirements

- Confirm `treeman` is available on `PATH` before use.
- Run commands from a Git repository with an `origin` remote.
- Use native `treeman` commands. Do not use shell shortcuts such as `wt` because an agent cannot change its parent shell directory.
- Do not use `git checkout -b`, `git switch -c`, or `git worktree` as substitutes.

## Create a Feature Worktree

Ask for a branch name when the user did not provide one. Then run:

```sh
treeman create <branch-name>
```

TreeMan prints the new worktree path to stdout. Capture that path. Use it as the working directory for all later commands. Do not use `cd` or assume a shell directory persists between tool calls.

TreeMan fetches the default branch, creates the branch and worktree, copies environment files, installs dependencies, and runs configured hooks. Report a TreeMan failure. Do not repeat its setup steps manually.

## Open an Existing Branch

For a known remote branch, run:

```sh
treeman branch <branch-name>
```

Capture the printed path and use it as the working directory. Do not use the interactive picker in a non-interactive agent session.

## Review a Pull Request or Merge Request

For a known review number, run:

```sh
treeman review <number>
```

Capture the printed path and use it as the working directory. TreeMan detects GitHub or GitLab from `origin`.

## Switch Worktrees

`treeman switch` prints a path but cannot change an agent's parent shell directory. Use the selected or known path through the tool working-directory option instead.

## List Worktrees

Use `treeman list --json` to discover existing worktrees. It reports stable path, branch, main, current, dirty, and detached fields without an interactive picker.

## Delete a Worktree

Do not delete a worktree unless the user explicitly asks. TreeMan refuses dirty worktrees, and branches whose commits are on no remote and not on the default branch, unless `--force` is specified.

Before direct deletion:

1. Use `treeman list --json` to identify the exact path and branch.
2. Confirm the target is not the main worktree or default branch.
3. Do not add `--force` unless the user explicitly accepts losing changed and untracked files, or commits that exist nowhere else.

After confirmation, run:

```sh
treeman delete --path <absolute-path> --branch <branch-name> --yes
```

Do not construct a path or branch from a guess. Do not call direct delete with untrusted values.

## Verify Setup

After TreeMan creates a worktree:

1. Confirm `git branch --show-current` matches the requested branch in the returned path.
2. Confirm `git status --short` succeeds in the returned path.
3. Run project setup or baseline tests only when the repository workflow requires them.
