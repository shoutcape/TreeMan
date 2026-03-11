<img width="2758" height="1504" alt="TreeMan_Logo_no_white_bg_smooth2" src="https://github.com/user-attachments/assets/d12d7c55-cd61-4116-932d-e0f5f63ae613" />




Shell functions for git worktree workflows — create new branch worktrees, jump straight into them, spin up review worktrees from PRs or MRs, and switch between them with an interactive picker.

No runtime required. Single shell script. Works with bash and zsh.

```bash
wt feature/my-thing        # create a new worktree + branch
wtpr 123                   # create a review worktree from PR #123
wtmr                       # pick an open PR/MR with fzf, then create a review worktree
wts                        # switch between worktrees (fzf picker)
wtd                        # delete a worktree (interactive picker)
```

### `wt` — create

<video src="https://github.com/user-attachments/assets/7491c0eb-896f-4046-b46b-1c3db01619c3" width="400"></video>

```
Fetching latest main from origin...
Creating worktree at ~/Github/my-project.feature-my-thing (branch: feature/my-thing)...
Copied .env, .env.local
Detected pnpm-lock.yaml — running pnpm install...

Worktree ready:
  Auto-switched to: /home/user/Github/my-project.feature-my-thing
  Path: /home/user/Github/my-project.feature-my-thing
```

### `wtpr` / `wtmr` — review

`wtpr` and `wtmr` are identical commands. TreeMan uses PR and MR interchangeably for this workflow.

```bash
$ wtpr 123
Fetching PR/MR #123 from origin...
Creating review worktree at ~/Github/my-project.feature-login-fix (branch: feature/login-fix)...
Detected pnpm-lock.yaml — running pnpm install...

Review worktree ready:
  PR/MR:  #123
  Title:  Fix login redirect loop
  Branch: feature/login-fix
  Path:   /home/user/Github/my-project.feature-login-fix
```

If you omit the PR number, TreeMan uses `fzf` to let you choose from open PRs/MRs:

- PR/MR number is highlighted first
- branch name is shown next for quick scanning
- title stays visible so related branches are easy to tell apart

```bash
wtpr
wtmr
```

### `wts` — switch


<video src="https://github.com/user-attachments/assets/ebb762c0-f530-4f35-aa3e-4b2c04c1f75b" width="300"></video>

```
┌──────────────── worktrees ────────────────────────────┐
│ switch >                                              │
│ Github/my-project  [main]                             │
│ Github/my-project.feature-my-thing [feature/my-thing] │
└───────────────────────────────────────────────────────┘
```

### `wtd` — delete

```
┌──────────────── worktrees ────────────────────────────┐
│ delete >                                              │
│ Github/my-project.feature-my-thing [feature/my-thing] │
└───────────────────────────────────────────────────────┘
```
```
Are you sure you want to delete this worktree and its branch? [y/N] y
```

---

## How it works

### `wt` — worktree creation

1. **Validates** you're inside a git repo with a branch name argument
2. **Detects the default branch** — checks for `origin/main`, falls back to `origin/master`
3. **Fetches** the latest from origin so the new branch is always up to date
4. **Creates** a new branch and worktree at `../<repo>.<branch-slug>` (sibling of your main repo)
5. **Copies `.env*` files** from the main worktree so the new worktree has the same environment
6. **Installs dependencies** by detecting the project's lockfile:
   | Lockfile | Command |
   |---|---|
   | `pnpm-lock.yaml` | `pnpm install` |
   | `yarn.lock` | `yarn install` |
   | `package-lock.json` | `npm install` |
   | `go.mod` | `go mod download` |
   | `requirements.txt` / `pyproject.toml` | notifies you to activate venv manually |
7. **Auto-`cd`s** into the new worktree and prints the path

Works correctly even when run from inside an existing worktree — always targets the main worktree root.

### `wtpr` / `wtmr` — review worktree creation

