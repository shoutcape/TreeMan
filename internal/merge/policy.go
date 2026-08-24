package merge

// Evaluate applies the deletion policy to normalized evidence without reading
// Git or querying a forge. It preserves snapshot candidate order.
func Evaluate(snapshot Snapshot) []Decision {
	decisions := make([]Decision, len(snapshot.Candidates))
	for index, candidate := range snapshot.Candidates {
		decision := Decision{
			Candidate: candidate.Candidate,
			Verdict:   VerdictRetain,
			Reason:    ReasonRemoteGoneUnverified,
		}
		switch {
		case candidate.Tip == TipChanged:
			decision.Verdict = VerdictUnknown
			decision.Reason = ReasonTipChanged
		case candidate.Ancestor == AncestorYes:
			decision.Verdict = VerdictCleanable
			decision.Reason = ReasonAncestorOfDefault
		case candidate.Remote == RemotePresent:
			decision.Reason = ReasonRemoteExists
		case candidate.Remote == RemoteAbsent && candidate.Merge == MergeYes:
			decision.Verdict = VerdictCleanable
		}
		decisions[index] = decision
	}
	return decisions
}
