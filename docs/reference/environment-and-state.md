# Environment Files and State

TreeMan has no database or daemon for its own state. It uses Git, files, Docker, and one error log.

## Worktree Paths

By default, TreeMan creates worktrees under this path:

```text
<main-worktree>/.worktrees/<branch-slug>
```

The branch slug changes `/` to `-`. Therefore, different branch names can produce the same slug. For example, `feature/login` and `feature-login` both produce `feature-login`.

Set `worktree_dir` in `.treeman.toml` to choose another parent directory, including an external path. The first branch to get a worktree keeps the plain slug. If a different branch holds that path, TreeMan adds a slug suffix:

```text
<main-worktree>/.worktrees/<branch-slug>-<slug-suffix>
```

The slug suffix comes from the full branch name. Therefore, one branch always gets the same path.

Existing worktrees keep their paths. TreeMan reads the branch of a worktree from `git worktree list`, not from the directory name. Existing target directories that are not worktrees stop creation.

When `update_gitignore` is enabled, TreeMan tries to add the resolved internal worktree directory to the main `.gitignore`. It does not add an entry for external directories. This action is warning-only.

## Environment Files

TreeMan copies each root-level, non-directory file with a name that starts with `.env`. Examples include `.env`, `.env.local`, and `.env.test`.

The copy policy depends on the command:

| Command | Destination file exists | Destination file is absent |
| --- | --- | --- |
| `treeman create`, `branch`, `review` | TreeMan replaces the file | TreeMan copies the file |
| `treeman setup` | TreeMan keeps the file | TreeMan copies the file |
| `treeman setup --refresh-env` | TreeMan replaces the file unless database ownership checks block it | TreeMan copies the file unless database ownership checks block it |

No copy removes a file that only the destination has.

TreeMan preserves source permissions for a new file. A refresh keeps the permissions of the destination file, writes atomically, and refuses to follow a symlink. TreeMan refuses a source or destination entry that is not a regular file. A branch-database rewrite also preserves the current file mode, writes atomically, and refuses to follow a symlink.

Copy failures create warnings. TreeMan reports each file as copied, preserved, skipped, or failed, and one failed or skipped file does not stop the other files.

### Refresh and Database Ownership

`--refresh-env` can replace the file that names a branch database. Before that replacement, TreeMan checks the ownership record against both the current file and the replacement file.

- When both checks succeed, TreeMan restores the database name that it owns after the copy.
- A different host, port, user, or configured container is an error.
- If ownership cannot be proven, TreeMan skips that file entirely and prints a warning. An existing file stays unchanged; an absent file is not created. The other environment files still refresh.
- A missing current `.env` cannot prove the owned database target, even when the replacement matches the record. Invalid or unreadable ownership state also blocks the file.

`--skip-database` skips database setup. It does not skip these checks.

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
| Setup serialization for one worktree | `<git-common-dir>/treeman/setup/` |
| Worktrees and branches made for Claude Code | `<git-common-dir>/treeman/agent-worktrees.json` |
| Shell directory change | Bash, Zsh, or Fish wrapper |
| Selected theme | `$XDG_STATE_HOME/treeman/theme` or `$HOME/.local/state/treeman/theme` |
| Warm-start suggestion already offered | `$XDG_STATE_HOME/treeman/warm-start-prompts/` or `$HOME/.local/state/treeman/warm-start-prompts/` |

## Warm-Start Suggestion State

Warm-start suggestions are remembered per user and machine, outside project
configuration. Each marker is an empty `0600` file named by the SHA-256 hash of
the canonical Git common-directory path. All linked worktrees share that
identity; separate clones receive separate offers. Moving a repository to a new
path can result in a new offer. The parent directory is created with mode `0700`.

The marker is claimed before the interactive prompt, including when the user
declines or the session ends. No marker is created for a noninteractive run,
`--skip-deps`, an already-declared reuse setting, or a repository without an
eligible candidate. If TreeMan cannot record the offer, it warns and does not
prompt. Deleting these markers resets the corresponding one-time suggestions;
it does not enable or disable configured reuse.

## Claude Code Worktree State

`treeman claude worktree-create` records each worktree that it makes in this file:

```text
<git-common-dir>/treeman/agent-worktrees.json
<git-common-dir>/treeman/agent-worktrees.lock
```

Each record has the Claude Code worktree name, the branch, the worktree path, and the Claude Code session ID. It also tells if TreeMan made the branch, with the start commit and the time.

`worktree-remove`, `delete`, and `clean` update the records:

- When a branch is deleted, TreeMan removes all records for that branch.
- When TreeMan removes a worktree and keeps the branch, the record stays without a worktree path. A new request for the same name adds a worktree for that branch again.
- `worktree-create` removes records for branches that do not exist.

TreeMan writes the file with mode `0600` and replaces it in one operation. A malformed file causes `worktree-create` and `worktree-remove` to fail.

## Hook Approval State

TreeMan stores hook approvals outside repositories:

```text
$XDG_STATE_HOME/treeman/hook-approvals.json
$XDG_STATE_HOME/treeman/hook-approvals.lock
```

When `XDG_STATE_HOME` is unset, TreeMan uses:

```text
$HOME/.local/state/treeman/hook-approvals.json
$HOME/.local/state/treeman/hook-approvals.lock
```

`XDG_STATE_HOME` must be an absolute path. TreeMan creates the state directory
with mode `0700` and state files with mode `0600`. Symlinked home or state
parents are resolved to their canonical location, which must be outside the
repository TreeMan is running in: neither its Git directory nor its working
tree may contain the state directory. The TreeMan state directory cannot itself
be a symlink; approval and lock files must be regular files, not symlinks,
directories, or FIFOs. Missing state means that no approval exists. Malformed
or unreadable state fails closed.

Approval reads and writes use a stable lock. Writes use a private temporary
file and an atomic replacement. TreeMan does not hold the lock while it asks
for consent or runs hooks. `--trust-hooks` and `--skip-hooks` do not access
this state.
