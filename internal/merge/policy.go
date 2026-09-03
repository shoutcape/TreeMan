package merge

// Cleanable applies the deletion policy without reading Git or querying a
// forge. It preserves snapshot candidate order.
func Cleanable(snapshot Snapshot) []Candidate {
	cleanable := make([]Candidate, 0, len(snapshot.Candidates))
	for _, evidence := range snapshot.Candidates {
		if evidence.Tip == TipStable && (evidence.Ancestor == AncestorYes || (evidence.Remote == RemoteAbsent && mergeAuthorizes(evidence.Merge))) {
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
		if evidence.Tip == TipStable && (evidence.Ancestor == AncestorYes || mergeAuthorizes(evidence.Merge) || evidence.Merge == MergeAncestor) {
			merged = append(merged, evidence.Candidate)
		}
	}
	return merged
}

// mergeAuthorizes reports whether forge evidence accounts for every commit on
// the candidate tip. Both states prove the tip's commits reached the default
// branch: MergeYes because the tip was the merged head, MergeContained because
// the merged head reached the tip's commits along with its own.
func mergeAuthorizes(state MergeState) bool {
	return state == MergeYes || state == MergeContained
}
