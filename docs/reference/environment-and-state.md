# Environment Files and State

TreeMan has no database or daemon for its own state. It uses Git, files, Docker, and one error log.

## Worktree Paths

TreeMan creates worktrees under this path:

```text
<main-worktree>/.worktrees/<branch-slug>
```

The branch slug changes `/` to `-`. Therefore, different branch names can produce the same slug. For example, `feature/login` and `feature-login` both produce `feature-login`.

The first branch to get a worktree keeps the plain slug. If a different branch holds that path, TreeMan adds a slug suffix:

```text
<main-worktree>/.worktrees/<branch-slug>-<slug-suffix>
```

The slug suffix comes from the full branch name. Therefore, one branch always gets the same path.

Existing worktrees keep their paths. TreeMan reads the branch of a worktree from `git worktree list`, not from the directory name. Existing target directories that are not worktrees stop creation.

TreeMan tries to add `.worktrees/` to the main `.gitignore`. This action is warning-only.

## Environment Files

TreeMan copies each root-level, non-directory file with a name that starts with `.env`. Examples include `.env`, `.env.local`, and `.env.test`.

The copy overwrites a destination file. TreeMan preserves source permissions during the copy. A branch-database rewrite preserves the current file mode, writes atomically, and refuses to follow a symlink.

Copy failures create warnings and do not stop creation.

## Configuration Files

Project configuration is `.treeman.toml`.

Read [Configuration Reference](configuration.md).

## Delete Error Log

Background deletion writes failures to one path:

```text
$XDG_DATA_HOME/treeman/delete-errors.log
$HOME/.local/share/treeman/delete-errors.log
```

The next TreeMan command prints and removes this file. Concurrent deletion processes can write to the same file.

## Other State

| State | Owner |
| --- | --- |
| Branches and linked worktrees | Git |
| Worktree directories | Git and file system |
| Environment values | `.env*` files |
| Branch databases | Docker PostgreSQL container |
| Branch database ownership and pending cleanup | `<git-common-dir>/treeman/databases/` |
| Shell directory change | Bash, Zsh, or Fish wrapper |
| Selected theme | `$XDG_STATE_HOME/treeman/theme` or `$HOME/.local/state/treeman/theme` |
