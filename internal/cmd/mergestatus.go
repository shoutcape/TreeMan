package cmd

import (
	"fmt"
	"os/exec"
	"slices"

	"github.com/shoutcape/treeman/internal/forge"
	"github.com/shoutcape/treeman/internal/git"
)

// forgeMergedLookup returns a verifier that reports the head SHAs of merged
// PRs/MRs sourced from a branch on the origin's forge, plus an optional
// warning describing why a detected forge could not be queried (e.g. missing
// CLI). A nil verifier means verification is unavailable; callers must then
// treat remote-gone branches as not cleanable. Overridden in tests.
var forgeMergedLookup = defaultForgeMergedLookup

func defaultForgeMergedLookup() (func(branch string) ([]string, error), string) {
	remoteURL, err := git.OriginRemoteURL()
	if err != nil {
		return nil, ""
	}
	forgeType, repoSlug, host, err := forge.ResolveFromRemote(remoteURL)
	if err != nil {
		return nil, ""
	}
	cliTool := forge.CLITool(forgeType)
	if _, err := exec.LookPath(cliTool); err != nil {
		return nil, fmt.Sprintf("%s not found: cannot verify merged PRs/MRs for branches deleted on %s", cliTool, forgeType)
	}
	return func(branch string) ([]string, error) {
		return forge.MergedPRHeadSHAs(forgeType, repoSlug, host, branch)
	}, ""
}

// classifyCleanable determines the verified SHA for each branch eligible for
// merge cleanup relative to target (e.g. "origin/main").
//
// A branch qualifies when either:
//   - its tip is an ancestor of target (literal merge), or
//   - its counterpart on origin is gone, the forge reports a merged PR/MR
//     sourced from it (squash or rebase merge), and the branch's local tip
//     equals one of those merged head SHAs. Commits added after a merge, or
//     reused branch names, never match and are retained.
//
// Branches that cannot be verified are never cleanable. When origin cannot be
// tied to a supported forge, verification is skipped silently; when the forge
// is detected but lookup fails, the returned warning explains the gap so
// commands can surface why candidate lists may be incomplete.
func classifyCleanable(target string, branches []string) (verified map[string]string, warning string, err error) {
	verified = make(map[string]string, len(branches))
	ancestors, err := git.MergedBranches(target)
	if err != nil {
		return nil, "", err
	}
	for _, branch := range branches {
		if sha := ancestors[branch]; branch != "" && sha != "" {
			verified[branch] = sha
		}
	}

	var unverified []string
	for _, branch := range branches {
		if branch != "" && ancestors[branch] == "" {
			unverified = append(unverified, branch)
		}
	}
	if len(unverified) == 0 {
		return verified, warning, nil
	}

	exists, err := git.RemoteBranchesExist(unverified)
	if err != nil {
		return verified, joinWarning(warning, fmt.Sprintf("could not query remote branches: %v", err)), nil
	}
	var remoteGone []string
	for _, branch := range unverified {
		if !exists[branch] {
			remoteGone = append(remoteGone, branch)
		}
	}
	if len(remoteGone) == 0 {
		return verified, warning, nil
	}

	verify, lookupWarning := forgeMergedLookup()
	if verify == nil {
		return verified, joinWarning(warning, lookupWarning), nil
	}
	for _, branch := range remoteGone {
		// Only the exact commits that the forge merged may be discarded: the
		// local tip must equal a merged PR/MR head SHA. Commits added after
		// the merge, or reused branch names, never match and are retained.
		tip, err := git.BranchSHA(branch)
		if err != nil {
			warning = joinWarning(warning, fmt.Sprintf("could not resolve local branch %q: %v", branch, err))
			continue
		}
		headSHAs, err := verify(branch)
		if err != nil {
			warning = joinWarning(warning, fmt.Sprintf("merge verification failed: %v", err))
			continue
		}
		if slices.Contains(headSHAs, tip) {
			verified[branch] = tip
		}
	}
	return verified, warning, nil
}

func joinWarning(existing, next string) string {
	if next == "" {
		return existing
	}
	if existing == "" {
		return next
	}
	return existing + "; " + next
}
