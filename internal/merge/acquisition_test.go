package merge

import (
	"errors"
	"testing"

	"github.com/shoutcape/treeman/internal/forge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAcquireUsesFreshDefaultBranchForAncestorEvidence(t *testing.T) {
	a := testAcquirer()
	a.git.remoteHeads = func(branches []string) (map[string]string, error) {
		assert.Equal(t, []string{"main", "feature"}, branches)
		return map[string]string{"main": "default", "feature": "feature-sha"}, nil
	}
	a.git.remoteTrackingSHA = func(string) (string, bool, error) { return "default", true, nil }
	a.git.mergedBranches = func(target string) (map[string]string, error) {
		assert.Equal(t, "origin/main", target)
		return map[string]string{"feature": "feature-sha"}, nil
	}

	snapshot, warnings, err := a.acquire("main", []Candidate{{Branch: "feature", SHA: "feature-sha"}})

	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, Snapshot{DefaultSHA: "default", Candidates: []Evidence{{
		Candidate: Candidate{Branch: "feature", SHA: "feature-sha"},
		Remote:    RemotePresent,
		Ancestor:  AncestorYes,
		Merge:     MergeUnknown,
		Tip:       TipStable,
	}}}, snapshot)
}

func TestAcquirerClassifySnapshotsCandidatesWithInjectedBranchSHAs(t *testing.T) {
	a := testAcquirer()
	a.git.branchSHAs = func(branches []string) (map[string]string, error) {
		assert.Equal(t, []string{"feature"}, branches)
		return map[string]string{"feature": "feature-sha"}, nil
	}
	a.git.remoteHeads = func(branches []string) (map[string]string, error) {
		assert.Equal(t, []string{"main", "feature"}, branches)
		return map[string]string{"main": "default", "feature": "feature-sha"}, nil
	}
	a.git.remoteTrackingSHA = func(string) (string, bool, error) { return "default", true, nil }
	a.git.mergedBranches = func(string) (map[string]string, error) { return map[string]string{"feature": "feature-sha"}, nil }

	result, err := a.Classify("main", []string{"feature"})

	require.NoError(t, err)
	assert.Equal(t, []Candidate{{Branch: "feature", SHA: "feature-sha"}}, result.Merged)
	assert.Equal(t, []Candidate{{Branch: "feature", SHA: "feature-sha"}}, result.Cleanable)
}

func TestAcquirerSnapshotCandidatesRejectsDuplicateBranches(t *testing.T) {
	a := testAcquirer()
	a.git.branchSHAs = func(branches []string) (map[string]string, error) {
		assert.Equal(t, []string{"feature", "feature"}, branches)
		return map[string]string{"feature": "feature-sha"}, nil
	}

	_, err := a.snapshotCandidates([]string{"feature", "feature"})

	require.EqualError(t, err, `duplicate local branch "feature"`)
}

func TestAcquirerSnapshotCandidatesRejectsUnresolvedBranch(t *testing.T) {
	a := testAcquirer()
	a.git.branchSHAs = func(branches []string) (map[string]string, error) {
		assert.Equal(t, []string{"feature", "missing"}, branches)
		return map[string]string{"feature": "feature-sha"}, nil
	}

	_, err := a.snapshotCandidates([]string{"feature", "missing"})

	require.EqualError(t, err, `could not resolve local branch "missing"`)
}

func TestAcquirerSnapshotCandidatesPreservesInputOrder(t *testing.T) {
	a := testAcquirer()
	a.git.branchSHAs = func(branches []string) (map[string]string, error) {
		assert.Equal(t, []string{"second", "first", "third"}, branches)
		return map[string]string{
			"first":  "first-sha",
			"second": "second-sha",
			"third":  "third-sha",
		}, nil
	}

	candidates, err := a.snapshotCandidates([]string{"second", "first", "third"})

	require.NoError(t, err)
	assert.Equal(t, []Candidate{
		{Branch: "second", SHA: "second-sha"},
		{Branch: "first", SHA: "first-sha"},
		{Branch: "third", SHA: "third-sha"},
	}, candidates)
}

