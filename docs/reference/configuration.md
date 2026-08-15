# Configuration Reference

TreeMan reads optional TOML configuration. Invalid project configuration produces a warning. It does not stop worktree creation.

## Project Configuration

Put `.treeman.toml` in the main project directory. TreeMan searches from the main worktree upward to the file system root. It uses the first file that it finds.

```toml
[database]
env_key = "DATABASE_URI"

[hooks]
post_create = ["pnpm generate", "git status --short"]

```

## `[database]`

```toml
[database]
env_key = "DATABASE_URI"
```

`env_key` is required when `[database]` exists. It identifies a PostgreSQL URI in the copied `.env` file.

Read [Branch Databases](../integrations/postgresql.md).

## `[hooks]`

```toml
[hooks]
post_create = ["pnpm generate", "git status --short"]
```

TreeMan runs each command in sequence in the new worktree. It uses `sh -c` on Unix and `cmd /C` on Windows.

TreeMan attempts later hooks after a hook failure. Hook failures create warnings. Hooks can run arbitrary commands. Use only trusted project configuration.
