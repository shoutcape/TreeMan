# TreeMan

A single shell function that creates a git worktree, a new branch based off the latest `main`, and auto-installs dependencies — in one command.

No runtime required. No config files. Works with bash and zsh.

```bash
wt feature/my-thing
```

```
Fetching latest main from origin...
Creating worktree at ~/Github/my-project.feature-my-thing (branch: feature/my-thing)...
Detected pnpm-lock.yaml — running pnpm install...

Worktree ready:
  cd /home/user/Github/my-project.feature-my-thing
```

---

## How it works

1. **Validates** you're inside a git repo with a branch name argument
2. **Detects the default branch** — checks for `origin/main`, falls back to `origin/master`
3. **Fetches** the latest from origin so the new branch is always up to date
4. **Creates** a new branch and worktree at `../<repo>.<branch-slug>` (sibling of your main repo)
5. **Installs dependencies** by detecting the project's lockfile:
   | Lockfile | Command |
   |---|---|
   | `pnpm-lock.yaml` | `pnpm install` |
   | `yarn.lock` | `yarn install` |
   | `package-lock.json` | `npm install` |
   | `go.mod` | `go mod download` |
   | `requirements.txt` / `pyproject.toml` | notifies you to activate venv manually |
6. **Prints the path** to `cd` into

Works correctly even when run from inside an existing worktree — always targets the main worktree root.

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
# From inside any git repo:
wt <branch-name>

# Both of these work once the alias is set up:
wt feature/my-thing
git wt feature/my-thing
```

---

## Uninstall

```bash
curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/uninstall.sh | bash
```

Or manually:
- Remove the `# TreeMan` and `source` lines from your shell config
- Run `git config --global --unset alias.wt`
- Delete `~/.treeman/`
