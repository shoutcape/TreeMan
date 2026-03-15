// Package deps handles dependency detection and installation for worktrees.
//
// This package is intentionally thin — the actual logic lives in
// internal/git.InstallDeps. This package exists as a namespace for
// future expansion (e.g. caching, parallel installs).
package deps
