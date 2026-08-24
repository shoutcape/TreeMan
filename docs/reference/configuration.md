# Configuration Reference

TreeMan reads optional TOML configuration. Invalid project configuration produces a warning. It does not stop worktree creation.

## Project Configuration

Put `.treeman.toml` in the main project directory. TreeMan searches from the main worktree upward to the file system root. It uses the first file that it finds.

```toml
update_gitignore = true

[database]
env_key = "DATABASE_URI"
container = "project-postgres-1"

[hooks]
post_create = ["pnpm generate", "git status --short"]
```

## `update_gitignore`

```toml
update_gitignore = true
```

When `true`, TreeMan appends `.worktrees/` to the root `.gitignore` the first time a worktree is created (idempotent - no duplicate entries). Defaults to `false`. Set this to `true` if you want TreeMan to manage the `.gitignore` entry automatically.

## `[database]`

```toml
[database]
env_key = "DATABASE_URI"
container = "project-postgres-1"
```

`env_key` is required when `[database]` exists. It identifies a PostgreSQL URI in the copied `.env` file.

`container` is optional. It names the running Docker container that hosts PostgreSQL. Without it, TreeMan requires exactly one PostgreSQL container publishing the URI port on a local host. Set it for remote URI hosts, unexposed Docker ports, or machines with multiple matching containers.

Read [Branch Databases](../integrations/postgresql.md).

## `[hooks]`

```toml
[hooks]
post_create = ["pnpm generate", "git status --short"]
```

TreeMan runs each command in sequence in the new worktree. It uses `sh -c` on Unix and `cmd /C` on Windows.

TreeMan attempts later hooks after a hook failure. Hook failures create warnings. Hooks can run arbitrary commands. Use only trusted project configuration.

## Themes

Use the following commands to select a theme.

1. Use `treeman theme` or `treeman theme set` without a name to open the interactive theme picker.
2. Use `treeman theme set <name>` to select a theme directly.

Interactive theme selection requires `fzf`, stdin, and stderr terminals. Use `treeman theme list` to see themes without a terminal, then use `treeman theme set <name>`.

Available themes are `forest` (the default), `catppuccin-mocha`, `dracula`, `gruvbox`, `nord`, `one-dark`, `solarized-dark`, `solarized-light`, and `tokyo-night`. `catppuccin` is an alias for `catppuccin-mocha`.

TreeMan saves the selected theme in `$XDG_STATE_HOME/treeman/theme`. If `XDG_STATE_HOME` is not set, it uses `$HOME/.local/state/treeman/theme`.

Set `TREEMAN_THEME` to temporarily override the saved theme without modifying it.
