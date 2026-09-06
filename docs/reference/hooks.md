# Hook Approval

TreeMan can run commands from `[hooks].post_create` in each new worktree. Hook
approval is user consent. It is not a sandbox.

## What Approval Covers

An approval covers one exact request. The request contains:

- The canonical absolute Git common-directory path.
- The absolute, cleaned path to `.treeman.toml`.
- The hook phase, currently `post_create`.
- Every command string, in its original order.

TreeMan preserves command bytes. It does not parse, normalize, trim, or
deduplicate shell strings. Changing whitespace, command order, additions,
removals, or duplicates creates a new approval ID. The ID is a SHA-256
fingerprint of the versioned scope.

TreeMan does not fingerprint the contents of a script. For example, approval
of `./setup.sh` approves that command string, not the current contents of
`setup.sh`, its dependencies, or commands that the script can execute. Review
the scripts and project configuration before you approve them.

The Git common directory identifies the repository. Linked worktrees of one
repository share approval state. Separate clones do not. Replacing a
repository at the same path can retain a matching approval. Approval is local
consent, not cryptographic repository provenance.

## Consent Rules

TreeMan checks hook policy before it fetches or creates a worktree. It uses
this order:

1. `--skip-hooks` skips hooks without reading approval state.
2. No configured hooks need no approval.
3. `--trust-hooks` approves hooks for this invocation only.
4. A matching saved approval allows the exact request.
5. An interactive command with no approval asks for consent.
6. A noninteractive command with no approval fails and shows `--trust-hooks`
   and `--skip-hooks` guidance.

The prompt shows the repository identity, configuration path, phase, execution
directory, and every command in order. Command text is not truncated. Terminal
control characters are escaped. The default answer is no.

Interactive acceptance saves the exact approval. Refusal, end-of-file input,
or a missing noninteractive consent stops creation before a branch or worktree
is created. If saving an accepted approval fails, TreeMan also stops creation.
It does not run hooks after a failed save.

`--trust-hooks` authorizes only the current invocation and never saves state.
`--skip-hooks` does not run hooks and does not read or write approval state.
Do not use both flags. TreeMan rejects them because their meanings conflict.

`--yes` is not hook authorization. It does not accept an approval prompt. This
also applies when `--yes` is used for another command that supports it.

After an approved worktree is created, TreeMan runs hooks in order. A failed
hook produces a warning, then TreeMan continues with later hooks and delivers
the worktree.

## Creation Flags

The following creation commands support hook approval:

```text
treeman create <branch-name> [--trust-hooks] [--skip-hooks]
treeman branch [query] [--trust-hooks] [--skip-hooks]
treeman review [pr-number] [--trust-hooks] [--skip-hooks]
```

`--trust-hooks` trusts post-create hooks for the current invocation. It does
not persist approval. `--skip-hooks` skips post-create hooks and approval state.
The other setup flags remain independent:

```text
--skip-env       Skip copying .env* files
--skip-database  Skip branch database setup
--skip-deps      Skip dependency installation
--skip-hooks     Skip post-create hooks
--trust-hooks    Trust post-create hooks for this invocation
```

The hook decision is made after configuration and destination resolution, but
before fetches that mutate references or worktree creation. If destination
selection changes before creation, TreeMan refuses to run the approved hooks
in a different directory.

## Manage Approvals

Approval management does not load or execute project hooks. It works outside a
Git repository.

```text
treeman hooks approvals list
```

`list` displays approval IDs, repository identities, configuration paths,
phases, approval times, and ordered commands. It uses deterministic ID order.

`revoke` requires the exact approval ID. It removes only that record. An
unknown ID is an error.

## Benchmark Flags

The benchmark command accepts the creation setup flags and `--trust-hooks`:

```text
treeman benchmark [command] [target] [--skip-env] [--skip-database] \
  [--skip-deps] [--skip-hooks] [--trust-hooks]
```

Only the `delete` benchmark target prepares a worktree and runs project setup.
The `branch` and `review` targets measure worktree creation only: they always
skip setup and do not read approval state or execute hooks. All setup and hook
policy flags are valid only for `delete`; other targets reject them.
`--trust-hooks` is invocation-only and does not save approval. Use
`--skip-hooks` to bypass approval and hook execution during delete preparation.
