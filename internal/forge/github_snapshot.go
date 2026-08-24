package forge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrGitHubDefaultBranchChanged means snapshot batches observed different
// default branch tips.
var ErrGitHubDefaultBranchChanged = errors.New("GitHub default branch changed during verification")

// githubSnapshotBatchSize keeps requests below GitHub's query and argument
// limits while retaining one request for ordinary worktree counts.
const githubSnapshotBatchSize = 40

// SnapshotCandidate identifies the exact local tip associated with a branch.
type SnapshotCandidate struct {
	Branch string
	SHA    string
}

// SnapshotVerification is GitHub's complete merge decision for a candidate.
// NeedsFallback means the REST verifier must check the candidate instead.
type SnapshotVerification uint8

const (
	SnapshotNotMerged SnapshotVerification = iota
	SnapshotMerged
	SnapshotNeedsFallback
)

// SnapshotBranch contains the remote and merge state for one exact candidate.
type SnapshotBranch struct {
	Candidate    SnapshotCandidate
	RemoteExists bool
	Verification SnapshotVerification
}

// GitHubSnapshot is a batch-consistent remote-state read. DefaultSHA is
// required; Branches retain candidate identity and ordering across batches.
type GitHubSnapshot struct {
	DefaultSHA string
	Branches   []SnapshotBranch
}

// GitHubCompleteSnapshot reads the default ref, candidate branch presence, and
// associated PR evidence together for each bounded batch.
func GitHubCompleteSnapshot(repoSlug, defaultBranch string, candidates []SnapshotCandidate) (GitHubSnapshot, error) {
	owner, name, err := githubSnapshotRepository(repoSlug, defaultBranch, candidates)
	if err != nil {
		return GitHubSnapshot{}, err
	}
	return githubSnapshotBatches(owner, name, defaultBranch, candidates, githubCompleteSnapshotBatch)
}

type githubSnapshotBatch func(string, string, string, []SnapshotCandidate) (GitHubSnapshot, error)

func githubSnapshotBatches(owner, name, defaultBranch string, candidates []SnapshotCandidate, query githubSnapshotBatch) (GitHubSnapshot, error) {
	snapshot := GitHubSnapshot{Branches: make([]SnapshotBranch, 0, len(candidates))}
	for start := 0; start < len(candidates) || start == 0; start += githubSnapshotBatchSize {
		end := min(start+githubSnapshotBatchSize, len(candidates))
		batch, err := query(owner, name, defaultBranch, candidates[start:end])
		if err != nil {
			return GitHubSnapshot{}, err
		}
		if snapshot.DefaultSHA != "" && snapshot.DefaultSHA != batch.DefaultSHA {
			return GitHubSnapshot{}, ErrGitHubDefaultBranchChanged
		}
		snapshot.DefaultSHA = batch.DefaultSHA
		snapshot.Branches = append(snapshot.Branches, batch.Branches...)
		if end == len(candidates) {
			break
		}
	}
	return snapshot, nil
}

func githubSnapshotRepository(repoSlug, defaultBranch string, candidates []SnapshotCandidate) (string, string, error) {
	owner, name, ok := strings.Cut(repoSlug, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", "", fmt.Errorf("invalid GitHub repository %q", repoSlug)
	}
	if defaultBranch == "" {
		return "", "", errors.New("GitHub default branch is required")
	}
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate.Branch == "" || candidate.SHA == "" {
			return "", "", errors.New("GitHub snapshot candidates require branch and SHA")
		}
		if _, duplicate := seen[candidate.Branch]; duplicate {
			return "", "", fmt.Errorf("duplicate GitHub snapshot branch %q", candidate.Branch)
		}
		seen[candidate.Branch] = struct{}{}
	}

	return owner, name, nil
}

type githubSnapshotSlot struct {
	candidate   SnapshotCandidate
	refAlias    string
	shaAlias    string
	commitAlias string
}

type githubCompleteSnapshotPlan struct {
	defaultRefAlias string
	slots           []githubSnapshotSlot
}

