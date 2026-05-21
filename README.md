<img width="2758" height="1504" alt="TreeMan_Logo_no_white_bg_smooth2" src="https://github.com/user-attachments/assets/d12d7c55-cd61-4116-932d-e0f5f63ae613" />

Git worktree management CLI -- create new branch worktrees, check out remote branches, spin up review worktrees from PRs or MRs, and switch between them with an interactive picker.

Single compiled binary. No runtime required. Works with bash and zsh.

---

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/install.sh | bash
```

Downloads the `treeman` binary to `~/.treeman/bin/`, adds it to your `PATH`, and wires the shell functions via `eval "$(treeman init <shell>)"`. Optionally injects lazygit keybindings.

Reload your shell when done:

```bash
source ~/.zshrc   # or ~/.bashrc
```

<details>
<summary>Manual install</summary>

Download the binary for your platform from [GitHub Releases](https://github.com/shoutcape/TreeMan/releases/latest), place it somewhere on your `PATH`, then add to your shell config:

```bash
# ~/.zshrc or ~/.bashrc
eval "$(treeman init zsh)"   # or bash
```

</details>

<details>
<summary>Build from source</summary>

```bash
go install github.com/shoutcape/treeman/cmd/treeman@latest
```

Then add to your shell config:

```bash
eval "$(treeman init zsh)"   # or bash
```

</details>

---

## Uninstall

```bash
curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/uninstall.sh | bash
```

Or manually:
- Remove the `# TreeMan` block from your shell config (the `export PATH` line and the `eval "$(treeman init ...)"` line)
- Delete `~/.treeman/`

---

## Commands

| Command | Description |
|---------|-------------|
| `wt <branch-name>` | Create a new worktree + branch from latest main |
| `wtb [query]` | Check out a remote branch into a new worktree |
| `wtpr [pr-number]` | Create a review worktree from a GitHub PR |
| `wtmr [mr-number]` | Create a review worktree from a GitLab MR |
| `wts [query]` | Switch between worktrees (fzf picker) |
| `wtd [query]` | Delete a worktree and its branch (fzf picker) |
| `wto [query]` | Open a worktree in configured terminal |
| `lg` | Run lazygit with auto-cd on worktree switch |

### `wtb` -- Check out a remote branch

```bash
wtb                          # interactive fzf picker
wtb feature/some-branch      # exact match, skips picker
```

Lists remote branches from the forge API (GitHub/GitLab), excluding the default branch and branches that already exist locally. The picker shows:

- Branch name
- Last commit date (relative)
- Open MR/PR number (if one exists for that branch)

After selection, TreeMan fetches the branch, creates a worktree, sets upstream tracking, copies `.env` files, and installs dependencies -- the same post-create workflow as `wt` and `wtpr`/`wtmr`.

---

### Requirements

**Core**
    - `git`
    - `treeman` binary (installed via the one-liner or manually -- see [Install](#install))
- `bash` or `zsh` (for shell wrapper functions)

    **For `wtb`, `wtpr` / `wtmr`**
    - [`gh`](https://cli.github.com/) -- GitHub repos
    - [`glab`](https://gitlab.com/gitlab-org/cli) + [`jq`](https://jqlang.github.io/jq/) -- GitLab repos (including self-hosted)

    **For `wts`, `wtd`, `wtb` (picker), and the interactive PR/MR picker**
    - [`fzf`](https://github.com/junegunn/fzf)

    **For dependency auto-install**
- The package manager your project uses (only invoked when a matching lockfile is detected)

    **For per-branch databases**
    - `docker` -- used to find and exec into the running PostgreSQL container

---

## Per-Branch Database Management

TreeMan can automatically create a dedicated PostgreSQL database for each worktree and drop it on deletion. This keeps branch data fully isolated without manual setup.

### How it works

1. **On `wt create`** -- TreeMan reads the database URI from your `.env` file, derives a branch-specific database name (`<db>__<branch_slug>`), runs `CREATE DATABASE` inside the running Postgres container, and rewrites the `.env` in the new worktree to point at the new database.
2. **On `wtd` (delete)** -- TreeMan reads the `.env` from the worktree being deleted, and runs `DROP DATABASE IF EXISTS` to clean up. Only databases containing `__` (the branch separator) are eligible for auto-drop, so your main database is never touched.

### Setup

Create a `.treeman.toml` in your project root:

```toml
[database]
env_key = "DATABASE_URI"    # the .env variable that holds your postgres connection string
```

That's it. The `.env` file must contain a `postgres://` or `postgresql://` URI under that key:

```
DATABASE_URI=postgres://postgres:postgres@127.0.0.1:5432/myapp
```

### Database naming

Branch names are converted to safe PostgreSQL identifiers:

| Branch | Database |
|---|---|
| `jd/fix-123/add-user-auth` | `myapp__jd_fix_123_add_user_auth` |
| `hotfix` | `myapp__hotfix` |
| `feat/v2.0-support` | `myapp__feat_v2_0_support` |

Names are truncated to 63 characters (PostgreSQL's max identifier length).

### Container discovery

TreeMan finds the running PostgreSQL container automatically via `docker ps`. It tries in order:

1. A container publishing the port from the URI
2. A container with the `postgres` ancestor image
3. Any container with "postgres" in the image name

### Notes

- The feature is **opt-in** -- without `.treeman.toml`, nothing changes.
- Database operations are **best-effort** -- failures produce warnings, never block worktree creation or deletion.
- Only `postgres://` and `postgresql://` URIs are handled; other databases are silently skipped.
- Quoted values in `.env` (single or double quotes) are supported.

---

## Terminal Integration (Ghostty)

TreeMan can automatically control your terminal emulator when managing worktrees -- opening tabs on create, focusing them on switch, and closing them on delete.

Currently supported: **Ghostty** on macOS (requires Ghostty 1.3+ for AppleScript support).

### Setup

Add to your global config at `~/.config/treeman/config.toml`:

```toml
[terminal]
app = "ghostty"
```

That's it. Now `wt`, `wts`, and `wtd` will manage Ghostty tabs alongside worktrees.

### Layout splits

You can configure automatic pane splits per project. Add a `[terminal.layout]` section to your project's `.treeman.toml`:

```toml
[terminal.layout]
splits = [
    { direction = "right", command = "pnpm dev" },
    { direction = "down", command = "pnpm test --watch" },
]
```

When creating a worktree, TreeMan opens a new Ghostty tab and splits it according to the layout, running the specified commands in each pane. All panes start in the worktree directory.

You can also set a default layout in the global config (`~/.config/treeman/config.toml`). Project config overrides global when present.

### How it works

- **`wt create`** -- Opens a new Ghostty tab (or window if none exists) at the worktree directory. Names the terminal `treeman:<branch-slug>` for tracking.
- **`wts` (switch)** -- Finds and focuses the Ghostty terminal for the selected worktree. Still prints the path for the shell `cd`.
- **`wtd` (delete)** -- Closes all Ghostty terminals associated with the worktree before removing it.

### Notes

- The feature is **opt-in** -- without `terminal.app` in config, nothing changes.
- Terminal operations are **best-effort** -- failures produce warnings, never block worktree operations.
- If Ghostty is not running, `activate` will launch it.
- Split directions: `right`, `left`, `down`, `up`.
