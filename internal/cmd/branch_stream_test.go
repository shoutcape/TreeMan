package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
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

// stubBranchSources hands the stream fixed groups instead of a forge. It
// honours keep the way forge.StreamGitHubBranchPRs does, so the picker's own
// filter is exercised; assembling and de-duplicating the groups is covered in
// forge, not here.
func stubBranchSources(local []string, groups [][]forge.BranchPR, streamErr error) branchSources {
	localBranches := make(map[string]struct{}, len(local))
	for _, name := range local {
		localBranches[name] = struct{}{}
	}
	return branchSources{
		localBranches: func() (map[string]struct{}, error) { return localBranches, nil },
		defaultBranch: func() (string, error) { return "main", nil },
		branchPRs: func(_ context.Context, _ string, keep func(forge.BranchInfo) bool, onGroup func([]forge.BranchPR) error) error {
			for _, group := range groups {
				kept := group[:0:0]
				for _, entry := range group {
					if keep(entry.Branch) {
						kept = append(kept, entry)
					}
				}
				if len(kept) == 0 {
					continue
				}
				if err := onGroup(kept); err != nil {
					return err
				}
			}
			return streamErr
		},
	}
}

func newTestBranchStream(t *testing.T, sources branchSources) *branchStream {
	t.Helper()
	stream, err := newBranchStream(ui.Renderer{}, sources)
	require.NoError(t, err)
	return stream
}

func streamedBranchNames(t *testing.T, stream *branchStream) []string {
	t.Helper()
	out := &bytes.Buffer{}
	_, err := streamPickerRows(context.Background(), out, "HEADER", stream.produce("owner/repo"))
	require.NoError(t, err)

	var names []string
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n")[1:] {
		names = append(names, strings.Fields(line)[0])
	}
	return names
}

// The picker asks for branches it can actually offer: the default branch and
// anything already checked out locally are not worth a row or a query.
func TestBranchStreamOffersOnlyBranchesWorthShowing(t *testing.T) {
	stream := newTestBranchStream(t, stubBranchSources([]string{"already-local"}, nil, nil))

	assert.True(t, stream.offered(forge.BranchInfo{Name: "wanted"}))
	assert.False(t, stream.offered(forge.BranchInfo{Name: "main"}), "the default branch")
	assert.False(t, stream.offered(forge.BranchInfo{Name: "already-local"}), "a branch that exists locally")
	assert.False(t, stream.offered(forge.BranchInfo{Name: ""}), "an unnamed branch")
}

func TestBranchStreamWritesARowPerDeliveredBranch(t *testing.T) {
	stream := newTestBranchStream(t, stubBranchSources([]string{"already-local"},
		[][]forge.BranchPR{
			{branchPR("main", 0), branchPR("already-local", 0), branchPR("alpha", 0)},
			{branchPR("beta", 7)},
		}, nil))

	assert.Equal(t, []string{"alpha", "beta"}, streamedBranchNames(t, stream))
	assert.Equal(t, 7, stream.prMap["beta"].Number)
	assert.Empty(t, stream.prMap["alpha"], "a branch without an open PR is not in the map")
}

// The picker maps a selected row back by index, so the branches it kept must
// line up with the rows it wrote.
func TestBranchStreamKeepsSelectableBranchesAlignedWithRows(t *testing.T) {
	stream := newTestBranchStream(t, stubBranchSources([]string{"skipped"},
		[][]forge.BranchPR{
			{branchPR("alpha", 0), branchPR("skipped", 0), branchPR("main", 0)},
			{branchPR("omega", 0)},
		}, nil))

	out := &bytes.Buffer{}
	count, err := streamPickerRows(context.Background(), out, "HEADER", stream.produce("owner/repo"))

	require.NoError(t, err)
	assert.Equal(t, 2, count)
	require.Len(t, stream.branches, count)
	assert.Equal(t, "omega", stream.branches[1].Name)
}

func TestBranchStreamReportsABranchListFailure(t *testing.T) {
	stream := newTestBranchStream(t, stubBranchSources(nil, nil, errors.New("gh exploded")))

	_, err := streamPickerRows(context.Background(), &bytes.Buffer{}, "HEADER", stream.produce("owner/repo"))

	assert.ErrorContains(t, err, "failed to list remote branches")
	assert.ErrorContains(t, err, "gh exploded")
}

// A row that cannot be written means the picker closed, which is not a forge
// failure and must not be dressed up as one — streamPickerRows recognises it
// and ends the stream cleanly.
func TestBranchStreamReportsAClosedPickerAsItself(t *testing.T) {
	stream := newTestBranchStream(t, stubBranchSources(nil,
		[][]forge.BranchPR{{branchPR("alpha", 0)}, {branchPR("beta", 0)}}, nil))

	count, err := streamPickerRows(context.Background(), closedWriter{}, "HEADER", stream.produce("owner/repo"))

	require.NoError(t, err)
	assert.Zero(t, count)
}

// closedWriter stands in for the pipe an fzf that has already exited leaves
// behind.
type closedWriter struct{}

func (closedWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }
