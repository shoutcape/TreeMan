# Install TreeMan

## Release Binary

Use this command to install the latest release.

```bash
curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/install.sh | bash
```

The installer supports Linux and macOS. It supports amd64 and arm64 processors.

The installer puts `treeman` in `~/.treeman/bin` by default. It adds that directory to your shell startup file. It also adds a shell wrapper command.

Restart your shell after installation.

```bash
exec "$SHELL"
```

Set `TREEMAN_INSTALL_DIR` to use a different installation directory. Set `TREEMAN_SHELL_RC` to select a shell startup file.

## Manual Installation

Download a matching archive from [GitHub Releases](https://github.com/shoutcape/TreeMan/releases/latest). Put the `treeman` binary on `PATH`.

Add one command to your shell startup file.

```bash
eval "$(treeman init zsh)"
```

Use `bash` instead of `zsh` for Bash. For Fish, add this command to `~/.config/fish/config.fish`.

```fish
treeman init fish | source
```

## Build From Source

Install Go 1.23 or later. Then run:

```bash
go install github.com/shoutcape/treeman/cmd/treeman@latest
```

Add the shell wrapper command from the manual installation section.

For repository development, read [Development Setup](development/setup.md).

## Requirements

| Feature | Required program |
| --- | --- |
| All Git worktree actions | `git` |
| Shell wrappers | `bash`, `zsh`, or `fish` |
| Interactive selection | `fzf` |
| GitHub branches and reviews | `gh` |
| GitLab branches and reviews | `glab` |
| Dependency installation | Project package manager |
| Branch databases | `docker` and a running PostgreSQL container |

## Remove TreeMan

Use this command:

```bash
curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/uninstall.sh | bash
```

The script removes TreeMan blocks from common Bash, Zsh, and Fish startup files. It removes the installation directory. It also tries to remove TreeMan lazygit commands.

Set `TREEMAN_INSTALL_DIR` when you used a custom directory.
