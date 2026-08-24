package merge

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name     string
		evidence Evidence
		verdict  Verdict
		reason   Reason
	}{
		{
			name:     "literal ancestor is cleanable",
			evidence: Evidence{Candidate: Candidate{Branch: "ancestor", SHA: "a"}, Remote: RemotePresent, Ancestor: AncestorYes},
			verdict:  VerdictCleanable,
			reason:   ReasonAncestorOfDefault,
		},
		{
			name:     "remote branch is retained",
			evidence: Evidence{Candidate: Candidate{Branch: "present", SHA: "b"}, Remote: RemotePresent, Ancestor: AncestorNo},
			verdict:  VerdictRetain,
			reason:   ReasonRemoteExists,
		},
		{
			name:     "remote gone with exact merge evidence is cleanable",
			evidence: Evidence{Candidate: Candidate{Branch: "squash", SHA: "c"}, Remote: RemoteAbsent, Ancestor: AncestorNo, Merge: MergeYes},
			verdict:  VerdictCleanable,
			reason:   ReasonRemoteGoneUnverified,
		},
		{
			name:     "remote gone without merge evidence is retained",
			evidence: Evidence{Candidate: Candidate{Branch: "unknown", SHA: "d"}, Remote: RemoteAbsent, Ancestor: AncestorNo, Merge: MergeUnknown},
			verdict:  VerdictRetain,
			reason:   ReasonRemoteGoneUnverified,
		},
		{
			name:     "remote gone with negative evidence is retained",
			evidence: Evidence{Candidate: Candidate{Branch: "not-merged", SHA: "e"}, Remote: RemoteAbsent, Ancestor: AncestorNo, Merge: MergeNo},
			verdict:  VerdictRetain,
			reason:   ReasonRemoteGoneUnverified,
		},
		{
			name:     "moved tip is unknown",
			evidence: Evidence{Candidate: Candidate{Branch: "moved", SHA: "f"}, Remote: RemoteAbsent, Ancestor: AncestorYes, Merge: MergeYes, Tip: TipChanged},
			verdict:  VerdictUnknown,
			reason:   ReasonTipChanged,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decisions := Evaluate(Snapshot{Candidates: []Evidence{test.evidence}})
			assert.Equal(t, Decision{Candidate: test.evidence.Candidate, Verdict: test.verdict, Reason: test.reason}, decisions[0])
		})
	}
}

func TestEvaluatePreservesCandidateOrder(t *testing.T) {
	decisions := Evaluate(Snapshot{Candidates: []Evidence{
		{Candidate: Candidate{Branch: "first", SHA: "a"}, Remote: RemotePresent},
		{Candidate: Candidate{Branch: "second", SHA: "b"}, Ancestor: AncestorYes},
	}})

	assert.Equal(t, []Decision{
		{Candidate: Candidate{Branch: "first", SHA: "a"}, Verdict: VerdictRetain, Reason: ReasonRemoteExists},
		{Candidate: Candidate{Branch: "second", SHA: "b"}, Verdict: VerdictCleanable, Reason: ReasonAncestorOfDefault},
	}, decisions)
}
