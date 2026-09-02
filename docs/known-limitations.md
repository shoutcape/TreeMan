# Known Limitations

This document describes current behavior. It is not a product roadmap.

## Git and Worktrees

- Default branch detection reads `refs/remotes/origin/HEAD` first. If that ref is absent, TreeMan finds only `main` or `master` on `origin`.
- Branch validation does not implement all Git ref rules.
- Slug collision detection compares raw directory paths. Symlinked or equivalent paths can differ.
- A directory that is not a worktree stops creation at the target path. TreeMan does not add a slug suffix in this condition.
- Do not run direct `git worktree` mutations concurrently with `treeman clean` or `treeman delete`. TreeMan serializes its own worktree additions and guarded deletions, but direct Git commands do not participate in that lock.

## Forge Data

- Branch and PR or MR lists stop at 5000 items or 50 pages. The picker does not show a message when a list stops at one of these limits.
- Protected remote branches are not filtered.

## Database Actions

- A failed database drop remains pending until a later `treeman clean` can reach its configured container.
- Legacy branch databases created before ownership records are not removed automatically.
- Docker container identity is not durable across external container replacement. Cleanup requires the exact recorded container ID, so the database remains pending until that container is available again or is handled manually.

## Platform Support

- Release binaries support Linux and macOS only.
- Shell wrappers support Bash, Zsh, and Fish.
