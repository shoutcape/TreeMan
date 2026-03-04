# TreeMan

Shell functions for git worktree workflows — create new worktrees with automatic dependency installation, and switch between them with an interactive picker.

No runtime required. No config files. Works with bash and zsh.

```bash
wt feature/my-thing        # create a new worktree + branch
wts                        # switch between worktrees (fzf picker)
```

### `wt` — create

<video src="https://github.com/user-attachments/assets/7491c0eb-896f-4046-b46b-1c3db01619c3" width="400"></video>

```
Fetching latest main from origin...
Creating worktree at ~/Github/my-project.feature-my-thing (branch: feature/my-thing)...
Copied .env, .env.local
Detected pnpm-lock.yaml — running pnpm install...

Worktree ready:
  cd /home/user/Github/my-project.feature-my-thing
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
7. **Prints the path** to `cd` into

Works correctly even when run from inside an existing worktree — always targets the main worktree root.

### `wts` — worktree switching

1. **Lists** all worktrees via `git worktree list`
2. **Opens an fzf picker** showing shortened paths (last two path components) and branch names with color highlighting
3. **`cd`s** into the selected worktree

An optional query argument pre-filters the list (e.g. `wts feat`). If the query matches exactly one worktree, it's selected automatically.

---

## Worktree naming

Slashes in branch names become dashes in the directory name:

| Repo | Branch | Worktree directory |
|---|---|---|
| `~/Github/my-project` | `feature/cool-thing` | `~/Github/my-project.feature-cool-thing` |
| `~/Github/my-project` | `fix/bug-123` | `~/Github/my-project.fix-bug-123` |
| `~/Github/my-project` | `hotfix` | `~/Github/my-project.hotfix` |

---

## Requirements

- `git`
- `bash` or `zsh`
- [`fzf`](https://github.com/junegunn/fzf) (for `wts` only)
- The package manager your project uses (only needed when a lockfile is detected)

---

## Install

### One-liner

Downloads `wt.sh` to `~/.treeman/`, adds a source line to your shell config, and registers the `git wt` alias.

```bash
curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/install.sh | bash
```

Reload your shell when done:

```bash
source ~/.zshrc   # or ~/.bashrc
```

### Oh-my-zsh plugin

```bash
git clone https://github.com/shoutcape/TreeMan ${ZSH_CUSTOM:-~/.oh-my-zsh/custom}/plugins/TreeMan
```

Add `TreeMan` to your plugins in `~/.zshrc`:

```zsh
plugins=(... TreeMan)
```

### zinit

```zsh
zinit light shoutcape/TreeMan
```

### antigen

```zsh
antigen bundle shoutcape/TreeMan
```

### Manual

Copy the contents of [`wt.sh`](./wt.sh) into your `~/.zshrc` or `~/.bashrc`, then run:

```bash
git config --global alias.wt '!wt'
```

---

## Usage

```bash
# Create a new worktree + branch:
wt <branch-name>
wt feature/my-thing
git wt feature/my-thing     # git alias

# Switch between worktrees (fzf picker):
wts                         # opens interactive picker
wts feat                    # pre-filters the list
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

```bash
bash install-lazygit.sh
```

Requires `lazygit` to be installed and TreeMan's `install.sh` to have been run first. The script auto-detects the lazygit config location on macOS and Linux and injects the keybindings idempotently — running it twice is safe.

**Uninstall:**

```bash
bash uninstall-lazygit.sh
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
  local snacks = require("snacks")
  local newdir_file = vim.fn.expand("~/.lazygit/newdir")
  vim.fn.mkdir(vim.fn.expand("~/.lazygit"), "p")

  local win = snacks.lazygit({
    env = { LAZYGIT_NEW_DIR_FILE = newdir_file },
  })

  win:on("TermClose", function()
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
  end, { buf = true })
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

<details>
<summary>toggleterm.nvim (akinsho/toggleterm.nvim)</summary>

```lua
local Terminal = require("toggleterm.terminal").Terminal

local lazygit = Terminal:new({
  cmd = "lazygit",
  direction = "float",
  env = { LAZYGIT_NEW_DIR_FILE = vim.fn.expand("~/.lazygit/newdir") },
  on_close = function()
    local newdir_file = vim.fn.expand("~/.lazygit/newdir")
    local f = io.open(newdir_file, "r")
    if not f then return end
    local dir = f:read("*a"):gsub("%s+$", "")
    f:close()
    os.remove(newdir_file)
    if dir ~= "" and dir ~= vim.fn.getcwd() then
      vim.cmd("cd " .. vim.fn.fnameescape(dir))
    end
  end,
})

vim.keymap.set("n", "<leader>lg", function() lazygit:toggle() end, { desc = "Lazygit" })
```

</details>

The pattern is the same for any terminal plugin: set `LAZYGIT_NEW_DIR_FILE` as an env var, read the file after lazygit exits, and call `cd`.

---

## Uninstall

```bash
curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/uninstall.sh | bash
```

Or manually:
- Remove the `# TreeMan` and `source` lines from your shell config
- Run `git config --global --unset alias.wt`
- Delete `~/.treeman/`

### Uninstall lazygit keybindings

```bash
bash uninstall-lazygit.sh
```

Or manually remove all lines marked with `# TreeMan` from your lazygit `config.yml` (find its location with `lazygit -cd`).
