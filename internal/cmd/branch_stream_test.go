package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/shoutcape/treeman/internal/forge"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func branchPR(name string, number int) forge.BranchPR {
	entry := forge.BranchPR{Branch: forge.BranchInfo{Name: name, Date: "1 day ago"}}
	if number > 0 {
		entry.PR = forge.PRInfo{Number: number, Title: "title " + name, Branch: name}
		entry.HasPR = true
	}
	return entry
}

// stubBranchStreamSources wires the picker to fixed preview and page data.
// Enrichment is replaced by a direct lookup so the streaming logic is tested
// without concurrency; forge covers the concurrent path.
func stubBranchStreamSources(t *testing.T, local []string, preview []forge.BranchPR, previewErr error, pages [][]forge.BranchInfo, prs map[string]int) {
	t.Helper()
	previousPreview := branchPickerPreview
	previousPRs := branchPickerBranchPRs
	previousPages := branchPickerBranchPages
	previousLocal := branchPickerLocalBranches
	previousDefault := branchPickerDefaultBranch
	t.Cleanup(func() {
		branchPickerPreview = previousPreview
		branchPickerBranchPRs = previousPRs
		branchPickerBranchPages = previousPages
		branchPickerLocalBranches = previousLocal
		branchPickerDefaultBranch = previousDefault
	})

	localBranches := make(map[string]struct{}, len(local))
	for _, name := range local {
		localBranches[name] = struct{}{}
	}
	branchPickerLocalBranches = func() (map[string]struct{}, error) { return localBranches, nil }
	branchPickerDefaultBranch = func() (string, error) { return "main", nil }
	branchPickerPreview = func(string) ([]forge.BranchPR, error) { return preview, previewErr }
	branchPickerBranchPages = func(_ forge.Type, _, _ string, onPage func([]forge.BranchInfo) error) error {
		for _, page := range pages {
			if err := onPage(page); err != nil {
				return err
			}
		}
		return nil
	}
	branchPickerBranchPRs = func(_ string, produce func(func([]forge.BranchInfo) error) error, onGroup func([]forge.BranchPR) error) error {
		return produce(func(branches []forge.BranchInfo) error {
			group := make([]forge.BranchPR, 0, len(branches))
			for _, branch := range branches {
				entry := forge.BranchPR{Branch: branch}
				if number, ok := prs[branch.Name]; ok {
					entry.PR = forge.PRInfo{Number: number, Title: "title " + branch.Name, Branch: branch.Name}
					entry.HasPR = true
				}
				group = append(group, entry)
			}
			return onGroup(group)
		})
	}
}

func streamedBranchNames(t *testing.T, stream *branchStream) []string {
	t.Helper()
	out := &bytes.Buffer{}
	_, err := streamPickerRows(out, "HEADER", stream.produce("owner/repo"))
	require.NoError(t, err)

	var names []string
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n")[1:] {
		names = append(names, strings.Fields(line)[0])
	}
	return names
}

func newTestBranchStream(t *testing.T) *branchStream {
	t.Helper()
	stream, err := newBranchStream(ui.Renderer{})
	require.NoError(t, err)
	return stream
}

func TestBranchStreamShowsPreviewRowsBeforeTheBranchList(t *testing.T) {
	stubBranchStreamSources(t, nil,
		[]forge.BranchPR{branchPR("alpha", 0), branchPR("beta", 7)}, nil,
		[][]forge.BranchInfo{{{Name: "alpha"}, {Name: "beta"}, {Name: "gamma"}}},
		map[string]int{"beta": 7, "gamma": 9},
	)
	stream := newTestBranchStream(t)

	names := streamedBranchNames(t, stream)

	// The preview arrives first and its branches are not repeated when the
	// full list catches up.
	assert.Equal(t, []string{"alpha", "beta", "gamma"}, names)
	assert.Equal(t, []string{"alpha", "beta", "gamma"}, branchNames(stream.branches))
	assert.Equal(t, 7, stream.prMap["beta"].Number)
	assert.Equal(t, 9, stream.prMap["gamma"].Number)
}

func TestBranchStreamHidesTheDefaultAndLocalBranches(t *testing.T) {
	stubBranchStreamSources(t, []string{"already-local"},
		[]forge.BranchPR{branchPR("main", 0), branchPR("already-local", 0), branchPR("wanted", 0)}, nil,
		[][]forge.BranchInfo{{{Name: "main"}, {Name: "already-local"}, {Name: "wanted"}, {Name: "other"}}},
		nil,
	)
	stream := newTestBranchStream(t)

	assert.Equal(t, []string{"wanted", "other"}, streamedBranchNames(t, stream))
}

// The picker maps a selected row back by index, so the branches it kept must
// line up with the rows it wrote.
func TestBranchStreamKeepsSelectableBranchesAlignedWithRows(t *testing.T) {
	stubBranchStreamSources(t, []string{"skipped"},
		[]forge.BranchPR{branchPR("alpha", 0)}, nil,
		[][]forge.BranchInfo{{{Name: "alpha"}, {Name: "skipped"}, {Name: "main"}}, {{Name: "omega"}}},
		nil,
	)
	stream := newTestBranchStream(t)

	out := &bytes.Buffer{}
	count, err := streamPickerRows(out, "HEADER", stream.produce("owner/repo"))

	require.NoError(t, err)
	assert.Equal(t, 2, count)
	require.Len(t, stream.branches, count)
	assert.Equal(t, "omega", stream.branches[1].Name)
}

// The preview is only a head start; losing it must not lose the list.
func TestBranchStreamFallsBackToTheBranchListWhenThePreviewFails(t *testing.T) {
	stubBranchStreamSources(t, nil, nil, errors.New("preview unavailable"),
		[][]forge.BranchInfo{{{Name: "alpha"}, {Name: "beta"}}},
		map[string]int{"beta": 3},
	)
	stream := newTestBranchStream(t)

	assert.Equal(t, []string{"alpha", "beta"}, streamedBranchNames(t, stream))
	assert.Equal(t, 3, stream.prMap["beta"].Number)
}

func TestBranchStreamReportsABranchListFailure(t *testing.T) {
	stubBranchStreamSources(t, nil, nil, errors.New("no preview"), nil, nil)
	previous := branchPickerBranchPRs
	t.Cleanup(func() { branchPickerBranchPRs = previous })
	branchPickerBranchPRs = func(string, func(func([]forge.BranchInfo) error) error, func([]forge.BranchPR) error) error {
		return errors.New("gh exploded")
	}
	stream := newTestBranchStream(t)

	_, err := streamPickerRows(&bytes.Buffer{}, "HEADER", stream.produce("owner/repo"))

	assert.ErrorContains(t, err, "failed to list remote branches")
	assert.ErrorContains(t, err, "gh exploded")
}

func branchNames(branches []forge.BranchInfo) []string {
	names := make([]string, 0, len(branches))
	for _, branch := range branches {
		names = append(names, branch.Name)
	}
	return names
}
