# Architecture Overview

TreeMan is a single-process command-line program. It has no server and no persistent application database.

The `cmd/treeman` program creates the Cobra root command. `internal/cmd` coordinates each user command. Small internal packages perform Git, configuration, file, forge, database, and user-interface actions.

```mermaid
flowchart TD
    Main[cmd/treeman] --> Command[internal/cmd]
    Command --> Git[internal/git]
    Command --> Worktree[internal/worktree]
    Command --> Config[internal/config]
    Command --> Env[internal/envfile]
    Command --> Dependencies[internal/deps]
    Command --> Hooks[internal/hooks]
    Command --> Database[internal/database]
    Command --> Forge[internal/forge]
    Forge --> Remote[internal/remote]
    Command --> UI[internal/ui]
```

## State Ownership

| State | Owner |
| --- | --- |
| Branches and worktree records | Git |
| Worktree directories | Git and file system |
| Internal worktree-directory ignore entry | Main worktree `.gitignore` |
| Environment files | Main and linked worktrees |
| Project configuration | `.treeman.toml` |
| Branch databases | Docker PostgreSQL |

## Failure Boundaries

TreeMan stops commands for input errors, repository errors, required tool errors, Git creation errors, and forge API errors.

TreeMan reports warnings and continues for `.gitignore` updates, environment copies, database actions, dependency installs, hooks, and upstream setup.

Deletion validates the linked worktree, branch, protected state, and dirty state before it begins. Git removal and branch deletion complete before the command returns.

## External Programs

TreeMan runs `git`, `fzf`, `gh`, `glab`, package managers, Docker, and `psql` when their related feature runs.

Hooks run configured shell text. Treat project configuration as executable input.