func TestAcquireDelaysUnavailableForgeWarningUntilMergeEvidenceIsNeeded(t *testing.T) {
	a := testAcquirer()
	a.forge.originRemoteURL = func() (string, error) { return "", errors.New("origin unavailable") }
	a.git.remoteHeads = func([]string) (map[string]string, error) {
		return map[string]string{"main": "default"}, nil
	}
	a.git.remoteTrackingSHA = func(string) (string, bool, error) { return "default", true, nil }
	a.git.mergedBranches = func(string) (map[string]string, error) { return map[string]string{}, nil }
	a.git.branchSHAs = func(branches []string) (map[string]string, error) {
		assert.Equal(t, []string{"feature"}, branches)
		return map[string]string{"feature": "feature-sha"}, nil
	}

	snapshot, warnings, err := a.acquire("main", []Candidate{{Branch: "feature", SHA: "feature-sha"}})

	require.NoError(t, err)
	assert.Equal(t, RemoteAbsent, snapshot.Candidates[0].Remote)
	assert.Equal(t, MergeUnknown, snapshot.Candidates[0].Merge)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0].String(), "could not read origin remote")
}

func TestAcquireRejectsChangedDefaultBranchAfterFetch(t *testing.T) {
	a := testAcquirer()
	a.git.remoteHeads = func([]string) (map[string]string, error) { return map[string]string{"main": "expected"}, nil }
	trackingCalls := 0
	a.git.remoteTrackingSHA = func(string) (string, bool, error) {
		trackingCalls++
		if trackingCalls == 1 {
			return "stale", true, nil
		}
		return "changed", true, nil
	}
	fetched := false
	a.git.fetch = func(refspec string) error {
		fetched = true
		assert.Equal(t, "refs/heads/main:refs/remotes/origin/main", refspec)
		return nil
	}

	_, _, err := a.acquire("main", []Candidate{{Branch: "feature", SHA: "feature-sha"}})

	require.EqualError(t, err, "origin/main changed while refreshing merge state")
	assert.True(t, fetched)
}

func TestAcquireUsesOneCompleteGitHubSnapshotForDeletedSquashMerge(t *testing.T) {
	a := testAcquirer()
	a.forge.originRemoteURL = func() (string, error) { return "https://github.com/org/repo.git", nil }
	a.forge.resolveForge = func(string) (forge.Type, string, string, error) { return forge.GitHub, "org/repo", "github.com", nil }
	a.forge.lookPath = func(string) (string, error) { return "/bin/gh", nil }
	calls := 0
	a.forge.githubSnapshot = func(repo, defaultBranch string, candidates []forge.SnapshotCandidate) (forge.GitHubSnapshot, error) {
		calls++
		assert.Equal(t, "org/repo", repo)
		assert.Equal(t, "main", defaultBranch)
		return forge.GitHubSnapshot{DefaultSHA: "default", Branches: []forge.SnapshotBranch{{
			Candidate:    candidates[0],
			RemoteExists: false,
			Verification: forge.SnapshotMerged,
		}}}, nil
	}
	a.git.remoteTrackingSHA = func(string) (string, bool, error) { return "default", true, nil }
	a.git.mergedBranches = func(string) (map[string]string, error) { return map[string]string{}, nil }
	a.git.branchSHAs = func([]string) (map[string]string, error) { return map[string]string{"feature": "feature-sha"}, nil }

	snapshot, warnings, err := a.acquire("main", []Candidate{{Branch: "feature", SHA: "feature-sha"}})

	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, 1, calls)
	assert.Equal(t, RemoteAbsent, snapshot.Candidates[0].Remote)
	assert.Equal(t, MergeYes, snapshot.Candidates[0].Merge)
	assert.Equal(t, []Candidate{{Branch: "feature", SHA: "feature-sha"}}, Cleanable(snapshot))
}

