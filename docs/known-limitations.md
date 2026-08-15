# Known Limitations

This document describes current behavior. It is not a product roadmap.

## Git and Worktrees

- Default branch detection supports only `main` and `master`.
- Branch validation does not implement all Git ref rules.
- Branch names with `/` can collide after slug conversion to `-`.
- Direct delete mode does not verify the supplied worktree path and branch relationship.
- Deletion uses force removal and force branch deletion.

## Forge Data

- Branch and PR or MR lists have a 100-item limit.
- Protected remote branches are not filtered.
- GitLab commands require `jq` although API parsing is in Go.
- Large numeric review arguments can overflow internal integer parsing.

## Database Actions

- Long branch names can collide after 63-character database name truncation.
- A database can remain after environment-file rewrite failure.
- SQL identifier text is not escaped for embedded quotes.
- The `__` cleanup check reduces risk but does not prove database ownership.

## Background Deletion

- Multiple deletion processes can write to the same error log.
- Error reporting can race with a running deletion process.

## User Interface

- Identical picker display rows can map to the wrong worktree.
- `switch` compares raw directory paths. Symlinked or equivalent paths can differ.

## Platform Support

- Release binaries support Linux and macOS only.
- Shell wrappers support Bash and Zsh only.
