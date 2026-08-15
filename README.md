<img width="2758" height="1504" alt="TreeMan logo" src="https://github.com/user-attachments/assets/d12d7c55-cd61-4116-932d-e0f5f63ae613" />

# TreeMan

TreeMan is a Git worktree management CLI. It creates branch worktrees, adds remote branches, makes PR or MR review worktrees, and selects worktrees.

TreeMan has one compiled binary. Shell wrappers support Bash and Zsh.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/install.sh | bash
```

Restart the shell after installation.

```bash
exec "$SHELL"
```

Read [installation details](docs/installation.md) for manual installation, source builds, requirements, and removal.

## Start

```bash
wt feature/login
wtb
wtpr 42
wts
wtd
```

| Wrapper | Native command | Purpose |
| --- | --- | --- |
| `wt <branch>` | `treeman create <branch>` | Create a local branch worktree |
| `wtb [query]` | `treeman branch [query]` | Add a remote branch worktree |
| `wtpr [number]` | `treeman review [number]` | Add a review worktree |
| `wtmr [number]` | `treeman review [number]` | Add a review worktree |
| `wts [query]` | `treeman switch [query]` | Change shell directory to a worktree |
| `wtd [query]` | `treeman delete [query]` | Delete a worktree and branch |

Read [Getting Started](docs/getting-started.md) and the [Command Reference](docs/reference/cli.md).

> [!warning]
> `wtd` starts background deletion. It uses forced Git worktree and branch removal. Uncommitted and untracked files can be removed.

## Documentation

- [Documentation index](docs/README.md)
- [Workflows](docs/guides/workflows.md)
- [Configuration](docs/reference/configuration.md)
- [GitHub and GitLab](docs/integrations/github-gitlab.md)
- [PostgreSQL branch databases](docs/integrations/postgresql.md)
- [Troubleshooting](docs/operations/troubleshooting.md)
- [Known limitations](docs/known-limitations.md)
- [Architecture](docs/architecture/overview.md)
- [Development](docs/development/setup.md)

## Legacy Shell Script

`wt.sh` is legacy code. The Go CLI is the authoritative implementation. Read [legacy status](docs/development/legacy-wt-sh.md).

## Documentation Standard

Project documentation uses ASD-STE100 Simplified Technical English, Issue 9, as its writing standard. Read [writing controls](docs/writing-standard.md).
