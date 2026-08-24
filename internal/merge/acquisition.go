package merge

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/shoutcape/treeman/internal/forge"
	"github.com/shoutcape/treeman/internal/git"
)

const forgeVerificationWorkers = 4

type providerStatus uint8

const (
	providerNotApplicable providerStatus = iota
	providerAvailable
	providerUnavailable
)

// provider describes an adapter's applicability and availability separately.
// It is intentionally not an error state: Git evidence remains usable when a
// forge cannot be queried.
type provider struct {
	type_   forge.Type
	repo    string
	host    string
	status  providerStatus
	message string
}

type acquirer struct {
	branchSHAs        func([]string) (map[string]string, error)
	remoteHeads       func([]string) (map[string]string, error)
	remoteTrackingSHA func(string) (string, bool, error)
	fetch             func(string) error
	mergedBranches    func(string) (map[string]string, error)
	originRemoteURL   func() (string, error)
	resolveForge      func(string) (forge.Type, string, string, error)
	lookPath          func(string) (string, error)
	githubSnapshot    func(string, string, []forge.SnapshotCandidate) (forge.GitHubSnapshot, error)
	gitlabMergedHeads func(string, string, string, []forge.SnapshotCandidate) (map[string]bool, error)
	mergedPRHead      func(forge.Type, string, string, string, string, string) (bool, error)
}

func productionAcquirer() acquirer {
	return acquirer{
		branchSHAs:        git.BranchSHAs,
		remoteHeads:       git.RemoteHeads,
		remoteTrackingSHA: git.RemoteTrackingBranchSHA,
		fetch:             git.Fetch,
		mergedBranches:    git.MergedBranches,
		originRemoteURL:   git.OriginRemoteURL,
		resolveForge:      forge.ResolveFromRemote,
		lookPath:          exec.LookPath,
		githubSnapshot:    forge.GitHubCompleteSnapshot,
		gitlabMergedHeads: forge.GitLabMergedHeads,
		mergedPRHead:      forge.MergedPRHead,
	}
}

func NewClassifier() ClassifierFunc {
	return productionAcquirer().Classify
}

// Classify snapshots exact local branch tips, acquires fresh merge evidence,
// and applies the deletion policy.
func (a acquirer) Classify(defaultBranch string, branches []string) (Result, error) {
	candidates, err := a.snapshotCandidates(branches)
	if err != nil {
		return Result{}, err
	}
	if len(candidates) == 0 {
		return Result{}, nil
	}
	snapshot, diagnostics, err := a.acquire(defaultBranch, candidates)
	if err != nil {
		return Result{}, err
	}
	return Result{Cleanable: Cleanable(snapshot), Diagnostics: diagnostics}, nil
}

func (a acquirer) snapshotCandidates(branches []string) ([]Candidate, error) {
	tips, err := a.branchSHAs(branches)
	if err != nil {
		return nil, err
	}
	candidates := make([]Candidate, 0, len(branches))
	seen := make(map[string]struct{}, len(branches))
	for _, branch := range branches {
		if branch == "" {
			continue
		}
		if _, duplicate := seen[branch]; duplicate {
			return nil, fmt.Errorf("duplicate local branch %q", branch)
		}
		seen[branch] = struct{}{}
		if tips[branch] == "" {
			return nil, fmt.Errorf("could not resolve local branch %q", branch)
		}
		candidates = append(candidates, Candidate{Branch: branch, SHA: tips[branch]})
	}
	return candidates, nil
}

func (a acquirer) resolveProvider() provider {
	remoteURL, err := a.originRemoteURL()
	if err != nil {
		return provider{status: providerUnavailable, message: fmt.Sprintf("could not read origin remote: %v", err)}
	}
	forgeType, repo, host, err := a.resolveForge(remoteURL)
	if err != nil {
		return provider{status: providerNotApplicable, message: fmt.Sprintf("origin forge is unavailable: %v", err)}
	}
	if _, err := a.lookPath(forge.CLITool(forgeType)); err != nil {
		return provider{
			type_:   forgeType,
			repo:    repo,
			host:    host,
			status:  providerUnavailable,
			message: fmt.Sprintf("%s not found: cannot verify merged PRs/MRs for branches deleted on %s", forge.CLITool(forgeType), forgeType),
		}
	}
	return provider{type_: forgeType, repo: repo, host: host, status: providerAvailable}
}

