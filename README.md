<img width="2758" height="1504" alt="TreeMan_Logo_no_white_bg_smooth2" src="https://github.com/user-attachments/assets/d12d7c55-cd61-4116-932d-e0f5f63ae613" />

TreeMan is a Go CLI for Git worktree workflows with an optional per-worktree runtime layer.

- Create branch worktrees quickly
- Open review worktrees from GitHub PRs or GitLab MRs
- Switch and delete worktrees with an `fzf` picker
- Run isolated dev servers per worktree with auto-assigned ports

```bash
wt feature/my-thing         # create a new worktree + branch
wtpr 123                    # create a review worktree from PR/MR #123
wtmr                        # pick an open PR/MR with fzf
wts                         # switch between worktrees
wtd                         # delete a worktree

treeman init                # create .treeman.yml
treeman runtime up          # start per-worktree runtime
treeman runtime status      # show runtime status
treeman runtime env         # print assigned env vars
treeman runtime down        # stop runtime
```

The shell aliases come from `wt.sh`. The actual implementation lives in the `treeman` binary.

### `wt` - create

<video src="https://github.com/user-attachments/assets/7491c0eb-896f-4046-b46b-1c3db01619c3" width="400"></video>

```text
Fetching latest main from origin...
Creating worktree at ~/Github/my-project.feature-my-thing (branch: feature/my-thing)...
Copied .env, .env.local
Detected pnpm-lock.yaml - running pnpm install...

Worktree ready:
  Path: /home/user/Github/my-project.feature-my-thing
```

### `wtpr` / `wtmr` - review

`wtpr` and `wtmr` are identical. TreeMan uses PR and MR interchangeably for the review-worktree flow.

```text
$ wtpr 123
Fetching PR/MR #123 from origin...
Creating review worktree at ~/Github/my-project.feature-login-fix (branch: feature/login-fix)...
Detected pnpm-lock.yaml - running pnpm install...

Review worktree ready:
  PR/MR:  #123
  Title:  Fix login redirect loop
  Branch: feature/login-fix
  Path:   /home/user/Github/my-project.feature-login-fix
```

If you omit the number, TreeMan can open an `fzf` picker of open PRs/MRs.

### `wts` - switch

<video src="https://github.com/user-attachments/assets/ebb762c0-f530-4f35-aa3e-4b2c04c1f75b" width="300"></video>

TreeMan shows shortened worktree paths plus branch names, then prints the selected path so the shell wrapper can `cd` into it.

### `wtd` - delete

TreeMan confirms before deletion, protects the main worktree, and removes the worktree first before deleting the branch.

If Git refuses to remove a dirty worktree, TreeMan fails safely and tells you to force removal manually with `git worktree remove --force`.

### Runtime isolation

`treeman runtime` lets each worktree run its own dev server with separate ports and generated env vars.

```yaml
runtime:
  type: process
  command: pnpm dev
  env_file: .env.treeman
  ports:
    app: 3000
    api: 4000
```

When you run `treeman runtime up`, TreeMan:

- allocates unique ports for the current worktree
- writes the generated env file
- starts the configured process
- stores runtime state under `~/.treeman/`

Available runtime commands:

```bash
treeman runtime up
treeman runtime down
treeman runtime status
treeman runtime logs
treeman runtime env
treeman runtime ls
```

`env_file` must stay inside the worktree. Relative nested paths like `config/.env.treeman` are supported.

---

## How it works

### `wt`

1. Validates that you are inside a Git repo and that the branch name is valid.
2. Detects the default branch from `origin`.
3. Fetches the latest default branch.
4. Creates a sibling worktree at `../<repo>.<branch-slug>`.
5. Copies `.env*` files from the main worktree.
6. Detects dependencies and runs one of:

| Lockfile | Command |
|---|---|
| `pnpm-lock.yaml` | `pnpm install` |
| `yarn.lock` | `yarn install` |
| `package-lock.json` | `npm install` |
| `go.mod` | `go mod download` |
| `requirements.txt` / `pyproject.toml` | warns to activate a venv manually |

### `wtpr` / `wtmr`

1. Detects GitHub or GitLab from the `origin` remote.
2. Reads PR/MR metadata with `gh api` or `glab api`.
3. Optionally opens an `fzf` picker for open PRs/MRs.
4. Fetches the review ref from origin.
5. Creates a review worktree from `FETCH_HEAD`.

