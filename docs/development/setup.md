# Development Setup

## Requirements

Install Go 1.23 or later. Install Git. Install `make` for Makefile targets.

Clone the repository. Run these commands from its root directory.

```bash
make build
make test
make lint
```

`make build` writes `bin/treeman`. It adds version, commit, and UTC build date through linker flags.

## Make Targets

| Target | Action |
| --- | --- |
| `make build` | Build `bin/treeman` |
| `make test` | Run `go test ./...` |
| `make lint` | Run `go vet ./...` |
| `make tidy` | Run `go mod tidy` and `go mod verify` |
| `make install` | Install to `GOPATH/bin` |
| `make clean` | Remove `bin/` |
| `make help` | List targets |

## Source Layout

The native binary entry point is `cmd/treeman`. Command orchestration is in `internal/cmd`. Read [Package Map](../architecture/packages.md).

Keep new side effects in focused internal packages. Keep `internal/cmd` responsible for command order and user messages.

## Documentation Changes

Follow [Writing Standard](../writing-standard.md) and [Terminology](../terminology.md). Update the command reference when a command, flag, output rule, or cancellation rule changes.