func TestAcquireUsesCompleteGitHubSnapshotForRemotePresentAndAncestorBranches(t *testing.T) {
	a := testAcquirer()
	a.forge.originRemoteURL = func() (string, error) { return "https://github.com/org/repo.git", nil }
	a.forge.resolveForge = func(string) (forge.Type, string, string, error) { return forge.GitHub, "org/repo", "github.com", nil }
	a.forge.lookPath = func(string) (string, error) { return "/bin/gh", nil }
	a.forge.githubSnapshot = func(_ string, _ string, candidates []forge.SnapshotCandidate) (forge.GitHubSnapshot, error) {
		return forge.GitHubSnapshot{DefaultSHA: "default", Branches: []forge.SnapshotBranch{
			{Candidate: candidates[0], RemoteExists: true, Verification: forge.SnapshotNotMerged},
			{Candidate: candidates[1], Verification: forge.SnapshotNotMerged},
		}}, nil
	}
	a.git.remoteTrackingSHA = func(string) (string, bool, error) { return "default", true, nil }
	a.git.mergedBranches = func(string) (map[string]string, error) { return map[string]string{"ancestor": "ancestor-sha"}, nil }

	snapshot, warnings, err := a.acquire("main", []Candidate{{Branch: "present", SHA: "present-sha"}, {Branch: "ancestor", SHA: "ancestor-sha"}})

	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, RemotePresent, snapshot.Candidates[0].Remote)
	assert.Equal(t, AncestorYes, snapshot.Candidates[1].Ancestor)
}

func TestAcquireMarksPostMergeDescendantWithoutAuthorizingCleanup(t *testing.T) {
	a := testAcquirer()
	a.forge.originRemoteURL = func() (string, error) { return "https://github.com/org/repo.git", nil }
	a.forge.resolveForge = func(string) (forge.Type, string, string, error) { return forge.GitHub, "org/repo", "github.com", nil }
	a.forge.lookPath = func(string) (string, error) { return "/bin/gh", nil }
	a.forge.githubSnapshot = func(_ string, _ string, candidates []forge.SnapshotCandidate) (forge.GitHubSnapshot, error) {
		return forge.GitHubSnapshot{DefaultSHA: "default", Branches: []forge.SnapshotBranch{{
			Candidate:    candidates[0],
			Verification: forge.SnapshotNotMerged,
			MergedHeads:  []string{"missing-sha", "merged-sha"},
		}}}, nil
	}
	a.git.remoteTrackingSHA = func(string) (string, bool, error) { return "default", true, nil }
	a.git.mergedBranches = func(string) (map[string]string, error) { return map[string]string{}, nil }
	a.git.branchSHAs = func([]string) (map[string]string, error) { return map[string]string{"feature": "post-merge-sha"}, nil }
	a.git.anyAncestor = func(ancestors []string, descendant string) (bool, error) {
		assert.Equal(t, []string{"missing-sha", "merged-sha"}, ancestors)
		assert.Equal(t, "post-merge-sha", descendant)
		return true, nil
	}

	snapshot, warnings, err := a.acquire("main", []Candidate{{Branch: "feature", SHA: "post-merge-sha"}})

	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, MergeAncestor, snapshot.Candidates[0].Merge)
	assert.Equal(t, []Candidate{{Branch: "feature", SHA: "post-merge-sha"}}, Merged(snapshot))
	assert.Empty(t, Cleanable(snapshot))
}

