# TreeMan

A shell function for creating git worktrees with automatic branch setup and dependency installation.

```
wt <branch-name>
```

Creates a new worktree as a sibling directory, branched off the latest `origin/main`, and auto-installs dependencies based on the project's lockfile.

## What it does

1. Fetches the latest `main` (or `master`) from origin
2. Creates a new branch and worktree at `../<repo>.<branch-name>`
3. Detects and runs the appropriate dependency installer:
   - `pnpm-lock.yaml` → `pnpm install`
   - `yarn.lock` → `yarn install`
   - `package-lock.json` → `npm install`
   - `go.mod` → `go mod download`
   - Python project → notifies you to activate a venv manually
4. Prints the path to `cd` into

**Works from inside existing worktrees too** — always targets the main worktree root.

## Requirements

- `git`
- `bash` or `zsh`
- The relevant package manager for your project (only needed at install time)

## Install

### One-liner

```bash
curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/install.sh | bash
```

Then reload your shell:

```bash
source ~/.zshrc   # or ~/.bashrc
```

### Oh-my-zsh plugin

```bash
git clone https://github.com/shoutcape/TreeMan $ZSH_CUSTOM/plugins/TreeMan
```

Add `TreeMan` to your plugins list in `~/.zshrc`:

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

Copy the contents of `wt.sh` into your `~/.zshrc` or `~/.bashrc`.

## Usage

```bash
# From inside any git repo:
wt feature/my-thing

# Creates: ../my-project.feature-my-thing
# Branch:  feature/my-thing, based on origin/main
# Deps:    auto-installed from lockfile
```

Both of these are equivalent once the alias is set up:

```bash
wt feature/my-thing
git wt feature/my-thing
```

The `git wt` alias is registered automatically by the installer. To set it up manually:

```bash
git config --global alias.wt '!wt'
```

## Worktree naming

| Repo | Branch | Worktree path |
|------|--------|---------------|
| `~/Github/my-project` | `feature/cool-thing` | `~/Github/my-project.feature-cool-thing` |
| `~/Github/my-project` | `fix/bug-123` | `~/Github/my-project.fix-bug-123` |
| `~/Github/my-project` | `hotfix` | `~/Github/my-project.hotfix` |

Slashes in branch names are replaced with `-` in the directory name.

## Uninstall

```bash
curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/uninstall.sh | bash
```

Or manually: remove the `# TreeMan` and `source` lines from your shell config, and run `git config --global --unset alias.wt`.
