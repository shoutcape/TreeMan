# Troubleshooting

## TreeMan Cannot Find the Default Branch

TreeMan detects only `main` and `master`. Set `origin` to a reachable remote. Rename the default branch to one supported name, or create the worktree with Git.

## TreeMan Cannot Select an Item

Install `fzf` for interactive branch, review, switch, and delete commands. An exact `wtb <branch>` command does not need `fzf`.

## Forge Commands Fail

Check the detected remote and forge login.

```bash
git remote get-url origin
gh auth status
glab auth status
```

## Configuration Has No Effect

Check the configuration path. TreeMan searches upward from the main worktree. It uses the first `.treeman.toml` file that it finds.

Run a TreeMan command and read stderr for parse warnings. A malformed project configuration does not stop worktree creation.

## Database Setup Fails

Check these items:

1. Docker is running.
2. A PostgreSQL container is running.
3. The configured environment key exists in copied `.env`.
4. The URI starts with `postgres://` or `postgresql://`.
5. The Docker container has `psql` and accepts the URI user.

Database setup is warning-only. Read [Branch Databases](../integrations/postgresql.md).

## Delete Finished With an Error

Run another TreeMan command. It reports errors from the previous background delete process.

Read one of these files if you need the error before another command runs:

```text
$XDG_DATA_HOME/treeman/delete-errors.log
$HOME/.local/share/treeman/delete-errors.log
```

Verify Git worktree state before manual cleanup.

```bash
git worktree list
git branch --list
```

## Worktree Path Exists

TreeMan does not overwrite an existing target directory. Check `.worktrees/` for a path with the same branch slug. Branches that differ only by slash replacement can collide.