func TestAcquireRetainsReusedBranchWithoutMergedHeadAncestry(t *testing.T) {
	a := testAcquirer()
	a.forge.originRemoteURL = func() (string, error) { return "https://github.com/org/repo.git", nil }
	a.forge.resolveForge = func(string) (forge.Type, string, string, error) { return forge.GitHub, "org/repo", "github.com", nil }
	a.forge.lookPath = func(string) (string, error) { return "/bin/gh", nil }
	a.forge.githubSnapshot = func(_ string, _ string, candidates []forge.SnapshotCandidate) (forge.GitHubSnapshot, error) {
		return forge.GitHubSnapshot{DefaultSHA: "default", Branches: []forge.SnapshotBranch{{
			Candidate:    candidates[0],
			Verification: forge.SnapshotNotMerged,
			MergedHeads:  []string{"old-merged-sha"},
		}}}, nil
	}
	a.git.remoteTrackingSHA = func(string) (string, bool, error) { return "default", true, nil }
	a.git.mergedBranches = func(string) (map[string]string, error) { return map[string]string{}, nil }
	a.git.branchSHAs = func([]string) (map[string]string, error) { return map[string]string{"feature": "new-sha"}, nil }
	a.git.anyAncestor = func([]string, string) (bool, error) { return false, nil }

	snapshot, warnings, err := a.acquire("main", []Candidate{{Branch: "feature", SHA: "new-sha"}})

	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, MergeNo, snapshot.Candidates[0].Merge)
	assert.Empty(t, Merged(snapshot))
	assert.Empty(t, Cleanable(snapshot))
}

func TestAcquireRejectsChangedDefaultBranchAfterGitHubSnapshot(t *testing.T) {
	a := testAcquirer()
	a.forge.originRemoteURL = func() (string, error) { return "https://github.com/org/repo.git", nil }
	a.forge.resolveForge = func(string) (forge.Type, string, string, error) { return forge.GitHub, "org/repo", "github.com", nil }
	a.forge.lookPath = func(string) (string, error) { return "/bin/gh", nil }
	a.forge.githubSnapshot = func(_ string, _ string, candidates []forge.SnapshotCandidate) (forge.GitHubSnapshot, error) {
		return forge.GitHubSnapshot{DefaultSHA: "default", Branches: []forge.SnapshotBranch{{Candidate: candidates[0], Verification: forge.SnapshotNotMerged}}}, nil
	}
	trackingCalls := 0
	a.git.remoteTrackingSHA = func(string) (string, bool, error) {
		trackingCalls++
		if trackingCalls == 1 {
			return "stale", true, nil
		}
		return "changed", true, nil
	}
	a.git.fetch = func(string) error { return nil }

	_, _, err := a.acquire("main", []Candidate{{Branch: "feature", SHA: "feature-sha"}})

	require.EqualError(t, err, "origin/main changed while refreshing merge state")
}

func TestAcquireFallsBackToGitWhenGitHubSnapshotDefaultSHAChanges(t *testing.T) {
	a := testAcquirer()
	a.forge.originRemoteURL = func() (string, error) { return "https://github.com/org/repo.git", nil }
	a.forge.resolveForge = func(string) (forge.Type, string, string, error) { return forge.GitHub, "org/repo", "github.com", nil }
	a.forge.lookPath = func(string) (string, error) { return "/bin/gh", nil }
	a.forge.githubSnapshot = func(string, string, []forge.SnapshotCandidate) (forge.GitHubSnapshot, error) {
		return forge.GitHubSnapshot{}, forge.ErrGitHubDefaultBranchChanged
	}
	a.git.remoteHeads = func(branches []string) (map[string]string, error) {
		assert.Equal(t, []string{"main", "feature"}, branches)
		return map[string]string{"main": "default", "feature": "feature-sha"}, nil
	}
	a.git.remoteTrackingSHA = func(string) (string, bool, error) { return "default", true, nil }
	a.git.mergedBranches = func(string) (map[string]string, error) {
		return map[string]string{"feature": "feature-sha"}, nil
	}

	snapshot, warnings, err := a.acquire("main", []Candidate{{Branch: "feature", SHA: "feature-sha"}})

	require.NoError(t, err)
	assert.Equal(t, AncestorYes, snapshot.Candidates[0].Ancestor)
	require.Len(t, warnings, 1)
	assert.ErrorIs(t, warnings[0].Err, forge.ErrGitHubDefaultBranchChanged)
}

