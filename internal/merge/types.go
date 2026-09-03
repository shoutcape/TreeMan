// Package merge classifies local branches that can be safely removed after merge.
package merge

import "fmt"

// Candidate identifies the exact local branch tip evaluated by a classification.
type Candidate struct {
	Branch string
	SHA    string
}

// RemoteState records whether a candidate still has a remote branch.
type RemoteState uint8

const (
	RemoteUnknown RemoteState = iota
	RemotePresent
	RemoteAbsent
)

// AncestorState records whether a candidate tip is reachable from the exact
// freshly observed default-branch tip.
type AncestorState uint8

const (
	AncestorUnknown AncestorState = iota
	AncestorNo
	AncestorYes
)

// MergeState records forge evidence for a branch. MergeYes and MergeContained
// are exact evidence and can authorize cleanup; MergeAncestor is display-only
// historical evidence.
type MergeState uint8

const (
	MergeUnknown MergeState = iota
	MergeNo
	MergeYes
	// MergeContained means the tip is reachable from a merged pull-request
	// head. The merge carried every commit on the tip, so nothing local is
	// lost by removing it. This is how a branch looks when its pull request
	// was updated on the remote -- typically by merging the default branch
	// into it -- and the local checkout never pulled that final head.
	MergeContained
	MergeAncestor
)

// TipState records whether the local branch changed while evidence was read.
type TipState uint8

const (
	TipStable TipState = iota
	TipChanged
)

// Evidence is the normalized immutable observation for one candidate. It
// contains no cleanup decision and is safe to evaluate without I/O.
type Evidence struct {
	Candidate Candidate
	Remote    RemoteState
	Ancestor  AncestorState
	Merge     MergeState
	Tip       TipState
}

// Snapshot is a fresh, normalized view of the default branch and candidates.
// Candidate ordering is retained by Evidence.
type Snapshot struct {
	DefaultSHA string
	Candidates []Evidence
}

// Diagnostic describes a conservative gap in optional forge verification.
type Diagnostic struct {
	Operation string
	Err       error
}

// String returns a stable user-facing description.
func (d Diagnostic) String() string {
	if d.Err == nil {
		return d.Operation
	}
	return fmt.Sprintf("%s: %v", d.Operation, d.Err)
}

// Result separates informational merged candidates from exact branch tips
// authorized for cleanup. All omitted requested branches must be retained.
type Result struct {
	Merged      []Candidate
	Cleanable   []Candidate
	Diagnostics []Diagnostic
}

// ClassifierFunc resolves and classifies branches relative to a default branch.
type ClassifierFunc func(defaultBranch string, branches []string) (Result, error)
