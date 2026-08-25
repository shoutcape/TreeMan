package merge

// Cleanable applies the deletion policy without reading Git or querying a
// forge. It preserves snapshot candidate order.
func Cleanable(snapshot Snapshot) []Candidate {
	cleanable := make([]Candidate, 0, len(snapshot.Candidates))
	for _, evidence := range snapshot.Candidates {
		if evidence.Tip == TipStable && (evidence.Ancestor == AncestorYes || (evidence.Remote == RemoteAbsent && evidence.Merge == MergeYes)) {
			cleanable = append(cleanable, evidence.Candidate)
		}
	}
	return cleanable
}
