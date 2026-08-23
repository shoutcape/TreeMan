# Start With TreeMan

## Prepare Your Shell

Install TreeMan, then enable shell integration if the installer did not do so.

```bash
treeman shell install
```

Start a new shell. Run these commands from a Git repository with `origin` configured.

## Create a Worktree

Create a branch and worktree.

```bash
wt feature/login
```

TreeMan fetches the default branch. It makes `feature/login`. It puts the worktree at `.worktrees/feature-login` in the main worktree.

Both `wt` and `treeman create` change your current directory when TreeMan prints a path.

## Use an Existing Remote Branch

Select a remote branch.

```bash
wtb
```

Use an exact branch name to skip the picker.

```bash
wtb feature/login
```

TreeMan gets branches from GitHub or GitLab. It excludes the default branch and existing local branches.

## Make a Review Worktree

Create a worktree for a pull request or merge request.

```bash
wtpr 42
```

Use `wtmr 42` for the same command. TreeMan detects GitHub or GitLab from `origin`.

## Switch Worktrees

Use the worktree picker.

```bash
wts
```

Shell integration changes the current shell directory.

## Delete a Worktree

Select a linked worktree.

```bash
wtd
```

TreeMan asks for confirmation and completes deletion before it returns. When you delete the current worktree, the `wtd` wrapper changes back to the main worktree.

> [!warning]
> TreeMan refuses to delete a dirty worktree or unmerged branch. Use `--force` only when you intend to remove changed or untracked files.

Read [Workflows](guides/workflows.md) before you use direct deletion in a script.