func TestAcquireFallsBackToGitAndRESTAfterGitHubSnapshotFailure(t *testing.T) {
	a := testAcquirer()
	a.forge.originRemoteURL = func() (string, error) { return "https://github.com/org/repo.git", nil }
	a.forge.resolveForge = func(string) (forge.Type, string, string, error) { return forge.GitHub, "org/repo", "github.com", nil }
	a.forge.lookPath = func(string) (string, error) { return "/bin/gh", nil }
	a.forge.githubSnapshot = func(string, string, []forge.SnapshotCandidate) (forge.GitHubSnapshot, error) {
		return forge.GitHubSnapshot{}, assert.AnError
	}
	a.git.remoteHeads = func(branches []string) (map[string]string, error) {
		assert.Equal(t, []string{"main", "feature"}, branches)
		return map[string]string{"main": "default"}, nil
	}
	a.git.remoteTrackingSHA = func(string) (string, bool, error) { return "default", true, nil }
	a.git.mergedBranches = func(string) (map[string]string, error) { return map[string]string{}, nil }
	a.git.branchSHAs = func([]string) (map[string]string, error) { return map[string]string{"feature": "feature-sha"}, nil }
	called := false
	a.forge.mergedPRHead = func(kind forge.Type, repo, host, defaultBranch, branch, sha string) (bool, error) {
		called = true
		assert.Equal(t, forge.GitHub, kind)
		assert.Equal(t, "org/repo", repo)
		assert.Equal(t, "github.com", host)
		assert.Equal(t, "main", defaultBranch)
		assert.Equal(t, "feature", branch)
		assert.Equal(t, "feature-sha", sha)
		return true, nil
	}

	snapshot, warnings, err := a.acquire("main", []Candidate{{Branch: "feature", SHA: "feature-sha"}})

	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, MergeYes, snapshot.Candidates[0].Merge)
	assert.Equal(t, []Candidate{{Branch: "feature", SHA: "feature-sha"}}, Cleanable(snapshot))
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0].String(), "GitHub snapshot failed")
}

func TestAcquireReportsGitHubSnapshotFailureWhenFallbackFindsAncestor(t *testing.T) {
	a := testAcquirer()
	a.forge.originRemoteURL = func() (string, error) { return "https://github.com/org/repo.git", nil }
	a.forge.resolveForge = func(string) (forge.Type, string, string, error) { return forge.GitHub, "org/repo", "github.com", nil }
	a.forge.lookPath = func(string) (string, error) { return "/bin/gh", nil }
	a.forge.githubSnapshot = func(string, string, []forge.SnapshotCandidate) (forge.GitHubSnapshot, error) {
		return forge.GitHubSnapshot{}, assert.AnError
	}
	a.git.remoteHeads = func([]string) (map[string]string, error) {
		return map[string]string{"main": "default", "feature": "feature-sha"}, nil
	}
	a.git.remoteTrackingSHA = func(string) (string, bool, error) { return "default", true, nil }
	a.git.mergedBranches = func(string) (map[string]string, error) {
		return map[string]string{"feature": "feature-sha"}, nil
	}

	_, warnings, err := a.acquire("main", []Candidate{{Branch: "feature", SHA: "feature-sha"}})

	require.NoError(t, err)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0].String(), "GitHub snapshot failed")
}

