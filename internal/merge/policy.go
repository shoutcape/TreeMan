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

// Merged reports candidates whose tips are known to be integrated into the
// default branch or descend from a merged pull request head. Unlike Cleanable,
// it is informational and never authorizes deletion from historical evidence.
func Merged(snapshot Snapshot) []Candidate {
	merged := make([]Candidate, 0, len(snapshot.Candidates))
	for _, evidence := range snapshot.Candidates {
		if evidence.Tip == TipStable && (evidence.Ancestor == AncestorYes || evidence.Merge == MergeYes || evidence.Merge == MergeAncestor) {
			merged = append(merged, evidence.Candidate)
		}
	}
	return merged
}
