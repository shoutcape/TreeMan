# Workflows

## Create a Feature Worktree

1. Go to any worktree in the repository.
2. Run `wt feature/name`.
3. Work in `.worktrees/feature-name`, or in the parent selected by `worktree_dir`.
4. Run `wts` to return to another worktree.

TreeMan fetches the default branch before it creates the branch. It reads `refs/remotes/origin/HEAD` to find that branch. If that ref is absent, it looks for `main` or `master` on `origin`.

## Work on a Remote Branch

1. Authenticate `gh` or `glab` for your forge.
2. Run `wtb`.
3. Select a branch.
4. Work in the new worktree.

Run `wtb branch-name` for an exact remote branch name. This path does not need `fzf`.

## Review a Change

1. Run `wtpr 42` or `wtmr 42`.
2. Review the change in the new worktree.
3. Run `wtd` when the review ends.


## Use a Script

Native commands report a selected or new path for the caller's shell. They send status to stderr.

```bash
path=$(treeman create feature/login)
cd "$path"
```

Successful `create` and `branch` commands report their worktree path.

## Start a Command in the Worktree

Use `-x <command>` when you want to work in the new worktree immediately.

```bash
wt feature/login -x claude
```

TreeMan replaces itself with the command after setup. The command owns the terminal and prints no worktree path. Read the [Command Reference](../reference/cli.md#run-a-command-in-the-worktree).

## Delete From a Tool

Use direct deletion only when the tool provides a trusted worktree path and branch.

```bash
treeman delete --path /path/to/repo/.worktrees/feature-login --branch feature/login --yes
```

The command starts a detached process. Do not remove related files until the process finishes.