// acquire gathers fresh Git and forge observations into a snapshot. Failures
// that prevent a fresh default-branch view are fatal; forge-only failures are
// returned as warnings and leave merge evidence unknown.
func (a acquirer) acquire(defaultBranch string, candidates []Candidate) (Snapshot, []Diagnostic, error) {
	snapshot := Snapshot{Candidates: make([]Evidence, len(candidates))}
	for index, candidate := range candidates {
		snapshot.Candidates[index] = Evidence{Candidate: candidate, Remote: RemoteUnknown, Ancestor: AncestorUnknown, Merge: MergeUnknown, Tip: TipStable}
	}

	provider := a.resolveProvider()
	var deferredWarnings []Diagnostic
	if provider.type_ == forge.GitHub && provider.status == providerAvailable {
		githubSnapshot, err := a.githubSnapshot(provider.repo, defaultBranch, forgeCandidates(candidates))
		if err != nil {
			if errors.Is(err, forge.ErrGitHubDefaultBranchChanged) {
				return Snapshot{}, nil, err
			}
			deferredWarnings = append(deferredWarnings, Diagnostic{Operation: "GitHub snapshot failed", Err: err})
		} else {
			githubState := Snapshot{Candidates: append([]Evidence(nil), snapshot.Candidates...)}
			if err := applyGitHubSnapshot(&githubState, githubSnapshot); err != nil {
				deferredWarnings = append(deferredWarnings, Diagnostic{Operation: "GitHub snapshot failed", Err: err})
			} else {
				snapshot = githubState
			}
		}
	}
	if snapshot.DefaultSHA == "" {
		heads, err := a.remoteHeads(append([]string{defaultBranch}, candidateBranches(candidates)...))
		if err != nil {
			return Snapshot{}, nil, err
		}
		snapshot.DefaultSHA = heads[defaultBranch]
		for index, candidate := range candidates {
			snapshot.Candidates[index].Remote = RemoteAbsent
			if heads[candidate.Branch] != "" {
				snapshot.Candidates[index].Remote = RemotePresent
			}
		}
	}
	if snapshot.DefaultSHA == "" {
		return Snapshot{}, nil, fmt.Errorf("origin does not have default branch %q", defaultBranch)
	}
	if err := a.refreshDefaultBranch(defaultBranch, snapshot.DefaultSHA); err != nil {
		return Snapshot{}, nil, err
	}

	ancestors, err := a.mergedBranches("origin/" + defaultBranch)
	if err != nil {
		return Snapshot{}, nil, err
	}
	remoteGone := make([]int, 0, len(snapshot.Candidates))
	for index := range snapshot.Candidates {
		candidate := &snapshot.Candidates[index]
		candidate.Ancestor = AncestorNo
		if ancestors[candidate.Candidate.Branch] == candidate.Candidate.SHA {
			candidate.Ancestor = AncestorYes
			continue
		}
		if candidate.Remote == RemoteAbsent {
			remoteGone = append(remoteGone, index)
		}
	}
	if len(remoteGone) == 0 {
		return snapshot, nil, nil
	}

	branches := make([]string, len(remoteGone))
	for index, candidateIndex := range remoteGone {
		branches[index] = snapshot.Candidates[candidateIndex].Candidate.Branch
	}
	tips, err := a.branchSHAs(branches)
	if err != nil {
		return Snapshot{}, nil, err
	}
	verify := make([]int, 0, len(remoteGone))
	for _, index := range remoteGone {
		candidate := &snapshot.Candidates[index]
		if tips[candidate.Candidate.Branch] != candidate.Candidate.SHA {
			candidate.Tip = TipChanged
			continue
		}
		if candidate.Merge == MergeUnknown {
			verify = append(verify, index)
		}
	}
	if len(verify) == 0 {
		return snapshot, nil, nil
	}
	if provider.status != providerAvailable {
		if provider.message != "" {
			deferredWarnings = append(deferredWarnings, Diagnostic{Operation: "forge merge verification unavailable", Err: fmt.Errorf("%s", provider.message)})
		}
		return snapshot, deferredWarnings, nil
	}
	if provider.type_ == forge.GitHub {
		verify = unresolvedRemoteGone(snapshot, verify)
	}
	if provider.type_ == forge.GitLab {
		if err := a.enrichGitLabBatch(&snapshot, verify, provider, defaultBranch); err != nil {
			deferredWarnings = append(deferredWarnings, Diagnostic{Operation: "GitLab merge verification failed", Err: err})
		} else {
			verify = unresolved(snapshot, verify)
		}
	}
	if len(verify) == 0 {
		return snapshot, deferredWarnings, nil
	}
	if provider.type_ != forge.GitHub && provider.type_ != forge.GitLab {
		return snapshot, deferredWarnings, nil
	}
	for resultIndex, result := range a.verifyMerged(provider, defaultBranch, evidenceCandidates(snapshot, verify)) {
		candidateIndex := verify[resultIndex]
		if result.err != nil {
			deferredWarnings = append(deferredWarnings, Diagnostic{Operation: fmt.Sprintf("merge verification for %q failed", snapshot.Candidates[candidateIndex].Candidate.Branch), Err: result.err})
			continue
		}
		if result.merged {
			snapshot.Candidates[candidateIndex].Merge = MergeYes
		} else {
			snapshot.Candidates[candidateIndex].Merge = MergeNo
		}
	}
	return snapshot, deferredWarnings, nil
}