func newGitHubCompleteSnapshotPlan(candidates []SnapshotCandidate) githubCompleteSnapshotPlan {
	plan := githubCompleteSnapshotPlan{defaultRefAlias: "ref0", slots: make([]githubSnapshotSlot, len(candidates))}
	for index, candidate := range candidates {
		plan.slots[index] = githubSnapshotSlot{
			candidate:   candidate,
			refAlias:    fmt.Sprintf("ref%d", index+1),
			shaAlias:    fmt.Sprintf("sha%d", index),
			commitAlias: fmt.Sprintf("commit%d", index),
		}
	}
	return plan
}

func (plan githubCompleteSnapshotPlan) variables(owner, name, defaultBranch string) map[string]string {
	variables := map[string]string{
		"owner": owner,
		"name":  name,
		"ref0":  "refs/heads/" + defaultBranch,
	}
	for _, slot := range plan.slots {
		variables[slot.refAlias] = "refs/heads/" + slot.candidate.Branch
		variables[slot.shaAlias] = slot.candidate.SHA
	}
	return variables
}

func (plan githubCompleteSnapshotPlan) query() string {
	var query strings.Builder
	query.WriteString("query($owner: String!, $name: String!, $ref0: String!")
	for _, slot := range plan.slots {
		fmt.Fprintf(&query, ", $%s: String!, $%s: String!", slot.refAlias, slot.shaAlias)
	}
	query.WriteString(") { repository(owner: $owner, name: $name) {")
	fmt.Fprintf(&query, "%s: ref(qualifiedName: $%s) { target { ... on Commit { oid } } }", plan.defaultRefAlias, plan.defaultRefAlias)
	for _, slot := range plan.slots {
		fmt.Fprintf(&query, "%s: ref(qualifiedName: $%s) { target { ... on Commit { oid } } }", slot.refAlias, slot.refAlias)
		fmt.Fprintf(&query, "%s: object(expression: $%s) { ... on Commit { associatedPullRequests(first: 100) { nodes { merged baseRefName headRefName headRefOid } pageInfo { hasNextPage } } } }", slot.commitAlias, slot.shaAlias)
	}
	query.WriteString("} }")
	return query.String()
}

func githubCompleteSnapshotBatch(owner, name, defaultBranch string, candidates []SnapshotCandidate) (GitHubSnapshot, error) {
	plan := newGitHubCompleteSnapshotPlan(candidates)
	variables := plan.variables(owner, name, defaultBranch)
	out, err := githubGraphQLCall(plan.query(), variables)
	if err != nil {
		return GitHubSnapshot{}, err
	}
	return parseGitHubCompleteSnapshot(out, defaultBranch, plan)
}

