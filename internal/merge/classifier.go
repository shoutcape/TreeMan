package merge

// NewClassifier returns the production merge classifier.
func NewClassifier() ClassifierFunc {
	return classifier{}.Classify
}

type classifier struct{}

func (classifier) Classify(defaultBranch string, branches []string) (Result, error) {
	candidates, err := snapshotCandidates(branches)
	if err != nil {
		return Result{}, err
	}
	if len(candidates) == 0 {
		return Result{}, nil
	}
	snapshot, warnings, err := acquire(defaultBranch, candidates)
	if err != nil {
		return Result{}, err
	}
	decisions := Evaluate(snapshot)
	cleanable := make([]Candidate, 0, len(decisions))
	for _, decision := range decisions {
		if decision.Verdict == VerdictCleanable {
			cleanable = append(cleanable, decision.Candidate)
		}
	}
	return Result{Cleanable: cleanable, Diagnostics: warnings}, nil
}
