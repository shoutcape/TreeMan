# GitHub and GitLab

TreeMan uses the `origin` remote URL to detect GitHub or GitLab. It supports SSH and HTTP remote URLs.

Supported forms include:

```text
git@host:group/project.git
ssh://git@host/group/project.git
https://host/group/project.git
http://host/group/project.git
```

## GitHub

Install and authenticate the GitHub CLI.

```bash
gh auth login
```

TreeMan uses `gh api` for branch lists, pull request lists, pull request data, and pull request fetches.

Use `wtb` to add a remote branch. Use `wtpr <number>` to add a pull request worktree.

## GitLab

Install and authenticate GitLab CLI.

```bash
glab auth login
```

TreeMan uses `glab api` for branch lists, merge request lists, merge request data, and merge request fetches.

Use `wtb` to add a remote branch. Use `wtmr <number>` to add a merge request worktree.

TreeMan passes the remote host to `glab`. This supports GitLab hosts with `gitlab` in their host name.

## Branch List Rules

TreeMan requests at most 100 remote branches. It also requests at most 100 open PRs or MRs for picker data.

TreeMan excludes the detected default branch and existing local branches. It gets protected-branch data but does not use it to filter branches.

For squash and rebase cleanup, TreeMan verifies the source branch name and exact head SHA.

GitHub uses a fresh GraphQL snapshot. Normal worktree counts use one request. Larger sets use bounded batches.

GitHub candidates requiring more data, including fork PRs, use per-branch verification. GitLab and other configurations use at most four requests at once.

## Authentication Errors

Run the forge CLI command directly when TreeMan reports an API error.

```bash
gh auth status
glab auth status
```

Verify that `origin` points to the intended repository. Verify that your token can read branches and pull requests or merge requests.
