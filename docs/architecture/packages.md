# Package Map

## Entry Point

| Path | Responsibility |
| --- | --- |
| `cmd/treeman/main.go` | Build data, root command execution, process exit |

## Command Package

| Path | Responsibility |
| --- | --- |
| `internal/cmd/root.go` | Root command and deferred delete errors |
| `internal/cmd/create.go` | Local branch and worktree creation |
| `internal/cmd/branch.go` | Remote branch selection and creation |
| `internal/cmd/review.go` | PR and MR review worktrees |
| `internal/cmd/switch.go` | Worktree path selection |
| `internal/cmd/delete.go` | Interactive and direct worktree deletion |
| `internal/cmd/list.go` | Worktree listing and merge-state rendering |
| `internal/cmd/clean.go` | Merge-aware worktree cleanup |
| `internal/cmd/benchmark.go` | List benchmark progress and result rendering |
| `internal/cmd/init.go` | Bash, Zsh, and Fish wrapper text |
| `internal/cmd/version.go` | Version output |

## Domain and Runtime Packages

| Package | Responsibility |
| --- | --- |
| `internal/git` | Git process calls and worktree data |
| `internal/worktree` | Branch slugs, paths, and ignore-file changes |
| `internal/config` | Project and global TOML configuration |
| `internal/envfile` | Root-level `.env*` file copies |
| `internal/deps` | Lockfile detection and dependency commands |
| `internal/hooks` | Post-create shell commands |
| `internal/database` | PostgreSQL URI, environment rewrite, Docker, and cleanup |
| `internal/forge` | GitHub and GitLab detection, CLI calls, REST calls, and GraphQL snapshots |
| `internal/merge` | Fresh evidence acquisition and pure merge cleanup policy |
| `internal/remote` | Git remote parsing |
| `internal/ui` | ANSI color and picker rows |
| `internal/validate` | Branch and review input checks |

## Core Types

| Type | Purpose |
| --- | --- |
| `git.WorktreeEntry` | Worktree path and branch data |
| `config.Config` | Database and hook configuration |
| `database.ParsedURI` | PostgreSQL URI parts |
| `database.SetupResult` | Database creation result |
| `forge.PRInfo` | PR or MR data for selection and review |
| `forge.BranchInfo` | Remote branch data |
| `forge.GitHubSnapshot` | Fresh GitHub branch and merge evidence |
| `merge.Candidate` | Exact local branch tip eligible for cleanup |
| `merge.Snapshot` | Normalized fresh merge evidence before policy evaluation |

Core packages, including `internal/merge`, have focused unit tests for their safety-critical behavior.
