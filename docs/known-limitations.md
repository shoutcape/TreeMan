# Known Limitations

This document describes current behavior. It is not a product roadmap.

## Git and Worktrees

- Default branch detection reads `refs/remotes/origin/HEAD` first. If that ref is absent, TreeMan finds only `main` or `master` on `origin`.
- Branch validation does not implement all Git ref rules.
- Slug collision detection compares raw directory paths. Symlinked or equivalent paths can differ.
- A directory that is not a worktree stops creation at the target path. TreeMan does not add a slug suffix in this condition.
- Do not run direct `git worktree` mutations concurrently with `treeman clean` or `treeman delete`. TreeMan serializes its own worktree additions and guarded deletions, but direct Git commands do not participate in that lock.
- Do not concurrently replace, move, or delete a worktree directory with direct filesystem commands while TreeMan removes it. TreeMan guards its own operations, but external filesystem mutations do not participate in that coordination.
- File cleanup after Git deletion is asynchronous and is retried only when a later worktree removal runs. Interrupted staging and failed restoration leave captured directories protected for manual recovery, even if their Git registration is later removed. Recover them from the location named in the error before retrying removal. A capture whose staging metadata does not record where it came from cannot be matched to a worktree or restored automatically; it stays protected and is reported by every later removal until you recover it.

- The `treeman setup` lock coordinates TreeMan setup runs for one worktree. It does not coordinate direct Git commands, editors, or other processes that write in that worktree at the same time.

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
- Linux uses atomic no-replace restoration and descriptor-relative cleanup paths. On macOS, restoration checks the destination before renaming and cleanup uses ordinary paths. Linux falls back to that same check on filesystems that do not implement the atomic flag, such as NFS and some FUSE mounts, because refusing to restore a capture would be worse than the narrower guarantee. Both platforms lock active cleanup jobs, but the check-then-rename fallbacks cannot prevent an external process from replacing paths between validation and use.
