# Troubleshooting

## TreeMan Cannot Find the Default Branch

TreeMan reads `refs/remotes/origin/HEAD` to find the default branch. If that ref is absent, TreeMan looks for `main` or `master` on `origin`. TreeMan stops with an error when it finds neither name.

Set `origin` to a reachable remote. Then write the missing ref.

```bash
git remote set-head origin --auto
```

If the default branch has a different name, this command records it. As an alternative, create the worktree with Git.

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
6. Exactly one PostgreSQL container publishes the local URI port, or `[database].container` names the intended running container.

Database setup is warning-only. Read [Branch Databases](../integrations/postgresql.md).

## Delete Finished With an Error

TreeMan returns Git deletion errors immediately. A failed owned database drop is retained as pending state and retried by `treeman clean`. Legacy databases without TreeMan ownership metadata are preserved and reported as warnings.

Verify Git worktree state before manual cleanup.

```bash
git worktree list
git branch --list
```

## Worktree Path Exists

TreeMan does not overwrite an existing target directory. Check the configured `worktree_dir` (default `.worktrees/`) for a path with the same branch slug.

A branch that collides with a different branch does not cause this error. TreeMan adds a slug suffix to the path of the second branch. This error shows a directory that is not a worktree, or a worktree for the same branch.

Do one of these actions:

- Remove the directory.
- Delete its worktree with `treeman delete`.