func TestAcquireFallsBackToRESTForRemoteAbsentGitHubSnapshotCandidates(t *testing.T) {
	a := testAcquirer()
	a.forge.originRemoteURL = func() (string, error) { return "https://github.com/org/repo.git", nil }
	a.forge.resolveForge = func(string) (forge.Type, string, string, error) { return forge.GitHub, "org/repo", "github.com", nil }
	a.forge.lookPath = func(string) (string, error) { return "/bin/gh", nil }
	a.forge.githubSnapshot = func(_ string, _ string, candidates []forge.SnapshotCandidate) (forge.GitHubSnapshot, error) {
		return forge.GitHubSnapshot{DefaultSHA: "default", Branches: []forge.SnapshotBranch{
			{Candidate: candidates[0], Verification: forge.SnapshotNotMerged},
			{Candidate: candidates[1], Verification: forge.SnapshotNeedsFallback},
		}}, nil
	}
	a.git.remoteTrackingSHA = func(string) (string, bool, error) { return "default", true, nil }
	a.git.mergedBranches = func(string) (map[string]string, error) { return map[string]string{}, nil }
	a.git.branchSHAs = func(branches []string) (map[string]string, error) {
		assert.Equal(t, []string{"complete", "fallback"}, branches)
		return map[string]string{"complete": "complete-sha", "fallback": "fallback-sha"}, nil
	}
	a.forge.mergedPRHead = func(kind forge.Type, _, _, _, branch, sha string) (bool, error) {
		assert.Equal(t, forge.GitHub, kind)
		if branch == "complete" {
			assert.Equal(t, "complete-sha", sha)
			return false, nil
		}
		assert.Equal(t, "fallback", branch)
		assert.Equal(t, "fallback-sha", sha)
		return true, nil
	}

	snapshot, warnings, err := a.acquire("main", []Candidate{{Branch: "complete", SHA: "complete-sha"}, {Branch: "fallback", SHA: "fallback-sha"}})

	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, MergeNo, snapshot.Candidates[0].Merge)
	assert.Equal(t, MergeYes, snapshot.Candidates[1].Merge)
	assert.Equal(t, []Candidate{{Branch: "fallback", SHA: "fallback-sha"}}, Cleanable(snapshot))
}

func TestAcquireNormalizesGitLabBatchEvidence(t *testing.T) {
	a := testAcquirer()
	a.forge.originRemoteURL = func() (string, error) { return "https://gitlab.example/group/repo.git", nil }
	a.forge.resolveForge = func(string) (forge.Type, string, string, error) {
		return forge.GitLab, "group/repo", "gitlab.example", nil
	}
	a.forge.lookPath = func(string) (string, error) { return "/bin/glab", nil }
	a.git.remoteHeads = func([]string) (map[string]string, error) { return map[string]string{"main": "default"}, nil }
	a.git.remoteTrackingSHA = func(string) (string, bool, error) { return "default", true, nil }
	a.git.mergedBranches = func(string) (map[string]string, error) { return map[string]string{}, nil }
	a.git.branchSHAs = func([]string) (map[string]string, error) { return map[string]string{"feature": "feature-sha"}, nil }
	a.forge.gitlabMergedHeads = func(repo, host, defaultBranch string, candidates []forge.SnapshotCandidate) (map[string]bool, error) {
		assert.Equal(t, "group/repo", repo)
		assert.Equal(t, "gitlab.example", host)
		assert.Equal(t, "main", defaultBranch)
		assert.Equal(t, []forge.SnapshotCandidate{{Branch: "feature", SHA: "feature-sha"}}, candidates)
		return map[string]bool{"feature": true}, nil
	}

	snapshot, warnings, err := a.acquire("main", []Candidate{{Branch: "feature", SHA: "feature-sha"}})

	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, MergeYes, snapshot.Candidates[0].Merge)
	assert.Equal(t, []Candidate{{Branch: "feature", SHA: "feature-sha"}}, Cleanable(snapshot))
}