1. **Detects the forge** — GitHub (`github.com`) or GitLab (any host containing `gitlab`, including self-hosted instances like `gitlab.company.com`)
2. **Resolves PR/MR metadata** with `gh api` (GitHub) or `glab api` (GitLab), or lets you choose from open PRs/MRs with `fzf`
3. **Fetches** the PR/MR head via `pull/<number>/head` (GitHub) or `merge-requests/<number>/head` (GitLab)
4. **Creates** a review worktree using the PR/MR head branch name at `../<repo>.<branch-slug>`
5. **Copies `.env*` files** from the main worktree
6. **Installs dependencies** using the same lockfile detection as `wt`
7. **Auto-`cd`s** into the new review worktree and prints review details including the PR/MR number, title, branch, and final path

If the PR head branch already exists locally or already has a worktree, TreeMan fails safely instead of guessing.

### `wts` — worktree switching

1. **Lists** all worktrees via `git worktree list`
2. **Opens an fzf picker** showing shortened paths (last two path components) and branch names with color highlighting
3. **`cd`s** into the selected worktree

An optional query argument pre-filters the list (e.g. `wts feat`). If the query matches exactly one worktree, it's selected automatically.

### `wtd` — worktree deletion

1. **Lists** all worktrees via `git worktree list` (protects the main worktree from deletion)
2. **Opens an fzf picker** showing shortened paths and branch names
3. **Prompts** for confirmation before taking any destructive action
4. **Removes** the selected worktree using `git worktree remove`
5. **Deletes** the associated branch using `git branch -D`

If Git refuses to remove a dirty worktree, you can force it manually with `git worktree remove --force`.

---

## Worktree naming

Slashes in branch names become dashes in the directory name:

| Repo | Branch | Worktree directory |
|---|---|---|
| `~/Github/my-project` | `feature/cool-thing` | `~/Github/my-project.feature-cool-thing` |
| `~/Github/my-project` | `fix/bug-123` | `~/Github/my-project.fix-bug-123` |
| `~/Github/my-project` | `hotfix` | `~/Github/my-project.hotfix` |

Review worktrees created by `wtpr` / `wtmr` use the same branch-based naming.

---

## Supported forges & remote URLs

`wtpr` / `wtmr` auto-detect the forge from your `origin` remote URL:

| Forge | Detection | CLI tool | Fetch ref |
|---|---|---|---|
| **GitHub** | Host is `github.com` | `gh` | `pull/<n>/head` |
| **GitLab** | Host contains `gitlab` (e.g. `gitlab.com`, `gitlab.company.com`) | `glab` + `jq` | `merge-requests/<n>/head` |

All common remote URL formats are supported:

| Format | Example |
|---|---|
| SSH shorthand | `git@github.com:owner/repo.git` |
| SSH shorthand (GitLab, nested groups) | `git@gitlab.company.com:group/subgroup/project.git` |
| HTTPS | `https://github.com/owner/repo.git` |
| SSH URL | `ssh://git@github.com/owner/repo.git` |

GitLab nested groups (e.g. `group/subgroup/project`) are fully supported.

---

## Requirements

