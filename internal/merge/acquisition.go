package merge

import (
	"fmt"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/shoutcape/treeman/internal/forge"
	"github.com/shoutcape/treeman/internal/git"
)

const forgeVerificationWorkers = 4

type forgeProvider interface {
	observe(string, []Candidate) forgeObservation
	verify(string, []Candidate) forgeVerification
	unavailableDiagnostic() *Diagnostic
}

type forgeObservation struct {
	defaultSHA  string
	candidates  []forgeCandidateObservation
	diagnostics []Diagnostic
}

type forgeCandidateObservation struct {
	remote      RemoteState
	merge       MergeState
	mergedHeads []string
}

type forgeVerification struct {
	results     []verificationResult
	diagnostics []Diagnostic
}

// gitAcquirer contains the Git observations used to establish mandatory merge
// evidence. Forge failures must not weaken these checks.
type gitAcquirer struct {
	branchSHAs        func([]string) (map[string]string, error)
	remoteHeads       func([]string) (map[string]string, error)
	remoteTrackingSHA func(string) (string, bool, error)
	fetch             func(string) error
	mergedBranches    func(string) (map[string]string, error)
	anyAncestor       func([]string, string) (bool, error)
}

// forgeAcquirer resolves one applicable forge and exposes its optional
// verification operations. The selected provider controls which operation is
// used for the entire classification.
type forgeAcquirer struct {
	originRemoteURL   func() (string, error)
	resolveForge      func(string) (forge.Type, string, string, error)
	lookPath          func(string) (string, error)
	githubSnapshot    func(string, string, []forge.SnapshotCandidate) (forge.GitHubSnapshot, error)
	gitlabMergedHeads func(string, string, string, []forge.SnapshotCandidate) (map[string]bool, error)
	mergedPRHead      func(forge.Type, string, string, string, string, string) (bool, error)
}

type acquirer struct {
	git   gitAcquirer
	forge forgeAcquirer
}

