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
    TreeMan->>DB: try database cleanup
    TreeMan->>Git: worktree remove
    TreeMan->>Git: branch -d
    TreeMan-->>TreeMan: return success or error
```

TreeMan reads the environment file before Git removes the worktree. `--force` permits deletion of dirty worktrees and unmerged branches.
