# Use TreeMan With Agents

TreeMan has an Agent Skill. The skill gives an agent TreeMan commands and safety rules.

## Install The Skill

Install the skill for one supported agent.

```bash
npx skills add shoutcape/TreeMan --skill treeman -g -a opencode
```

Change `opencode` to the agent that you use. Remove `-g` to install the skill in the current project.

Install the skill for all supported agents.

```bash
npx skills add shoutcape/TreeMan --skill treeman --agent '*' -g
```

Use this command to list skills before installation.

```bash
npx skills add shoutcape/TreeMan --list
```

The `skills` command uses the open Agent Skills format. The TreeMan skill source is [skills/treeman/SKILL.md](../../skills/treeman/SKILL.md).

## Agent Behavior

The skill tells an agent to use native `treeman` commands. Shell wrappers cannot change an agent parent shell directory.

Use `treeman list --json` when an agent needs to discover existing worktrees. The command reports each path, branch, main and current markers, dirty state, and detached state without requiring an interactive picker.

For a new feature, the agent runs `treeman create <branch>`. It reads the printed worktree path. It uses that path for later commands.

The skill does not permit deletion without an explicit user request. TreeMan verifies direct deletion targets and refuses dirty worktrees unless the command explicitly uses `--force`.

## Claude Code Worktrees

Claude Code makes worktrees for `claude --worktree`, the `EnterWorktree` tool, subagents with `isolation: "worktree"`, and background sessions. Without TreeMan, Claude Code uses plain Git worktrees under `.claude/worktrees/`. TreeMan can make these worktrees instead, with its paths, branch names, and setup.

### Install the Claude Code Hooks

1. Install TreeMan, and make sure that `treeman` is on `PATH`.
2. Run this command:

   ```bash
   treeman claude install
   ```

3. Restart the Claude Code sessions that are open.

The command adds `WorktreeCreate` and `WorktreeRemove` hooks to `~/.claude/settings.json`. When `CLAUDE_CONFIG_DIR` is set, it uses `$CLAUDE_CONFIG_DIR/settings.json`. Use `--settings <path>` to edit a different file.

The command keeps all other settings and their order. It copies the original file to `settings.json.treeman-backup-<time>` before it writes. A second run changes nothing. Use `--dry-run` to see the changes without a write.

If the file has a different `WorktreeCreate` or `WorktreeRemove` hook, the command stops and shows that hook. Use `--replace` to replace it with the TreeMan hook.

### Worktree Names

| Claude Code name | TreeMan result |
| --- | --- |
| `pr-<n>` | Worktree for PR or MR `<n>`, as `treeman review <n>` makes |
| `auth-fix` | Branch `feature/auth-fix` |
| `feature-auth-fix` | Branch `feature/auth-fix` |

The worktree path follows the usual TreeMan rules, which include `worktree_dir`. For a bare repository, TreeMan never puts worktrees in the Git directory. It uses `.worktrees/` next to a `.git` file that points to the bare repository. Otherwise, it uses a `<name>-worktrees` directory next to the bare repository.

### Create Behavior

`treeman claude worktree-create` reads the hook input from Claude Code on stdin. It writes only the worktree path to stdout. It writes status and warnings to stderr.

- When Claude Code asks for a name again, TreeMan returns the worktree that it made for that name.
- When the branch exists without a worktree, TreeMan adds a worktree for that branch.
- TreeMan never returns the main worktree. It never returns a worktree that it did not make for Claude Code. In these cases, the command fails with a message.
- A new branch starts from the latest `origin/<default-branch>`. If the fetch fails, the branch starts from the local copy of `origin/<default-branch>`. If there is no `origin`, the branch starts from `HEAD`. TreeMan shows a warning in both cases.
- TreeMan copies `.env*` files, installs dependencies, and sets up databases, as `treeman create` does.
- Post-create hooks run only when a saved approval matches them. Otherwise, TreeMan skips them and shows a warning. To approve hooks, run `treeman setup --rerun-hooks` in a worktree of the repository.

The command never asks a question. It turns off Git, SSH, and `gh` credential prompts. It keeps an SSH command that you configured.

### Remove Behavior

Claude Code can remove a worktree without a question to the user. Therefore, `treeman claude worktree-remove` is careful:

- If TreeMan made the worktree for Claude Code, TreeMan removes it, also when it has changes.
- TreeMan removes a different worktree only when it is clean. Otherwise, the command fails and keeps the worktree.
- TreeMan deletes the branch only when TreeMan made the branch for Claude Code and the branch is merged into the default branch or into `HEAD` of the main worktree.
- TreeMan keeps all other branches.

TreeMan records the worktrees and branches that it makes for Claude Code in `<git-common-dir>/treeman/agent-worktrees.json`. `treeman delete` and `treeman clean` update this record. Thus, a new branch with the name of a deleted branch is never taken as a TreeMan branch.

Claude Code does not remove subagent worktrees. Use `treeman clean` to remove them after their branches merge.

> [!NOTE]
> A branch that is deleted with `git branch -D` and made again is not a TreeMan branch. TreeMan compares the first entry in the branch reflog with its record. A bare repository has no branch reflogs by default. Therefore, `worktree-remove` keeps branches in bare repositories. Use `treeman clean` or `treeman delete` for them.

## Requirements

Install TreeMan before you install the skill. Read [Install TreeMan](../installation.md).

The agent must have access to `treeman` on `PATH`. The target repository needs Git and an `origin` remote.
