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

type mergeEvidence uint8

const (
	mergeEvidenceUnknown mergeEvidence = iota
	mergeEvidenceNotMerged
	mergeEvidenceMerged
)

type branchMergeState struct {
	sha          string
	remoteExists bool
	evidence     mergeEvidence
}

type mergeState struct {
	defaultSHA string
	branches   map[string]branchMergeState
}

// forgeMergedLookup returns nil, nil when verification is unavailable without
// warning, or nil, error when a detected forge cannot be queried. Overridden
// in tests.
var forgeMergedLookup = defaultForgeMergedLookup

// githubSnapshotLookup returns a typed fresh-state snapshot for a supported
// GitHub repository. It returns false when callers should use Git instead.
var githubSnapshotLookup = defaultGitHubSnapshotLookup

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
func defaultGitHubSnapshotLookup(defaultBranch string, candidates []forge.SnapshotCandidate) (forge.GitHubSnapshot, bool) {
	remoteURL, err := git.OriginRemoteURL()
	if err != nil {
		return forge.GitHubSnapshot{}, false
	}
	forgeType, repoSlug, _, err := forge.ResolveFromRemote(remoteURL)
	if err != nil || forgeType != forge.GitHub {
		return forge.GitHubSnapshot{}, false
	}
	if _, err := exec.LookPath(forge.CLITool(forgeType)); err != nil {
		return forge.GitHubSnapshot{}, false
	}
	snapshot, err := forge.GitHubRemoteSnapshot(repoSlug, defaultBranch, candidates)
	if err != nil {
		return forge.GitHubSnapshot{}, false
	}
	return snapshot, true
}

func refreshMergeState(defaultBranch string, branches []string) (mergeState, error) {
	candidates, err := snapshotCandidates(branches)
	if err != nil {
		return mergeState{}, err
	}
	snapshot, snapshotOK := githubSnapshotLookup(defaultBranch, candidates)
	var state mergeState
	if snapshotOK {
		state, err = mergeStateFromGitHubSnapshot(candidates, snapshot)
		if err != nil {
			return mergeState{}, err
		}
	} else {
		queryBranches := make([]string, 0, len(candidates)+1)
		queryBranches = append(queryBranches, defaultBranch)
		for _, candidate := range candidates {
			queryBranches = append(queryBranches, candidate.Branch)
		}
		heads, err := git.RemoteHeads(queryBranches)
		if err != nil {
			return mergeState{}, err
		}
		state = mergeStateFromRemoteHeads(defaultBranch, candidates, heads)
	}
	if state.defaultSHA == "" {
		return mergeState{}, fmt.Errorf("origin does not have default branch %q", defaultBranch)
	}
	localDefaultSHA, exists, err := git.RemoteTrackingBranchSHA(defaultBranch)
	if err != nil {
		return mergeState{}, err
	}
	if !exists || localDefaultSHA != state.defaultSHA {
		if err := git.Fetch("refs/heads/" + defaultBranch + ":refs/remotes/origin/" + defaultBranch); err != nil {
			return mergeState{}, fmt.Errorf("could not fetch origin/%s: %w", defaultBranch, err)
		}
		fetchedSHA, exists, err := git.RemoteTrackingBranchSHA(defaultBranch)
		if err != nil {
			return mergeState{}, err
		}
		if !exists || fetchedSHA != state.defaultSHA {
			return mergeState{}, fmt.Errorf("origin/%s changed while refreshing merge state", defaultBranch)
		}
	}
	return state, nil
}

func snapshotCandidates(branches []string) ([]forge.SnapshotCandidate, error) {
	tips, err := git.BranchSHAs(branches)
	if err != nil {
		return nil, err
	}
	candidates := make([]forge.SnapshotCandidate, 0, len(branches))
	seen := make(map[string]struct{}, len(branches))
	for _, branch := range branches {
		if branch == "" {
			continue
		}
		if _, duplicate := seen[branch]; duplicate {
			return nil, fmt.Errorf("duplicate local branch %q", branch)
		}
		seen[branch] = struct{}{}
		sha := tips[branch]
		if sha == "" {
			return nil, fmt.Errorf("could not resolve local branch %q", branch)
		}
		candidates = append(candidates, forge.SnapshotCandidate{Branch: branch, SHA: sha})
	}
	return candidates, nil
}

