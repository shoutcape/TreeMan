# Testing

## Unit Tests

Run all unit tests.

```bash
go test ./...
```

Tests cover command helpers, configuration, database logic, dependency detection, environment files, forge APIs, Git calls, hooks, remote parsing, validation, and worktree rules.

## List Benchmark

Install [hyperfine](https://github.com/sharkdp/hyperfine), then build and benchmark `treeman list`.

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
