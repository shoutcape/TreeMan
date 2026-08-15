# Legacy `wt.sh`

`wt.sh` is a legacy shell implementation. The Go binary is the authoritative TreeMan implementation.

Do not use `wt.sh` as current command documentation.

## Important Differences

| Behavior | Legacy `wt.sh` | Go CLI |
| --- | --- | --- |
| Worktree path | Sibling directory | `.worktrees/<branch-slug>` |
| Directory change | Shell code changes directory | Wrapper reads CLI stdout path |
| Deletion | Synchronous | Detached background process |
| Configuration | Legacy shell behavior | `.treeman.toml` and global TOML behavior |
| Command source | Shell functions | `treeman` Cobra commands |

The uninstaller still removes a legacy shell startup format. This support does not make `wt.sh` a current interface.
