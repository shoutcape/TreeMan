# Continuous Integration

GitHub Actions validates every pull request in `.github/workflows/ci.yml`.

## Checks

CI runs these commands on Ubuntu:

```bash
test -z "$(gofmt -l .)"
make lint
make test
make build
./smoke-test.sh
```

The workflow uses Go from `go.mod` and caches Go modules. It uses `actions/checkout@v5` and `actions/setup-go@v6`, which run on Node 24.

## Releases

The separate [release workflow](releases.md) runs when a pull request is merged into `main` or when a `v*` tag is pushed.
