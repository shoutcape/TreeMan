# Releases

TreeMan uses GoReleaser and GitHub Actions for release publishing.

## Build Matrix

GoReleaser builds static binaries for these targets:

| Operating system | Processor |
| --- | --- |
| Linux | amd64, arm64 |
| macOS | amd64, arm64 |

Archives use this name format:

```text
treeman_<os>_<arch>.tar.gz
```

GoReleaser creates `treeman_checksums.txt` with SHA-256 checksums.

## Version Data

The Makefile gets version data from Git. It puts version, short commit, and UTC build date into `main` linker variables.

GoReleaser uses release version, commit, and date values for the same variables.

## Publish a Release

1. Run `make test`.
2. Run `make lint`.
3. Run `make build`.
4. Run `./smoke-test.sh`.
5. Create and push a tag that starts with `v`.
6. Check the GitHub Actions release result.
7. Download and test each release archive.

The release workflow runs only for pushed `v*` tags. It uses GoReleaser v2 and `GITHUB_TOKEN`.

GoReleaser excludes `docs:`, `test:`, `chore:`, and merge commits from the generated changelog.