- `git`
- `bash` or `zsh`
- [`gh`](https://cli.github.com/) (for `wtpr` / `wtmr` with **GitHub** repos)
- [`glab`](https://gitlab.com/gitlab-org/cli) + [`jq`](https://jqlang.github.io/jq/) (for `wtpr` / `wtmr` with **GitLab** repos, including self-hosted instances)
- [`fzf`](https://github.com/junegunn/fzf) (for `wts`, `wtd`, and optional `wtpr` / `wtmr` picker mode)
- The package manager your project uses (only needed when a lockfile is detected)

---

## Install

### One-liner

Downloads `wt.sh` to `~/.treeman/`, adds a source line to your shell config, and optionally injects lazygit keybindings.

```bash
curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/install.sh | bash
```

Reload your shell when done:

```bash
source ~/.zshrc   # or ~/.bashrc
```

### Manual

Copy the contents of [`wt.sh`](./wt.sh) into your `~/.zshrc` or `~/.bashrc`.

---

## Usage

```bash
# Create a new worktree + branch:
wt <branch-name>
wt feature/my-thing

# Create a review worktree from a PR/MR number:
wtpr <pr-number>
wtmr <pr-number>

# Or pick from open PRs/MRs with fzf:
wtpr
wtmr

# Switch between worktrees (fzf picker):
wts                         # opens interactive picker
wts feat                    # pre-filters the list

# Delete a worktree (interactive picker):
wtd                         # opens interactive picker for deletion
wtd feat                    # pre-filters the list
```

---

## Lazygit integration

### Custom keybindings

TreeMan can add custom keybindings to lazygit for creating and deleting worktrees without leaving the UI.

| Key | Panel | Action |
|---|---|---|
| `W` | Branches | Create a new worktree + branch (prompts for branch name) |
| `D` | Worktrees | Delete the selected worktree and its branch |
| `D` | Branches | Delete the worktree associated with the selected branch and the branch itself |

**Install:**

`install.sh` automatically detects if lazygit is installed and idempotently injects the keybindings. Running it twice is safe.

```bash
curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/install.sh | bash
```

**Uninstall:**

The unified uninstaller natively detects and removes the injected lazygit configuration.

```bash
curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/uninstall.sh | bash
```

---

### `lg` wrapper

TreeMan includes an `lg` wrapper function for [lazygit](https://github.com/jesseduffield/lazygit). When you switch worktrees inside lazygit, `lg` automatically `cd`s your shell to the new worktree directory on exit.

```bash
lg          # launch lazygit with auto-cd
lg --help   # all lazygit flags work as normal
```

Use `Shift+Q` to quit lazygit without triggering a directory change.

The `lg` function is sourced alongside `wt` — no extra setup needed. If lazygit isn't installed, calling `lg` prints a warning and exits.

### Neovim

The `lg` shell wrapper doesn't help when lazygit runs inside Neovim — the terminal's `cd` can't reach Neovim's working directory. To get auto-cd working in Neovim, pass `LAZYGIT_NEW_DIR_FILE` via your plugin's `env` option and read it back on `TermClose`.

<details>
<summary>snacks.nvim (folke/snacks.nvim)</summary>

```lua
{ "<leader>lg", function()
  local newdir_file = vim.fn.expand("~/.lazygit/newdir")
  vim.fn.mkdir(vim.fn.expand("~/.lazygit"), "p")

  snacks.lazygit({
    env = { LAZYGIT_NEW_DIR_FILE = newdir_file },
  })

  vim.api.nvim_create_autocmd("TermClose", {
    pattern = "*lazygit*",
    once = true,
    callback = function()
      vim.schedule(function()
        local f = io.open(newdir_file, "r")
        if not f then return end
        local dir = f:read("*a"):gsub("%s+$", "")
        f:close()
        os.remove(newdir_file)
        if dir ~= "" and dir ~= vim.fn.getcwd() then
          vim.cmd("cd " .. vim.fn.fnameescape(dir))
        end
      end)
    end
  })
end, desc = "Lazygit" },
```

</details>

<details>
<summary>lazygit.nvim (kdheepak/lazygit.nvim)</summary>

`lazygit.nvim` supports this natively. Add to your config:

```lua
{
  "kdheepak/lazygit.nvim",
  config = function()
    vim.g.lazygit_use_neovim_remote = 1
  end,
}
```

The plugin sets `LAZYGIT_NEW_DIR_FILE` automatically and calls `cd` on exit.

</details>

The pattern is the same for any terminal plugin: set `LAZYGIT_NEW_DIR_FILE` as an env var, read the file after lazygit exits, and call `cd`.

---

## Uninstall

```bash
curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/uninstall.sh | bash
```

Or manually:
- Remove the `# TreeMan` and `source` lines from your shell config
- Delete `~/.treeman/`

### Uninstall lazygit keybindings

The lazygit configuration is automatically removed if you run the standard uninstall command (`curl .../uninstall.sh | bash`).

Or manually remove all lines marked with `# TreeMan` from your lazygit `config.yml` (find its location with `lazygit -cd`).