func mergeStateFromRemoteHeads(defaultBranch string, candidates []forge.SnapshotCandidate, heads map[string]string) mergeState {
	branches := make(map[string]branchMergeState, len(candidates))
	for _, candidate := range candidates {
		branches[candidate.Branch] = branchMergeState{
			sha:          candidate.SHA,
			remoteExists: heads[candidate.Branch] != "",
		}
	}
	return mergeState{defaultSHA: heads[defaultBranch], branches: branches}
}

func mergeStateFromGitHubSnapshot(candidates []forge.SnapshotCandidate, snapshot forge.GitHubSnapshot) (mergeState, error) {
	if len(snapshot.Branches) != len(candidates) {
		return mergeState{}, fmt.Errorf("GitHub snapshot returned %d branches for %d candidates", len(snapshot.Branches), len(candidates))
	}
	branches := make(map[string]branchMergeState, len(candidates))
	for index, candidate := range candidates {
		result := snapshot.Branches[index]
		if result.Candidate != candidate {
			return mergeState{}, fmt.Errorf("GitHub snapshot returned mismatched candidate %q", result.Candidate.Branch)
		}
		evidence := mergeEvidenceUnknown
		switch result.Verification {
		case forge.SnapshotNotMerged:
			evidence = mergeEvidenceNotMerged
		case forge.SnapshotMerged:
			evidence = mergeEvidenceMerged
		case forge.SnapshotNeedsFallback:
		default:
			return mergeState{}, fmt.Errorf("GitHub snapshot returned invalid verification for %q", candidate.Branch)
		}
		branches[candidate.Branch] = branchMergeState{
			sha:          candidate.SHA,
			remoteExists: result.RemoteExists,
			evidence:     evidence,
		}
	}
	return mergeState{defaultSHA: snapshot.DefaultSHA, branches: branches}, nil
}

func classifyCleanable(target, defaultBranch string, branches []string, state mergeState) (verified map[string]string, warning string, err error) {
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

	var remoteGone []string
	for _, branch := range unverified {
		branchState, known := state.branches[branch]
		if !known {
			return nil, warning, fmt.Errorf("remote state for branch %q is unknown", branch)
		}
		if !branchState.remoteExists {
			remoteGone = append(remoteGone, branch)
		}
	}
	if len(remoteGone) == 0 {
		return verified, warning, nil
	}

	tips, err := git.BranchSHAs(remoteGone)
	if err != nil {
		return nil, warning, err
	}
	candidates := make([]forge.SnapshotCandidate, 0, len(remoteGone))
	for _, branch := range remoteGone {
		tip := tips[branch]
		if tip == "" {
			warning = joinWarning(warning, fmt.Sprintf("could not resolve local branch %q", branch))
			continue
		}
		branchState := state.branches[branch]
		if branchState.sha == tip {
			switch branchState.evidence {
			case mergeEvidenceMerged:
				verified[branch] = tip
				continue
			case mergeEvidenceNotMerged:
				continue
			}
		}
		candidates = append(candidates, forge.SnapshotCandidate{Branch: branch, SHA: tip})
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
				merged, err := verify(candidates[index].Branch, candidates[index].SHA)
				completed[index] = result{merged: merged, err: err}
			}
		}()
	}
	wait.Wait()
	for index, candidate := range candidates {
		result := completed[index]
		if result.err != nil {
			warning = joinWarning(warning, fmt.Sprintf("merge verification for %q failed: %v", candidate.Branch, result.err))
			continue
		}
		if result.merged {
			verified[candidate.Branch] = candidate.SHA
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
