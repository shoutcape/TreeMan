<img width="2758" height="1504" alt="TreeMan_Logo_no_white_bg_smooth2" src="https://github.com/user-attachments/assets/d12d7c55-cd61-4116-932d-e0f5f63ae613" />

Git worktree management CLI — create new branch worktrees, jump straight into them, spin up review worktrees from PRs or MRs, and switch between them with an interactive picker.

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

### Requirements

**Core**
    - `git`
    - `treeman` binary (installed via the one-liner or manually — see [Install](#install))
- `bash` or `zsh` (for shell wrapper functions)

    **For `wtpr` / `wtmr`**
    - [`gh`](https://cli.github.com/) — GitHub repos
    - [`glab`](https://gitlab.com/gitlab-org/cli) + [`jq`](https://jqlang.github.io/jq/) — GitLab repos (including self-hosted)

    **For `wts`, `wtd`, and the interactive PR/MR picker**
    - [`fzf`](https://github.com/junegunn/fzf)

    **For dependency auto-install**
- The package manager your project uses (only invoked when a matching lockfile is detected)