func TestAcquireFallsBackToRESTAfterGitLabBatchFailure(t *testing.T) {
	a := testAcquirer()
	a.forge.originRemoteURL = func() (string, error) { return "https://gitlab.example/group/repo.git", nil }
	a.forge.resolveForge = func(string) (forge.Type, string, string, error) {
		return forge.GitLab, "group/repo", "gitlab.example", nil
	}
	a.forge.lookPath = func(string) (string, error) { return "/bin/glab", nil }
	a.git.remoteHeads = func([]string) (map[string]string, error) { return map[string]string{"main": "default"}, nil }
	a.git.remoteTrackingSHA = func(string) (string, bool, error) { return "default", true, nil }
	a.git.mergedBranches = func(string) (map[string]string, error) { return map[string]string{}, nil }
	a.git.branchSHAs = func([]string) (map[string]string, error) { return map[string]string{"feature": "feature-sha"}, nil }
	a.forge.gitlabMergedHeads = func(string, string, string, []forge.SnapshotCandidate) (map[string]bool, error) {
		return nil, assert.AnError
	}
	called := false
	a.forge.mergedPRHead = func(kind forge.Type, repo, host, defaultBranch, branch, sha string) (bool, error) {
		called = true
		assert.Equal(t, forge.GitLab, kind)
		assert.Equal(t, "group/repo", repo)
		assert.Equal(t, "gitlab.example", host)
		assert.Equal(t, "main", defaultBranch)
		assert.Equal(t, "feature", branch)
		assert.Equal(t, "feature-sha", sha)
		return true, nil
	}

	snapshot, warnings, err := a.acquire("main", []Candidate{{Branch: "feature", SHA: "feature-sha"}})

	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, MergeYes, snapshot.Candidates[0].Merge)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0].String(), "GitLab merge verification failed")
}

func TestAcquireRetainsBranchWhoseTipChangesDuringVerification(t *testing.T) {
	a := testAcquirer()
	a.git.remoteHeads = func([]string) (map[string]string, error) { return map[string]string{"main": "default"}, nil }
	a.git.remoteTrackingSHA = func(string) (string, bool, error) { return "default", true, nil }
	a.git.mergedBranches = func(string) (map[string]string, error) { return map[string]string{}, nil }
	a.git.branchSHAs = func([]string) (map[string]string, error) { return map[string]string{"feature": "moved-sha"}, nil }

	snapshot, warnings, err := a.acquire("main", []Candidate{{Branch: "feature", SHA: "feature-sha"}})

	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, TipChanged, snapshot.Candidates[0].Tip)
	assert.Empty(t, Cleanable(snapshot))
}

func testAcquirer() acquirer {
	return acquirer{
		git: gitAcquirer{
			branchSHAs:        func([]string) (map[string]string, error) { panic("unexpected BranchSHAs call") },
			remoteHeads:       func([]string) (map[string]string, error) { panic("unexpected RemoteHeads call") },
			remoteTrackingSHA: func(string) (string, bool, error) { panic("unexpected RemoteTrackingBranchSHA call") },
			fetch:             func(string) error { panic("unexpected Fetch call") },
			mergedBranches:    func(string) (map[string]string, error) { panic("unexpected MergedBranches call") },
			anyAncestor:       func([]string, string) (bool, error) { panic("unexpected AnyCommitIsAncestor call") },
		},
		forge: forgeAcquirer{
			originRemoteURL: func() (string, error) { return "", errors.New("no forge") },
			resolveForge:    func(string) (forge.Type, string, string, error) { panic("unexpected ResolveFromRemote call") },
			lookPath:        func(string) (string, error) { panic("unexpected LookPath call") },
			githubSnapshot: func(string, string, []forge.SnapshotCandidate) (forge.GitHubSnapshot, error) {
				panic("unexpected GitHub snapshot call")
			},
			gitlabMergedHeads: func(string, string, string, []forge.SnapshotCandidate) (map[string]bool, error) {
				panic("unexpected GitLab batch call")
			},
			mergedPRHead: func(forge.Type, string, string, string, string, string) (bool, error) {
				panic("unexpected merged PR call")
			},
		},
	}
}