func (a acquirer) refreshDefaultBranch(defaultBranch, expectedSHA string) error {
	localDefaultSHA, exists, err := a.remoteTrackingSHA(defaultBranch)
	if err != nil {
		return err
	}
	if exists && localDefaultSHA == expectedSHA {
		return nil
	}
	if err := a.fetch("refs/heads/" + defaultBranch + ":refs/remotes/origin/" + defaultBranch); err != nil {
		return fmt.Errorf("could not fetch origin/%s: %w", defaultBranch, err)
	}
	fetchedSHA, exists, err := a.remoteTrackingSHA(defaultBranch)
	if err != nil {
		return err
	}
	if !exists || fetchedSHA != expectedSHA {
		return fmt.Errorf("origin/%s changed while refreshing merge state", defaultBranch)
	}
	return nil
}

func applyGitHubSnapshot(snapshot *Snapshot, result forge.GitHubSnapshot) error {
	if len(result.Branches) != len(snapshot.Candidates) {
		return fmt.Errorf("GitHub snapshot returned %d branches for %d candidates", len(result.Branches), len(snapshot.Candidates))
	}
	for index, branch := range result.Branches {
		candidate := &snapshot.Candidates[index]
		if branch.Candidate != (forge.SnapshotCandidate{Branch: candidate.Candidate.Branch, SHA: candidate.Candidate.SHA}) {
			return fmt.Errorf("GitHub snapshot returned mismatched candidate %q", branch.Candidate.Branch)
		}
		candidate.Remote = RemoteAbsent
		if branch.RemoteExists {
			candidate.Remote = RemotePresent
		}
		switch branch.Verification {
		case forge.SnapshotNotMerged:
			candidate.Merge = MergeNo
		case forge.SnapshotMerged:
			candidate.Merge = MergeYes
		case forge.SnapshotNeedsFallback:
		default:
			return fmt.Errorf("GitHub snapshot returned invalid verification for %q", candidate.Candidate.Branch)
		}
	}
	snapshot.DefaultSHA = result.DefaultSHA
	return nil
}

func (a acquirer) enrichGitLabBatch(snapshot *Snapshot, indexes []int, provider provider, defaultBranch string) error {
	matched, err := a.gitlabMergedHeads(provider.repo, provider.host, defaultBranch, forgeCandidates(evidenceCandidates(*snapshot, indexes)))
	if err != nil {
		return err
	}
	for _, index := range indexes {
		if matched[snapshot.Candidates[index].Candidate.Branch] {
			snapshot.Candidates[index].Merge = MergeYes
		} else {
			snapshot.Candidates[index].Merge = MergeNo
		}
	}
	return nil
}

func unresolved(snapshot Snapshot, indexes []int) []int {
	result := make([]int, 0, len(indexes))
	for _, index := range indexes {
		if snapshot.Candidates[index].Merge == MergeUnknown {
			result = append(result, index)
		}
	}
	return result
}

func unresolvedRemoteGone(snapshot Snapshot, indexes []int) []int {
	result := make([]int, 0, len(indexes))
	for _, index := range indexes {
		if snapshot.Candidates[index].Remote == RemoteAbsent && snapshot.Candidates[index].Merge == MergeUnknown {
			result = append(result, index)
		}
	}
	return result
}

func evidenceCandidates(snapshot Snapshot, indexes []int) []Candidate {
	result := make([]Candidate, len(indexes))
	for index, candidateIndex := range indexes {
		result[index] = snapshot.Candidates[candidateIndex].Candidate
	}
	return result
}

type verificationResult struct {
	merged bool
	err    error
}

func (a acquirer) verifyMerged(provider provider, defaultBranch string, candidates []Candidate) []verificationResult {
	completed := make([]verificationResult, len(candidates))
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
				candidate := candidates[index]
				merged, err := a.mergedPRHead(provider.type_, provider.repo, provider.host, defaultBranch, candidate.Branch, candidate.SHA)
				completed[index] = verificationResult{merged: merged, err: err}
			}
		}()
	}
	wait.Wait()
	return completed
}

func forgeCandidates(candidates []Candidate) []forge.SnapshotCandidate {
	result := make([]forge.SnapshotCandidate, len(candidates))
	for index, candidate := range candidates {
		result[index] = forge.SnapshotCandidate{Branch: candidate.Branch, SHA: candidate.SHA}
	}
	return result
}

func candidateBranches(candidates []Candidate) []string {
	branches := make([]string, len(candidates))
	for index, candidate := range candidates {
		branches[index] = candidate.Branch
	}
	return branches
}
