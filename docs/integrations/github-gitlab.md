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

Use `tmb` to add a remote branch. Use `tmpr <number>` to add a pull request worktree.

## GitLab

Install and authenticate GitLab CLI.

```bash
glab auth login
```

TreeMan uses `glab api` for branch lists, merge request lists, merge request data, and merge request fetches.

Use `tmb` to add a remote branch. Use `tmmr <number>` to add a merge request worktree.

TreeMan passes the remote host to `glab`. This supports GitLab hosts with `gitlab` in their host name.

## Branch List Rules

TreeMan reads all pages of the remote branch list and the open PR or MR list. Each page has at most 100 items. TreeMan sends each page to the picker when the page arrives. The first rows show before the last page arrives.

TreeMan stops a list at 5000 items or at 50 pages. The first limit that TreeMan reaches stops the list. These limits keep the number of requests low on a very large repository.

If you close the picker, TreeMan stops the requests that are still in progress.

TreeMan excludes the detected default branch and existing local branches. It gets protected-branch data but does not use it to filter branches.

For squash and rebase cleanup, TreeMan verifies the source branch name and matches the local tip against merged head SHAs. The tip qualifies when it is that head, or when the merged head reaches it.

GitHub reads the default ref, candidate branch presence, and merged pull-request heads in one fresh GraphQL snapshot per batch. TreeMan then refreshes the default branch and checks local ancestry. Ordinary worktree counts use one GraphQL request per classification. TreeMan shows a local tip as merged when it descends from a merged pull-request head, but that case stays informational: such a tip carries commits the merge never saw. Cleanup needs the merge to account for every commit on the tip, which holds when the tip is the merged head, and when the merged head reaches the tip. The second case is what a local checkout looks like after its pull request was updated on the remote and never pulled again. If TreeMan cannot read a complete snapshot, it records a warning and refreshes state with Git. It uses exact-SHA REST verification only for stable remote-gone non-ancestors.

GitHub snapshot candidates that require more data use exact-SHA per-branch REST verification. These candidates include fork PRs. GitLab batches remote-gone candidates in a paginated GraphQL merge-request query. It matches the source branch, target branch, and diff-head SHA exactly. If that query is unavailable, GitLab and other configurations use at most four requests at once.

## Authentication Errors

Run the forge CLI command directly when TreeMan reports an API error.

```bash
gh auth status
glab auth status
```

Verify that `origin` points to the intended repository. Verify that your token can read branches and pull requests or merge requests.
