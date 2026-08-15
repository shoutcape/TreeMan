# Testing

## Unit Tests

Run all unit tests.

```bash
go test ./...
```

Tests cover command helpers, configuration, database logic, dependency detection, environment files, forge APIs, Git calls, hooks, remote parsing, validation, and worktree rules.

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

The smoke test creates temporary Git repositories. It uses mock `gh`, `glab`, and `fzf` programs. It tests installation, uninstallation, lazygit setup, create, review, branch, switch, delete, and unit tests.

The GitHub release workflow does not run these checks on pull requests.
