# Testing

## Unit Tests

Run all unit tests.

```bash
go test ./...
```

Tests cover command helpers, configuration, database logic, dependency detection, environment files, forge APIs, Git calls, hooks, remote parsing, validation, and worktree rules.

## Command Benchmarks

Use the public command for a quick directional measurement.

```bash
treeman benchmark list --warmup 3 --runs 10
```

Benchmark an exact remote branch or PR/MR. The branch benchmark measures the
exact remote-branch lookup, fetch, and worktree creation; the review benchmark
also measures forge resolution. Both skip project setup (environment files,
dependencies, databases, and hooks), then force-remove the created worktree and
local branch outside each timed iteration. Destructive iterations run in a
temporary clone of `origin`, leaving the current repository's refs, `FETCH_HEAD`,
branches, and worktrees unchanged.

```bash
treeman benchmark branch feature/login --warmup 3 --runs 10
treeman benchmark review 123 --warmup 3 --runs 10
```

The branch benchmark refuses a target that already exists locally or has an
existing worktree. Review benchmarks leave existing PR/MR branches untouched.
If cleanup fails, stop and resolve the reported Git error before re-running.

Measure how long the interactive commands need to load every available picker
result without opening `fzf` or changing the repository.

```bash
treeman benchmark branch-results --warmup 3 --runs 10
treeman benchmark review-results --warmup 3 --runs 10
```

`branch-results` measures forge detection, all branch and open-review API
pages, local-branch filtering, PR/MR association, and complete picker-row
rendering. `review-results` measures forge detection, all open-review API
pages, and complete picker-row rendering. Both report result counts and flag
count changes between timed runs.

Install [hyperfine](https://github.com/sharkdp/hyperfine) to run the repeatable development benchmark.

```bash
make benchmark-list
```

The target builds `./bin/treeman` and runs `hyperfine --warmup 3 --runs 10 './bin/treeman list'`.

## Static Checks

Run Go static checks.

```bash
go vet ./...
```

## Smoke Test

Build the binary first.

```bash
make build
./smoke-test.sh
```

Set `TREEMAN_BIN` to test another executable.

```bash
TREEMAN_BIN=/path/to/treeman ./smoke-test.sh
```

The smoke test creates temporary Git repositories. It uses mock `gh`, `glab`, and `fzf` programs. It tests installation, uninstallation, create, review, branch, switch, delete, and unit tests.

The GitHub [CI workflow](ci.md) runs formatting, static checks, unit tests, a build, and the smoke test on pull requests.
