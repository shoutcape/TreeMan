package merge

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCleanable(t *testing.T) {
	tests := []struct {
		name      string
		evidence  Evidence
		cleanable bool
	}{
		{
			name:      "literal ancestor is cleanable",
			evidence:  Evidence{Candidate: Candidate{Branch: "ancestor", SHA: "a"}, Remote: RemotePresent, Ancestor: AncestorYes},
			cleanable: true,
		},
		{
			name:     "remote branch is retained",
			evidence: Evidence{Candidate: Candidate{Branch: "present", SHA: "b"}, Remote: RemotePresent, Ancestor: AncestorNo},
		},
		{
			name:      "remote gone with exact merge evidence is cleanable",
			evidence:  Evidence{Candidate: Candidate{Branch: "squash", SHA: "c"}, Remote: RemoteAbsent, Ancestor: AncestorNo, Merge: MergeYes},
			cleanable: true,
		},
		{
			name:     "remote gone without merge evidence is retained",
			evidence: Evidence{Candidate: Candidate{Branch: "unknown", SHA: "d"}, Remote: RemoteAbsent, Ancestor: AncestorNo, Merge: MergeUnknown},
		},
		{
			name:     "remote gone with negative evidence is retained",
			evidence: Evidence{Candidate: Candidate{Branch: "not-merged", SHA: "e"}, Remote: RemoteAbsent, Ancestor: AncestorNo, Merge: MergeNo},
		},
		{
			name:     "remote gone descendant of a merged head is retained",
			evidence: Evidence{Candidate: Candidate{Branch: "post-merge", SHA: "g"}, Remote: RemoteAbsent, Ancestor: AncestorNo, Merge: MergeAncestor},
		},
		{
			name:     "moved tip is unknown",
			evidence: Evidence{Candidate: Candidate{Branch: "moved", SHA: "f"}, Remote: RemoteAbsent, Ancestor: AncestorYes, Merge: MergeYes, Tip: TipChanged},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleanable := Cleanable(Snapshot{Candidates: []Evidence{test.evidence}})
			if test.cleanable {
				assert.Equal(t, []Candidate{test.evidence.Candidate}, cleanable)
			} else {
				assert.Empty(t, cleanable)
			}
		})
	}
}

func TestMerged(t *testing.T) {
	merged := Merged(Snapshot{Candidates: []Evidence{
		{Candidate: Candidate{Branch: "ancestor", SHA: "a"}, Ancestor: AncestorYes},
		{Candidate: Candidate{Branch: "exact", SHA: "b"}, Merge: MergeYes},
		{Candidate: Candidate{Branch: "post-merge", SHA: "c"}, Merge: MergeAncestor},
		{Candidate: Candidate{Branch: "unmerged", SHA: "d"}, Merge: MergeNo},
		{Candidate: Candidate{Branch: "moved", SHA: "e"}, Ancestor: AncestorYes, Tip: TipChanged},
	}})

	assert.Equal(t, []Candidate{
		{Branch: "ancestor", SHA: "a"},
		{Branch: "exact", SHA: "b"},
		{Branch: "post-merge", SHA: "c"},
	}, merged)
}

func TestCleanablePreservesCandidateOrder(t *testing.T) {
	cleanable := Cleanable(Snapshot{Candidates: []Evidence{
		{Candidate: Candidate{Branch: "first", SHA: "a"}, Remote: RemotePresent},
		{Candidate: Candidate{Branch: "second", SHA: "b"}, Ancestor: AncestorYes},
	}})

	assert.Equal(t, []Candidate{{Branch: "second", SHA: "b"}}, cleanable)
}