func parseGitHubSnapshotRefs(data []byte, defaultBranch string, plan githubCompleteSnapshotPlan) (GitHubSnapshot, error) {
	var response struct {
		Data *struct {
			Repository map[string]json.RawMessage `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return GitHubSnapshot{}, fmt.Errorf("gh: parsing GitHub snapshot: %w", err)
	}
	if len(response.Errors) > 0 {
		return GitHubSnapshot{}, fmt.Errorf("gh: GitHub snapshot failed: %s", response.Errors[0].Message)
	}
	if response.Data == nil || response.Data.Repository == nil {
		return GitHubSnapshot{}, errors.New("gh: GitHub snapshot lacks repository data")
	}
	defaultRef, ok := response.Data.Repository[plan.defaultRefAlias]
	if !ok {
		return GitHubSnapshot{}, errors.New("gh: GitHub snapshot lacks default ref result")
	}
	defaultSHA, present, err := parseGitHubSnapshotRef(defaultRef)
	if err != nil {
		return GitHubSnapshot{}, fmt.Errorf("gh: parsing default ref: %w", err)
	}
	if !present {
		return GitHubSnapshot{}, fmt.Errorf("gh: GitHub snapshot lacks default branch %q", defaultBranch)
	}

	snapshot := GitHubSnapshot{DefaultSHA: defaultSHA, Branches: make([]SnapshotBranch, 0, len(plan.slots))}
	for _, slot := range plan.slots {
		ref, ok := response.Data.Repository[slot.refAlias]
		if !ok {
			return GitHubSnapshot{}, fmt.Errorf("gh: GitHub snapshot lacks ref result for %q", slot.candidate.Branch)
		}
		_, remoteExists, err := parseGitHubSnapshotRef(ref)
		if err != nil {
			return GitHubSnapshot{}, fmt.Errorf("gh: parsing ref %q: %w", slot.candidate.Branch, err)
		}
		snapshot.Branches = append(snapshot.Branches, SnapshotBranch{
			Candidate:    slot.candidate,
			RemoteExists: remoteExists,
		})
	}
	return snapshot, nil
}

func parseGitHubCompleteSnapshot(data []byte, defaultBranch string, plan githubCompleteSnapshotPlan) (GitHubSnapshot, error) {
	snapshot, err := parseGitHubSnapshotRefs(data, defaultBranch, plan)
	if err != nil {
		return GitHubSnapshot{}, err
	}
	var response struct {
		Data *struct {
			Repository map[string]json.RawMessage `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return GitHubSnapshot{}, fmt.Errorf("gh: parsing GitHub merge evidence: %w", err)
	}
	for index, slot := range plan.slots {
		commit, ok := response.Data.Repository[slot.commitAlias]
		if !ok {
			return GitHubSnapshot{}, fmt.Errorf("gh: GitHub merge evidence lacks commit result for %q", slot.candidate.Branch)
		}
		merged, complete, err := parseGitHubSnapshotCommit(commit, defaultBranch, slot.candidate)
		if err != nil {
			return GitHubSnapshot{}, fmt.Errorf("gh: parsing commit %q: %w", slot.candidate.SHA, err)
		}
		snapshot.Branches[index].Verification = SnapshotNotMerged
		if !complete {
			snapshot.Branches[index].Verification = SnapshotNeedsFallback
		} else if merged {
			snapshot.Branches[index].Verification = SnapshotMerged
		}
	}
	return snapshot, nil
}

func parseGitHubSnapshotRef(data json.RawMessage) (sha string, present bool, err error) {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return "", false, nil
	}
	var ref struct {
		Target *struct {
			OID string `json:"oid"`
		} `json:"target"`
	}
	if err := json.Unmarshal(data, &ref); err != nil {
		return "", false, err
	}
	if ref.Target == nil || ref.Target.OID == "" {
		return "", false, errors.New("expected commit target")
	}
	return ref.Target.OID, true, nil
}

func parseGitHubSnapshotCommit(data json.RawMessage, defaultBranch string, candidate SnapshotCandidate) (merged, complete bool, err error) {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return false, false, nil
	}
	var commit struct {
		AssociatedPullRequests *struct {
			Nodes []*struct {
				Merged      bool    `json:"merged"`
				BaseRefName *string `json:"baseRefName"`
				HeadRefName *string `json:"headRefName"`
				HeadRefOID  *string `json:"headRefOid"`
			} `json:"nodes"`
			PageInfo *struct {
				HasNextPage bool `json:"hasNextPage"`
			} `json:"pageInfo"`
		} `json:"associatedPullRequests"`
	}
	if err := json.Unmarshal(data, &commit); err != nil {
		return false, false, err
	}
	if commit.AssociatedPullRequests == nil || commit.AssociatedPullRequests.Nodes == nil || commit.AssociatedPullRequests.PageInfo == nil || commit.AssociatedPullRequests.PageInfo.HasNextPage {
		return false, false, nil
	}
	for _, pr := range commit.AssociatedPullRequests.Nodes {
		if pr == nil {
			return false, false, nil
		}
		if pr.Merged && (pr.BaseRefName == nil || pr.HeadRefName == nil || pr.HeadRefOID == nil) {
			return false, false, nil
		}
		if pr.Merged && *pr.BaseRefName == defaultBranch && *pr.HeadRefName == candidate.Branch && *pr.HeadRefOID == candidate.SHA {
			merged = true
		}
	}
	return merged, true, nil
}
