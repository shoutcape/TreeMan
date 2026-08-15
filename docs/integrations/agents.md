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

## Requirements

Install TreeMan before you install the skill. Read [Install TreeMan](../installation.md).

The agent must have access to `treeman` on `PATH`. The target repository needs Git and an `origin` remote.