If the PR/MR head branch already exists locally, TreeMan fails instead of guessing.

### `wts`

1. Lists worktrees with `git worktree list`.
2. Shows them in `fzf`.
3. Prints the selected path for the shell wrapper to `cd` into.

### `wtd`

1. Excludes the main worktree from deletion.
2. Prompts for confirmation.
3. Stops any tracked runtime for that worktree.
4. Runs `git worktree remove`.
5. Runs `git branch -D`.
6. Cleans runtime state, logs, generated env file, and port allocations only after git deletion succeeds.

---

## Worktree naming

Slashes in branch names become dashes in worktree directory names.

| Repo | Branch | Worktree directory |
|---|---|---|
| `~/Github/my-project` | `feature/cool-thing` | `~/Github/my-project.feature-cool-thing` |
| `~/Github/my-project` | `fix/bug-123` | `~/Github/my-project.fix-bug-123` |
| `~/Github/my-project` | `hotfix` | `~/Github/my-project.hotfix` |

---

## Supported forges

`wtpr` / `wtmr` support:

| Forge | Detection | CLI tool | Fetch ref |
|---|---|---|---|
| GitHub | host is `github.com` | `gh` | `pull/<n>/head` |
| GitLab | host contains `gitlab` | `glab` + `jq` | `merge-requests/<n>/head` |

Supported remote URL formats include:

- `git@github.com:owner/repo.git`
- `git@gitlab.company.com:group/subgroup/project.git`
- `https://github.com/owner/repo.git`
- `ssh://git@github.com/owner/repo.git`

---

## Requirements

- `git`
- `bash` or `zsh` for the shell wrapper
- `fzf` for `wts`, `wtd`, and optional review picking
- `gh` for GitHub review worktrees
- `glab` and `jq` for GitLab review worktrees
- the package manager your project uses

For runtime commands, TreeMan currently targets Unix-like process management. Linux and macOS are the supported install targets in `install.sh`.

---

## Install

### One-liner

This installs the `treeman` binary into `~/.treeman/`, downloads `wt.sh`, adds both to your shell config, and optionally injects lazygit bindings.

```bash
curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/install.sh | bash
```

Reload your shell when done:

```bash
source ~/.zshrc   # or ~/.bashrc
```

### Manual

```bash
go build -o treeman ./cmd/treeman
```

Then place the `treeman` binary somewhere on your `PATH`, and source [`wt.sh`](./wt.sh) from your shell config:

```bash
export PATH="/path/to/treeman/bin:$PATH"
source "/path/to/TreeMan/wt.sh"
```

---

## Usage

### Shell aliases

```bash
wt <branch-name>
wtpr [pr-number]
wtmr [pr-number]
wts [query]
wtd [query]
lg
```

### CLI

```bash
treeman worktree create <branch>
treeman worktree review [number]
treeman worktree switch [query]
treeman worktree delete [query]
treeman worktree list
treeman worktree main

treeman runtime up
treeman runtime down
treeman runtime status
treeman runtime logs
treeman runtime env
treeman runtime ls

treeman init
treeman version
```

### `.treeman.yml`

Generate a starter config:

```bash
treeman init
```

Example process runtime:

```yaml
runtime:
  type: process
  command: pnpm dev
  env_file: config/.env.treeman
  ports:
    app: 3000
```

---

## Lazygit integration

TreeMan can inject lazygit custom commands for creating and deleting worktrees.

| Key | Panel | Action |
|---|---|---|
| `W` | Branches | Create a new worktree + branch |
| `D` | Worktrees | Delete the selected worktree and its branch |
| `D` | Branches | Delete the selected branch's worktree and branch |

The installer adds these automatically when lazygit is present.

### `lg` wrapper

TreeMan also ships an `lg` shell wrapper for [lazygit](https://github.com/jesseduffield/lazygit). When you switch worktrees inside lazygit, `lg` can update your shell directory after lazygit exits.

```bash
lg
lg --help
```

---

## Uninstall

```bash
curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/uninstall.sh | bash
```

Manual uninstall:

- remove the `# TreeMan` block from your shell config
- delete `~/.treeman/`
- remove the TreeMan block from lazygit `config.yml` if you added it manually
