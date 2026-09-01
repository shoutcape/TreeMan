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

Measure normal worktree and branch deletion with a freshly prepared disposable
treebranch for every iteration.

```bash
treeman benchmark delete --warmup 1 --runs 5
```

The delete benchmark takes no branch argument. Outside the timer, each iteration
creates `treeman-benchmark-delete`, copies `.env*` files, creates the configured
branch database, installs dependencies, and runs post-create hooks. Timing starts
immediately before the equivalent of
`treeman delete --path <path> --branch treeman-benchmark-delete --yes` and stops
when deletion returns. Verification that the worktree and local branch are gone
is also outside the timer. When database setup is configured, PostgreSQL and its
configured Docker container must already be running.

Iterations run in a temporary clone of the repository's own main worktree, not of
`origin`, so the target works in any repository on disk: one with no remote, one
whose remote is unreachable, and one large enough that cloning it over the network
would cost more than the benchmark.

Every prepared worktree is reported, and the line repeats whenever it changes.

```text
prepared: environment completed, dependencies completed, database completed, hooks completed
```

The timed deletion includes the fetch that refreshes the branch's remote-tracking
ref before the merge check. In the benchmark that fetch is served by the local
clone the sandbox was made from, so it costs a few milliseconds of disk work
rather than the network round trip a deletion in a real checkout pays. Treat the
reported number as the local floor for a deletion, not as what a user waits for
against a remote.

Read the number next to that line, never on its own: what a deletion costs is
decided by what setup put in the worktree, so two repositories are only
comparable when they were prepared the same way. The same line reports untracked
setup output the project does not ignore, as `cleared N untracked setup path(s)`.
That output has to go before the timed, non-forced deletion can run, which leaves
that deletion less to remove than the project itself would.

A setup step that fails aborts the benchmark rather than measuring an incomplete
worktree, and the error names the flag that skips it. Use those flags where a
step cannot run on this machine -- a dependency install that needs credentials,
a database whose container is not running, a hook that needs a service.

```bash
treeman benchmark delete --warmup 1 --runs 5 --skip-deps
```

Measure how long the interactive commands need to load every available picker
result without opening `fzf` or changing the repository.

```bash
treeman benchmark branch-results --warmup 3 --runs 10
treeman benchmark review-results --warmup 3 --runs 10
```

`branch-results` measures forge detection, the branch preview query, all
branch batches or records, local-branch filtering, per-branch PR/MR association,
and complete picker-row rendering. `review-results` measures forge detection,
all open-review batches or records, and complete picker-row rendering. Both report result
counts and flag count changes between timed runs.

Both targets also report the time to the first picker row: how long after
forge detection starts the producer hands its first rendered row to the picker
writer. Both targets start that timer at the same point, and neither runs
`fzf`, so the number is producer row-ready latency rather than a measurement of
what `fzf` painted. Use it to compare what the user waits for before the first
result: both targets stream, so the first row comes seconds before the last.

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

The mock `fzf` reads the streamed list in one of two modes. By default it reads
the whole list before answering, which is what a user who waits for every row
does. With `FZF_MOCK_MODE=stream` it answers as soon as the wanted row arrives
and exits, leaving the rest unwritten — the picker closing under a running
producer. The branch picker is covered in both modes, because only the second
one exercises selecting an early row and cancelling the forge requests that
were still fetching the rest.

The GitHub [CI workflow](ci.md) runs formatting, static checks, unit tests, a build, and the smoke test on pull requests.
