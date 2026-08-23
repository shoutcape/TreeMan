# Install TreeMan

## Homebrew

Install TreeMan with Homebrew:

```bash
brew install shoutcape/tap/treeman
```

Enable shell integration:

```bash
treeman shell install
```

Homebrew installs the `treeman` binary. `treeman shell install` detects Bash, Zsh, or Fish and safely manages the appropriate startup-file entry. Use `--shell` or `--config` to override detection.

### Move From the Curl Installer

Remove the curl installer directory before you install with Homebrew:

```bash
rm -rf ~/.treeman/bin
```

Alternatively, give the Homebrew `bin` directory priority in `PATH`.

Remove the TreeMan shell initialization block from the curl installer.

## Release Binary

Use this command to install the latest release.

```bash
curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/install.sh | bash
```

The installer supports Linux and macOS. It supports amd64 and arm64 processors.

The installer puts `treeman` in `~/.treeman/bin` by default. It adds that directory and managed shell integration to your shell startup file.

Restart your shell after installation.

```bash
exec "$SHELL"
```

Set `TREEMAN_INSTALL_DIR` to use a different installation directory. Set `TREEMAN_SHELL_RC` to select a shell startup file.

## Manual Installation

Download a matching archive from [GitHub Releases](https://github.com/shoutcape/TreeMan/releases/latest). Put the `treeman` binary on `PATH`.

Enable shell integration.

```bash
treeman shell install
```

Use `--shell` or `--config` when automatic detection does not select the intended shell or configuration file.

## Install Local Changes

From a TreeMan repository checkout, run:

```bash
./install-local.sh
```

The script builds the current worktree and replaces the installed `treeman` binary. It uses an installed Homebrew formula directory when available; otherwise it uses the active `treeman` binary's prefix. For Homebrew, it updates the linked binary target without replacing Homebrew's public `bin/treeman` symlink, so `brew reinstall` and `brew upgrade` continue to work. A Homebrew reinstall or upgrade restores its packaged version. It requires Bash, Make, and Go 1.23 or later. Set `TREEMAN_INSTALL_DIR` to choose a different install prefix. `TREEMAN_SHELL_RC` works as described for the release installer.

## Build From Source

Install Go 1.23 or later. Then run:

```bash
go install github.com/shoutcape/treeman/cmd/treeman@latest
```

Run `treeman shell install` after the binary is on `PATH`.

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

If you installed with Homebrew, run:

```bash
brew uninstall shoutcape/tap/treeman
```

If you installed with the curl installer, use this command:

```bash
curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/uninstall.sh | bash
```

The script removes TreeMan blocks from common Bash, Zsh, and Fish startup files and removes the installation directory.

Set `TREEMAN_INSTALL_DIR` when you used a custom directory.
