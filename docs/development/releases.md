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

## Automatic Releases

Merging a pull request into `main` automatically publishes a release. The workflow finds the latest `vMAJOR.MINOR.PATCH` tag, increments its patch number, creates the new tag on the merge commit, and runs GoReleaser.

For example, the release after `v0.3.0` is `v0.3.1`.

CI runs the test, lint, build, and smoke-test checks before a pull request can be merged. After the release workflow completes, download and test the relevant archive if needed.

## Manual Releases

Pushing a `v*` tag still starts the release workflow. Use this to publish an explicitly chosen version, such as a minor or major release.

The release workflow uses GoReleaser v2 and `GITHUB_TOKEN`.

GoReleaser excludes `docs:`, `test:`, `chore:`, and merge commits from the generated changelog.
