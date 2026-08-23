# Terminology

Use these terms with the specified meaning.

| Term | Meaning |
| --- | --- |
| Agent Skill | Directory with instructions that extend an AI agent behavior |
| branch database | PostgreSQL database for one worktree branch |
| branch slug | Branch name with `/` changed to `-` for a worktree path |
| default branch | `main` or `master` branch detected from `origin` |
| formula | Ruby file that defines a Homebrew package |
| global configuration | `config.toml` in the TreeMan user configuration directory |
| Homebrew | Package manager for macOS and Linux |
| linked worktree | Worktree added by `git worktree add` |
| main worktree | First worktree from `git worktree list --porcelain` |
| post-create hook | Shell command that TreeMan runs after worktree creation |
| project configuration | `.treeman.toml` found at or above the main worktree |
| remote branch | Branch returned by the GitHub or GitLab API |
| shell integration | Managed Bash, Zsh, or Fish functions from `treeman shell install` |
| tap | Homebrew repository that provides formulas |
| worktree | Git checkout with its own directory and branch state |

Do not use `workspace`, `checkout directory`, or `working tree` to mean `worktree`.
