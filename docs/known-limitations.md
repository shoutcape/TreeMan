# Known Limitations

This document describes current behavior. It is not a product roadmap.

## Git and Worktrees

- Default branch detection supports only `main` and `master`.
- Branch validation does not implement all Git ref rules.
- Branch names with `/` can collide after slug conversion to `-`.
- Do not run direct `git worktree` mutations concurrently with `treeman clean` or `treeman delete`. TreeMan serializes its own worktree additions and guarded deletions, but direct Git commands do not participate in that lock.

## Forge Data

- Branch and PR or MR lists have a 100-item limit.
- Protected remote branches are not filtered.

## Database Actions

- A failed database drop remains pending until a later `treeman clean` can reach its configured container.
- Legacy branch databases created before ownership records are not removed automatically.
- Docker container identity is not durable across external container replacement. Cleanup requires the exact recorded container ID, so the database remains pending until that container is available again or is handled manually.

## User Interface

- `switch` compares raw directory paths. Symlinked or equivalent paths can differ.

## Platform Support

- Release binaries support Linux and macOS only.
- Shell wrappers support Bash, Zsh, and Fish.
