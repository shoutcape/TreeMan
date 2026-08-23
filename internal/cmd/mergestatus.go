package cmd

import (
	"fmt"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/shoutcape/treeman/internal/forge"
	"github.com/shoutcape/treeman/internal/git"
)

const forgeVerificationWorkers = 4

// forgeMergeVerifier checks whether branch at sha was merged. It can be
// invoked concurrently by up to forgeVerificationWorkers goroutines.
type forgeMergeVerifier func(branch, sha string) (bool, error)

// forgeMergedLookup returns nil, nil when verification is unavailable without
// warning, or nil, error when a detected forge cannot be queried. Overridden
// in tests.
var forgeMergedLookup = defaultForgeMergedLookup

func defaultForgeMergedLookup(defaultBranch string) (forgeMergeVerifier, error) {
	remoteURL, err := git.OriginRemoteURL()
	if err != nil {
		return nil, nil
	}
	forgeType, repoSlug, host, err := forge.ResolveFromRemote(remoteURL)
	if err != nil {
		return nil, nil
	}
	cliTool := forge.CLITool(forgeType)
	if _, err := exec.LookPath(cliTool); err != nil {
		return nil, fmt.Errorf("%s not found: cannot verify merged PRs/MRs for branches deleted on %s", cliTool, forgeType)
	}
	return func(branch, sha string) (bool, error) {
		return forge.MergedPRHead(forgeType, repoSlug, host, defaultBranch, branch, sha)
	}, nil
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
func classifyCleanable(target, defaultBranch string, branches []string) (verified map[string]string, warning string, err error) {
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

	type candidate struct {
		branch string
		sha    string
	}
	candidates := make([]candidate, 0, len(remoteGone))
	for _, branch := range remoteGone {
		tip, err := git.BranchSHA(branch)
		if err != nil {
			warning = joinWarning(warning, fmt.Sprintf("could not resolve local branch %q: %v", branch, err))
			continue
		}
		candidates = append(candidates, candidate{branch: branch, sha: tip})
	}
	if len(candidates) == 0 {
		return verified, warning, nil
	}

	verify, lookupErr := forgeMergedLookup(defaultBranch)
	if lookupErr != nil {
		warning = joinWarning(warning, lookupErr.Error())
		return verified, warning, nil
	}
	if verify == nil {
		return verified, warning, nil
	}

	type result struct {
		merged bool
		err    error
	}
	completed := make([]result, len(candidates))
	workers := min(forgeVerificationWorkers, len(candidates))
	var next atomic.Int64
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for {
				index := int(next.Add(1) - 1)
				if index >= len(candidates) {
					return
				}
				merged, err := verify(candidates[index].branch, candidates[index].sha)
				completed[index] = result{merged: merged, err: err}
			}
		}()
	}
	wait.Wait()
	for index, candidate := range candidates {
		result := completed[index]
		if result.err != nil {
			warning = joinWarning(warning, fmt.Sprintf("merge verification for %q failed: %v", candidate.branch, result.err))
			continue
		}
		if result.merged {
			verified[candidate.branch] = candidate.sha
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
