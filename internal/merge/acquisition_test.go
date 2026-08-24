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
	a.remoteHeads = func(branches []string) (map[string]string, error) {
		assert.Equal(t, []string{"main", "feature"}, branches)
		return map[string]string{"main": "default", "feature": "feature-sha"}, nil
	}
	a.remoteTrackingSHA = func(string) (string, bool, error) { return "default", true, nil }
	a.mergedBranches = func(target string) (map[string]string, error) {
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

func TestAcquireDelaysUnavailableForgeWarningUntilMergeEvidenceIsNeeded(t *testing.T) {
	a := testAcquirer()
	a.originRemoteURL = func() (string, error) { return "", errors.New("origin unavailable") }
	a.remoteHeads = func([]string) (map[string]string, error) {
		return map[string]string{"main": "default"}, nil
	}
	a.remoteTrackingSHA = func(string) (string, bool, error) { return "default", true, nil }
	a.mergedBranches = func(string) (map[string]string, error) { return map[string]string{}, nil }
	a.branchSHAs = func(branches []string) (map[string]string, error) {
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
	a.remoteHeads = func([]string) (map[string]string, error) { return map[string]string{"main": "expected"}, nil }
	trackingCalls := 0
	a.remoteTrackingSHA = func(string) (string, bool, error) {
		trackingCalls++
		if trackingCalls == 1 {
			return "stale", true, nil
		}
		return "changed", true, nil
	}
	fetched := false
	a.fetch = func(refspec string) error {
		fetched = true
		assert.Equal(t, "refs/heads/main:refs/remotes/origin/main", refspec)
		return nil
	}

	_, _, err := a.acquire("main", []Candidate{{Branch: "feature", SHA: "feature-sha"}})

	require.EqualError(t, err, "origin/main changed while refreshing merge state")
	assert.True(t, fetched)
}

func TestAcquireNormalizesGitHubSnapshotEvidence(t *testing.T) {
	a := testAcquirer()
	a.originRemoteURL = func() (string, error) { return "https://github.com/org/repo.git", nil }
	a.resolveForge = func(string) (forge.Type, string, string, error) { return forge.GitHub, "org/repo", "github.com", nil }
	a.lookPath = func(string) (string, error) { return "/bin/gh", nil }
	a.githubSnapshot = func(repo, defaultBranch string, candidates []forge.SnapshotCandidate) (forge.GitHubSnapshot, error) {
		assert.Equal(t, "org/repo", repo)
		assert.Equal(t, "main", defaultBranch)
		return forge.GitHubSnapshot{DefaultSHA: "default", Branches: []forge.SnapshotBranch{{
			Candidate:    candidates[0],
			RemoteExists: false,
			Verification: forge.SnapshotMerged,
		}}}, nil
	}
	a.remoteTrackingSHA = func(string) (string, bool, error) { return "default", true, nil }
	a.mergedBranches = func(string) (map[string]string, error) { return map[string]string{}, nil }
	a.branchSHAs = func([]string) (map[string]string, error) { return map[string]string{"feature": "feature-sha"}, nil }

	snapshot, warnings, err := a.acquire("main", []Candidate{{Branch: "feature", SHA: "feature-sha"}})

	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, RemoteAbsent, snapshot.Candidates[0].Remote)
	assert.Equal(t, MergeYes, snapshot.Candidates[0].Merge)
	assert.Equal(t, VerdictCleanable, Evaluate(snapshot)[0].Verdict)
}

func TestAcquireNormalizesGitLabBatchEvidence(t *testing.T) {
	a := testAcquirer()
	a.originRemoteURL = func() (string, error) { return "https://gitlab.example/group/repo.git", nil }
	a.resolveForge = func(string) (forge.Type, string, string, error) {
		return forge.GitLab, "group/repo", "gitlab.example", nil
	}
	a.lookPath = func(string) (string, error) { return "/bin/glab", nil }
	a.remoteHeads = func([]string) (map[string]string, error) { return map[string]string{"main": "default"}, nil }
	a.remoteTrackingSHA = func(string) (string, bool, error) { return "default", true, nil }
	a.mergedBranches = func(string) (map[string]string, error) { return map[string]string{}, nil }
	a.branchSHAs = func([]string) (map[string]string, error) { return map[string]string{"feature": "feature-sha"}, nil }
	a.gitlabMergedHeads = func(repo, host, defaultBranch string, candidates []forge.SnapshotCandidate) (map[string]bool, error) {
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
	assert.Equal(t, VerdictCleanable, Evaluate(snapshot)[0].Verdict)
}

func TestAcquireRetainsBranchWhoseTipChangesDuringVerification(t *testing.T) {
	a := testAcquirer()
	a.remoteHeads = func([]string) (map[string]string, error) { return map[string]string{"main": "default"}, nil }
	a.remoteTrackingSHA = func(string) (string, bool, error) { return "default", true, nil }
	a.mergedBranches = func(string) (map[string]string, error) { return map[string]string{}, nil }
	a.branchSHAs = func([]string) (map[string]string, error) { return map[string]string{"feature": "moved-sha"}, nil }

	snapshot, warnings, err := a.acquire("main", []Candidate{{Branch: "feature", SHA: "feature-sha"}})

	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, TipChanged, snapshot.Candidates[0].Tip)
	assert.Equal(t, VerdictUnknown, Evaluate(snapshot)[0].Verdict)
}

func testAcquirer() acquirer {
	return acquirer{
		branchSHAs:        func([]string) (map[string]string, error) { panic("unexpected BranchSHAs call") },
		remoteHeads:       func([]string) (map[string]string, error) { panic("unexpected RemoteHeads call") },
		remoteTrackingSHA: func(string) (string, bool, error) { panic("unexpected RemoteTrackingBranchSHA call") },
		fetch:             func(string) error { panic("unexpected Fetch call") },
		mergedBranches:    func(string) (map[string]string, error) { panic("unexpected MergedBranches call") },
		originRemoteURL:   func() (string, error) { return "", errors.New("no forge") },
		resolveForge:      func(string) (forge.Type, string, string, error) { panic("unexpected ResolveFromRemote call") },
		lookPath:          func(string) (string, error) { panic("unexpected LookPath call") },
		githubSnapshot: func(string, string, []forge.SnapshotCandidate) (forge.GitHubSnapshot, error) {
			panic("unexpected GitHub snapshot call")
		},
		gitlabMergedHeads: func(string, string, string, []forge.SnapshotCandidate) (map[string]bool, error) {
			panic("unexpected GitLab batch call")
		},
		mergedPRHead: func(forge.Type, string, string, string, string, string) (bool, error) {
			panic("unexpected merged PR call")
		},
	}
}
