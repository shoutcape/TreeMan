# Command Lifecycles

## Create

```mermaid
sequenceDiagram
    participant User
    participant Shell as Shell wrapper
    participant CLI as TreeMan
    participant Git
    participant Setup as Post-create setup
    User->>Shell: wt feature/name
    Shell->>CLI: treeman create feature/name
    CLI->>Git: detect main worktree and default branch
    CLI->>Git: fetch origin default branch
    CLI->>Git: add linked worktree and branch
    CLI->>Setup: update ignore file and copy environment files
    CLI->>Setup: load config, database, dependencies, and hooks
    CLI-->>Shell: path on stdout
    Shell->>Shell: cd to path
```

## Remote Branch and Review

```mermaid
sequenceDiagram
    participant CLI as TreeMan
    participant Git
    participant Forge as gh or glab
    participant Picker as fzf
    CLI->>Git: read origin URL
    CLI->>Forge: get branch or review data
    CLI->>Picker: select item when no exact input exists
    CLI->>Git: fetch branch or review ref
    CLI->>Git: add linked worktree
    CLI->>CLI: run post-create setup
```

## Switch

```mermaid
flowchart LR
    A[Read Git worktrees] --> B[Select with fzf]
    B --> C[Print path to stdout]
```

## Delete

```mermaid
sequenceDiagram
    participant TreeMan
    participant Git
    participant DB as Branch database
    TreeMan->>Git: verify linked worktree and branch
    TreeMan->>TreeMan: protect main and default branch; inspect dirty state
    TreeMan->>DB: prepare database cleanup target
    TreeMan->>Git: worktree remove
    TreeMan->>Git: compare-and-delete branch at verified SHA
    TreeMan->>DB: try database cleanup
    TreeMan-->>TreeMan: return success or error
```

TreeMan reads and prepares the database target before Git removes the worktree. It drops the database only after worktree and branch deletion succeed. Branch deletion compares the current ref with the SHA that passed deletion checks. TreeMan serializes its own worktree additions and guarded deletions, preserving a branch that another TreeMan process checks out before deletion. Direct Git worktree mutation is not coordinated. `--force` permits deletion of dirty worktrees and unmerged branches.

## List and Clean

`list` and `clean` capture local branch tips, then read fresh remote merge state. GitHub uses one complete GraphQL snapshot per bounded batch with default ref, candidate ref, and PR evidence. Incomplete snapshot candidates use exact-SHA REST verification only after stable remote-gone non-ancestor checks. A failed snapshot records a diagnostic and falls back to fresh Git state plus that bounded REST verification. GitLab batches deleted-branch merge verification. Other remotes use Git and bounded forge verification.

TreeMan fetches the default branch only when its tracking ref is stale. GitHub requests over the batch limit are not globally atomic; TreeMan verifies that the default branch SHA remains stable between batches. `clean` repeats classification after confirmation, including with `--yes`, and deletes only matching branch tips.