func productionAcquirer() acquirer {
	return acquirer{
		git: gitAcquirer{
			branchSHAs:        git.BranchSHAs,
			remoteHeads:       git.RemoteHeads,
			remoteTrackingSHA: git.RemoteTrackingBranchSHA,
			fetch:             git.Fetch,
			mergedBranches:    git.MergedBranches,
			anyAncestor:       git.AnyCommitIsAncestor,
		},
		forge: forgeAcquirer{
			originRemoteURL:   git.OriginRemoteURL,
			resolveForge:      forge.ResolveFromRemote,
			lookPath:          exec.LookPath,
			githubSnapshot:    forge.GitHubCompleteSnapshot,
			gitlabMergedHeads: forge.GitLabMergedHeads,
			mergedPRHead:      forge.MergedPRHead,
		},
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
	return Result{Merged: Merged(snapshot), Cleanable: Cleanable(snapshot), Diagnostics: diagnostics}, nil
}

func (a acquirer) snapshotCandidates(branches []string) ([]Candidate, error) {
	tips, err := a.git.branchSHAs(branches)
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

func (a acquirer) selectProvider() forgeProvider {
	remoteURL, err := a.forge.originRemoteURL()
	if err != nil {
		return unavailableForgeProvider{message: fmt.Sprintf("could not read origin remote: %v", err)}
	}
	forgeType, repo, host, err := a.forge.resolveForge(remoteURL)
	if err != nil {
		return unavailableForgeProvider{message: fmt.Sprintf("origin forge is unavailable: %v", err)}
	}
	if _, err := a.forge.lookPath(forge.CLITool(forgeType)); err != nil {
		return unavailableForgeProvider{message: fmt.Sprintf("%s not found: cannot verify merged PRs/MRs for branches deleted on %s", forge.CLITool(forgeType), forgeType)}
	}
	switch forgeType {
	case forge.GitHub:
		return githubProvider{repo: repo, host: host, snapshot: a.forge.githubSnapshot, mergedPRHead: a.forge.mergedPRHead}
	case forge.GitLab:
		return gitlabProvider{repo: repo, host: host, mergedHeads: a.forge.gitlabMergedHeads, mergedPRHead: a.forge.mergedPRHead}
	default:
		return unavailableForgeProvider{message: fmt.Sprintf("unsupported forge %s", forgeType)}
	}
}

// acquire gathers fresh Git and forge observations into a snapshot. Failures
// that prevent a fresh default-branch view are fatal; forge-only failures are
// returned as warnings and leave merge evidence unknown.
func (a acquirer) acquire(defaultBranch string, candidates []Candidate) (Snapshot, []Diagnostic, error) {
	snapshot := Snapshot{Candidates: make([]Evidence, len(candidates))}
	for index, candidate := range candidates {
		snapshot.Candidates[index] = Evidence{Candidate: candidate, Remote: RemoteUnknown, Ancestor: AncestorUnknown, Merge: MergeUnknown, Tip: TipStable}
	}

	provider := a.selectProvider()
	observation := provider.observe(defaultBranch, candidates)
	deferredWarnings := observation.diagnostics
	if observation.defaultSHA != "" {
		snapshot.DefaultSHA = observation.defaultSHA
		for index, candidate := range observation.candidates {
			snapshot.Candidates[index].Remote = candidate.remote
			snapshot.Candidates[index].Merge = candidate.merge
		}
		deferredWarnings = append(deferredWarnings, a.markMergedDescendants(&snapshot, observation.candidates)...)
	}
	if snapshot.DefaultSHA == "" {
		heads, err := a.git.remoteHeads(append([]string{defaultBranch}, candidateBranches(candidates)...))
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

	ancestors, err := a.git.mergedBranches("origin/" + defaultBranch)
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
		return snapshot, deferredWarnings, nil
	}

	branches := make([]string, len(remoteGone))
	for index, candidateIndex := range remoteGone {
		branches[index] = snapshot.Candidates[candidateIndex].Candidate.Branch
	}
	tips, err := a.git.branchSHAs(branches)
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
		return snapshot, deferredWarnings, nil
	}
	if diagnostic := provider.unavailableDiagnostic(); diagnostic != nil {
		deferredWarnings = append(deferredWarnings, *diagnostic)
		return snapshot, deferredWarnings, nil
	}
	verification := provider.verify(defaultBranch, evidenceCandidates(snapshot, verify))
	deferredWarnings = append(deferredWarnings, verification.diagnostics...)
	for resultIndex, result := range verification.results {
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

func (a acquirer) markMergedDescendants(snapshot *Snapshot, candidates []forgeCandidateObservation) []Diagnostic {
	diagnostics := make([]Diagnostic, 0)
	for index, candidate := range candidates {
		evidence := &snapshot.Candidates[index]
		if evidence.Merge != MergeNo || len(candidate.mergedHeads) == 0 {
			continue
		}
		ancestor, err := a.git.anyAncestor(candidate.mergedHeads, evidence.Candidate.SHA)
		if err != nil {
			evidence.Merge = MergeUnknown
			diagnostics = append(diagnostics, Diagnostic{Operation: fmt.Sprintf("merge ancestry check for %q failed", evidence.Candidate.Branch), Err: err})
			continue
		}
		if ancestor {
			evidence.Merge = MergeAncestor
		}
	}
	return diagnostics
}

func (a acquirer) refreshDefaultBranch(defaultBranch, expectedSHA string) error {
	localDefaultSHA, exists, err := a.git.remoteTrackingSHA(defaultBranch)
	if err != nil {
		return err
	}
	if exists && localDefaultSHA == expectedSHA {
		return nil
	}
	if err := a.git.fetch("refs/heads/" + defaultBranch + ":refs/remotes/origin/" + defaultBranch); err != nil {
		return fmt.Errorf("could not fetch origin/%s: %w", defaultBranch, err)
	}
	fetchedSHA, exists, err := a.git.remoteTrackingSHA(defaultBranch)
	if err != nil {
		return err
	}
	if !exists || fetchedSHA != expectedSHA {
		return fmt.Errorf("origin/%s changed while refreshing merge state", defaultBranch)
	}
	return nil
}

func normalizeGitHubSnapshot(candidates []Candidate, result forge.GitHubSnapshot) (forgeObservation, error) {
	if len(result.Branches) != len(candidates) {
		return forgeObservation{}, fmt.Errorf("GitHub snapshot returned %d branches for %d candidates", len(result.Branches), len(candidates))
	}
	observation := forgeObservation{defaultSHA: result.DefaultSHA, candidates: make([]forgeCandidateObservation, len(candidates))}
	for index, branch := range result.Branches {
		candidate := candidates[index]
		if branch.Candidate != (forge.SnapshotCandidate{Branch: candidate.Branch, SHA: candidate.SHA}) {
			return forgeObservation{}, fmt.Errorf("GitHub snapshot returned mismatched candidate %q", branch.Candidate.Branch)
		}
		observation.candidates[index].remote = RemoteAbsent
		if branch.RemoteExists {
			observation.candidates[index].remote = RemotePresent
		}
		switch branch.Verification {
		case forge.SnapshotNotMerged:
			observation.candidates[index].merge = MergeNo
		case forge.SnapshotMerged:
			observation.candidates[index].merge = MergeYes
		case forge.SnapshotNeedsFallback:
		default:
			return forgeObservation{}, fmt.Errorf("GitHub snapshot returned invalid verification for %q", candidate.Branch)
		}
		observation.candidates[index].mergedHeads = branch.MergedHeads
	}
	return observation, nil
}

type unavailableForgeProvider struct{ message string }

func (p unavailableForgeProvider) observe(string, []Candidate) forgeObservation {
	return forgeObservation{}
}

func (p unavailableForgeProvider) verify(string, []Candidate) forgeVerification {
	return forgeVerification{}
}

func (p unavailableForgeProvider) unavailableDiagnostic() *Diagnostic {
	if p.message == "" {
		return nil
	}
	return &Diagnostic{Operation: "forge merge verification unavailable", Err: fmt.Errorf("%s", p.message)}
}

type githubProvider struct {
	repo         string
	host         string
	snapshot     func(string, string, []forge.SnapshotCandidate) (forge.GitHubSnapshot, error)
	mergedPRHead func(forge.Type, string, string, string, string, string) (bool, error)
}

func (p githubProvider) observe(defaultBranch string, candidates []Candidate) forgeObservation {
	snapshot, err := p.snapshot(p.repo, defaultBranch, forgeCandidates(candidates))
	if err != nil {
		return forgeObservation{diagnostics: []Diagnostic{{Operation: "GitHub snapshot failed", Err: err}}}
	}
	observation, err := normalizeGitHubSnapshot(candidates, snapshot)
	if err != nil {
		return forgeObservation{diagnostics: []Diagnostic{{Operation: "GitHub snapshot failed", Err: err}}}
	}
	return observation
}

func (githubProvider) unavailableDiagnostic() *Diagnostic { return nil }

type verificationResult struct {
	merged bool
	err    error
}

func (p githubProvider) verify(defaultBranch string, candidates []Candidate) forgeVerification {
	return forgeVerification{results: verifyMerged(p.mergedPRHead, forge.GitHub, p.repo, p.host, defaultBranch, candidates)}
}

type gitlabProvider struct {
	repo         string
	host         string
	mergedHeads  func(string, string, string, []forge.SnapshotCandidate) (map[string]bool, error)
	mergedPRHead func(forge.Type, string, string, string, string, string) (bool, error)
}

func (gitlabProvider) observe(string, []Candidate) forgeObservation { return forgeObservation{} }

func (p gitlabProvider) unavailableDiagnostic() *Diagnostic { return nil }

func (p gitlabProvider) verify(defaultBranch string, candidates []Candidate) forgeVerification {
	matched, err := p.mergedHeads(p.repo, p.host, defaultBranch, forgeCandidates(candidates))
	if err != nil {
		return forgeVerification{
			results:     verifyMerged(p.mergedPRHead, forge.GitLab, p.repo, p.host, defaultBranch, candidates),
			diagnostics: []Diagnostic{{Operation: "GitLab merge verification failed", Err: err}},
		}
	}
	results := make([]verificationResult, len(candidates))
	for index, candidate := range candidates {
		results[index].merged = matched[candidate.Branch]
	}
	return forgeVerification{results: results}
}

func evidenceCandidates(snapshot Snapshot, indexes []int) []Candidate {
	result := make([]Candidate, len(indexes))
	for index, candidateIndex := range indexes {
		result[index] = snapshot.Candidates[candidateIndex].Candidate
	}
	return result
}

func verifyMerged(mergedPRHead func(forge.Type, string, string, string, string, string) (bool, error), forgeType forge.Type, repo, host, defaultBranch string, candidates []Candidate) []verificationResult {
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
				merged, err := mergedPRHead(forgeType, repo, host, defaultBranch, candidate.Branch, candidate.SHA)
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
