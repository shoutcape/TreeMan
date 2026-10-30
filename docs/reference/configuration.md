# Configuration Reference

TreeMan reads optional TOML configuration. Read-only commands report invalid project configuration as a warning. Commands that create worktrees stop before fetching or changing Git when configuration cannot be read, parsed, or validated.

## Project Configuration

Put `.treeman.toml` in the main project directory. TreeMan searches from the main worktree upward to the file system root. It uses the first file that it finds.

```toml
update_gitignore = true
worktree_dir = "~/worktrees/{repo}"

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

When `true`, TreeMan appends the resolved internal worktree directory to the root `.gitignore` the first time a worktree is created (idempotent - no duplicate entries). The default entry remains `.worktrees/`. External directories need no repository ignore entry, so TreeMan does not add one. Defaults to `false`.

## `worktree_dir`

```toml
worktree_dir = "~/worktrees/{repo}"
```

`worktree_dir` selects the parent directory for newly created worktrees. TreeMan appends the branch slug, and a collision suffix when needed. The default is `.worktrees` inside the main worktree.

Relative values resolve from the main worktree root, including when TreeMan is run from a linked worktree. Absolute paths and a leading `~/` are supported. `{repo}` expands to the main worktree directory name. No other placeholder is supported; `{branch}`, unknown placeholders, malformed braces, and forms such as `~other/path` are rejected.

TreeMan creates missing parent directories after validating the destination. The worktree parent must be a dedicated directory, not the main worktree root itself. TreeMan also refuses existing destinations, invalid parent paths, Git metadata, and aliases of protected paths reached through symlinks. External paths are allowed when filesystem permissions permit them. Changing this setting does not move existing worktrees; lifecycle commands continue to use the paths recorded by Git.

## `[database]`

```toml
[database]
env_key = "DATABASE_URI"
container = "project-postgres-1"
```

`env_key` is required when `[database]` exists. It identifies a PostgreSQL URI in the copied `.env` file.

`container` is optional. It names the running Docker container that hosts PostgreSQL. Without it, TreeMan requires exactly one PostgreSQL container publishing the URI port on a local host. Set it for remote URI hosts, unexposed Docker ports, or machines with multiple matching containers.

Read [Branch Databases](../integrations/postgresql.md).

## `[deps]`

```toml
[deps]
reuse = ["node_modules", "packages/api/node_modules"]
```

`reuse` is an optional list of explicit repository-relative directories. Before
dependency installation, TreeMan copies each existing directory from the main
worktree into the new worktree. The copy is independent and does not use
hardlinks, reflinks, or links to the main worktree. TreeMan never overwrites an
existing destination. A missing source is skipped. `--skip-deps` skips both
reuse and installation.

Paths must not be empty, absolute, duplicated, overlapping, or contain `..` or
`.git` (in any letter case). Configured path ancestors must not be symlinks.
Nested `.git` directories inside a selected directory, such as those in
Git-installed packages, are copied as ordinary content. Relative symlinks in a
selected directory are preserved only when their targets, including symlink
chains, remain inside that selected directory. Absolute or escaping links are
unsafe and cancel that reuse attempt.

Contained dangling links are allowed; cycles and unsupported entries such as
sockets and FIFOs are rejected. Copy failures produce warnings and trigger
cleanup of attempt-owned files; cleanup failures are reported too. The normal
installer still runs. `treeman setup` uses the same policy and preserves existing
destinations rather than refreshing them. Running `treeman setup` in the main
worktree skips reuse, because there is nothing to copy from.

Copies keep the read, write, and execute permissions of each file and
directory. Setuid, setgid, and sticky bits are not copied.

When `reuse` is configured, the setup summary includes a **Warm starts** line
with the number of copied, skipped, and failed paths. A failed copy shows as a
failure there, while the installer still runs and reports its own result.

This is a general-purpose copy and has no portability guarantee. Virtual
environments can contain absolute paths, and native or compiled artifacts can
depend on the operating system, compiler, or source paths. TreeMan does not
sandbox install scripts or change package-manager shared-cache behavior. The
list is applied once for the repository; paths are not inferred and are not
automatically inherited by nested modules. Nested modules are not installed
automatically.

Avoid changing the source dependencies while creating a worktree: recursive
copying is not an atomic filesystem snapshot. Installers that delete their
output directory discard the copy, and even a successful reuse can be slower
than a normal installation with an effective cache.

### One-Time Warm-Start Suggestion

After setup, `create`, `branch`, `review`, and `setup` may offer to configure
warm starts when main contains a regular `package.json` and a real root
`node_modules` directory. This is a candidate check, not a recursive scan or a
guarantee that those dependencies are portable or faster to reuse.

The suggestion includes a **What are warm starts?** documentation link (or a
plain URL when terminal hyperlinks are unavailable), names the configuration
file it will change, and asks:

```text
Configure warm starts for this repo? [y/N]
```

Answering `y` or `yes` adds `reuse = ["node_modules"]` to the `[deps]` table
in main's `.treeman.toml`. Without a `[deps]` table, it adds the dotted key
`deps.reuse = ["node_modules"]`. If the file is missing, TreeMan creates it
with a `[deps]` table and normal file permissions. Existing comments, formatting,
and unrelated settings are preserved. If the file cannot be safely extended,
TreeMan warns and leaves manual configuration to you. TreeMan rechecks the file
just before replacing it and refuses if it changed. An edit saved by another
program in that final instant can still be lost, because editors take no lock
TreeMan can share. It does not rerun the installation that just finished;
this setting applies to future setup runs.
In particular, inline or dotted-key definitions of an existing `deps` table
require manual editing; the automatic writer extends ordinary `[deps]` tables
without rewriting the rest of the file.

The offer is shown **once per repository on this machine**, across its linked
worktrees. Enter, `n`, end of input, and interruption do not cause another offer.
The offer is recorded before prompting, so concurrent commands cannot both ask.
Declining changes no project configuration, and you can configure reuse manually
later.

There is no prompt in CI or noninteractive sessions, with `--skip-deps`, or when
`reuse` is already declared (including `reuse = []`). These skipped cases do not
consume the offer. Automatic configuration is also suppressed for configuration
inherited from a parent directory, rather than editing settings shared by other
repositories. Virtual environments, compiled outputs, and nested dependency
directories are never automatically suggested.

## `[hooks]`

```toml
[hooks]
post_create = ["pnpm generate", "git status --short"]
```

TreeMan runs each command in sequence in the new worktree. It uses `sh -c` on Unix and `cmd /C` on Windows.

TreeMan requires approval before it fetches or creates a worktree with hooks.
Approval includes the absolute configuration path, the hook phase, and the
exact ordered command strings. It does not include the contents of scripts
called by those strings. Hooks can run arbitrary commands. Use only trusted
project configuration.

Read [Hook Approval](hooks.md) for consent, invocation flags, and approval
management. After approved execution, TreeMan attempts later hooks after a
hook failure. Hook failures create warnings.

## Themes

Use the following commands to select a theme.

1. Use `treeman theme` or `treeman theme set` without a name to open the interactive theme picker.
2. Use `treeman theme set <name>` to select a theme directly.

Interactive theme selection requires `fzf`, stdin, and stderr terminals. Use `treeman theme list` to see themes without a terminal, then use `treeman theme set <name>`.

Available themes are `forest` (the default), `catppuccin-mocha`, `dracula`, `gruvbox`, `nord`, `one-dark`, `solarized-dark`, `solarized-light`, and `tokyo-night`. `catppuccin` is an alias for `catppuccin-mocha`.

TreeMan saves the selected theme in `$XDG_STATE_HOME/treeman/theme`. If `XDG_STATE_HOME` is not set, it uses `$HOME/.local/state/treeman/theme`.

Set `TREEMAN_THEME` to temporarily override the saved theme without modifying it.
