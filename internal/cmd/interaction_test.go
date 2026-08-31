package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/shoutcape/treeman/internal/forge"
	"github.com/shoutcape/treeman/internal/terminal"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfirmYNRejectsNonInteractiveInput(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("y\n"))
	cmd.SetErr(&bytes.Buffer{})

	confirmed, err := confirmYN(cmd, "Remove these worktrees and branches? [y/N] ")

	assert.False(t, confirmed)
	require.EqualError(t, err, "confirmation required; rerun with --yes")
}

func TestConfirmYNAcceptsInteractiveYes(t *testing.T) {
	previousCapabilities := terminalCapabilities
	terminalCapabilities = func(io.Reader, io.Writer) terminal.Capabilities {
		return terminal.Capabilities{Interactive: true}
	}
	t.Cleanup(func() { terminalCapabilities = previousCapabilities })

	stderr := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("Y\n"))
	cmd.SetErr(stderr)

	confirmed, err := confirmYN(cmd, "Remove these worktrees and branches? [y/N] ")

	require.NoError(t, err)
	assert.True(t, confirmed)
	assert.Equal(t, "Remove these worktrees and branches? [y/N] ", stderr.String())
}

func TestPickBranchBypassesInteractionForExactMatch(t *testing.T) {
	cmd := &cobra.Command{}
	branch, err := pickBranch(cmd, []forge.BranchInfo{{Name: "feature/exact"}}, "feature/exact", nil)

	require.NoError(t, err)
	assert.Equal(t, "feature/exact", branch.Name)
}

func TestPickBranchExplainsUnavailableInteraction(t *testing.T) {
	cmd := &cobra.Command{}
	_, err := pickBranch(cmd, []forge.BranchInfo{{Name: "feature/exact"}}, "", nil)

	require.EqualError(t, err, "interactive selection is unavailable; pass an exact branch name")
}

func TestLoadBranchPickerDataFiltersAndAssociatesConcurrently(t *testing.T) {
	previousBranchList := branchPickerBranchList
	previousPRList := branchPickerPRList
	previousBranchSHAs := branchPickerBranchSHAs
	previousDefaultBranch := branchPickerDefaultBranch
	t.Cleanup(func() {
		branchPickerBranchList = previousBranchList
		branchPickerPRList = previousPRList
		branchPickerBranchSHAs = previousBranchSHAs
		branchPickerDefaultBranch = previousDefaultBranch
	})

	branchStarted := make(chan struct{})
	prStarted := make(chan struct{})
	release := make(chan struct{})
	branchPickerBranchList = func(context.Context, forge.Type, string, string) ([]forge.BranchInfo, error) {
		close(branchStarted)
		<-release
		return []forge.BranchInfo{{Name: "main"}, {Name: "feature/existing"}, {Name: "feature/available"}}, nil
	}
	branchPickerPRList = func(context.Context, forge.Type, string, string) ([]forge.PRInfo, error) {
		close(prStarted)
		<-release
		return []forge.PRInfo{{Number: 42, Branch: "feature/available", Title: "Available"}}, nil
	}
	branchPickerDefaultBranch = func() (string, error) { return "main", nil }
	var branchNames []string
	branchPickerBranchSHAs = func(branches []string) (map[string]string, error) {
		branchNames = append([]string(nil), branches...)
		return map[string]string{"feature/existing": "abc123"}, nil
	}

	command := &cobra.Command{}
	command.SetErr(&bytes.Buffer{})
	result := make(chan struct {
		data branchPickerData
		err  error
	}, 1)
	go func() {
		data, err := loadBranchPickerDataForForge(command, forge.GitHub, "owner/repo", "github.com")
		result <- struct {
			data branchPickerData
			err  error
		}{data, err}
	}()

	<-branchStarted
	<-prStarted
	close(release)
	loaded := <-result

	require.NoError(t, loaded.err)
	assert.Equal(t, []string{"main", "feature/existing", "feature/available"}, branchNames)
	assert.Equal(t, []forge.BranchInfo{{Name: "feature/available"}}, loaded.data.branches)
	assert.Equal(t, forge.PRInfo{Number: 42, Branch: "feature/available", Title: "Available"}, loaded.data.prMap["feature/available"])
}

func TestLoadBranchPickerDataKeepsPRFailuresNonFatal(t *testing.T) {
	previousBranchList := branchPickerBranchList
	previousPRList := branchPickerPRList
	previousBranchSHAs := branchPickerBranchSHAs
	previousDefaultBranch := branchPickerDefaultBranch
	t.Cleanup(func() {
		branchPickerBranchList = previousBranchList
		branchPickerPRList = previousPRList
		branchPickerBranchSHAs = previousBranchSHAs
		branchPickerDefaultBranch = previousDefaultBranch
	})

	branchPickerBranchList = func(context.Context, forge.Type, string, string) ([]forge.BranchInfo, error) {
		return []forge.BranchInfo{{Name: "feature/available"}}, nil
	}
	branchPickerPRList = func(context.Context, forge.Type, string, string) ([]forge.PRInfo, error) {
		return nil, errors.New("API unavailable")
	}
	branchPickerDefaultBranch = func() (string, error) { return "main", nil }
	branchPickerBranchSHAs = func([]string) (map[string]string, error) { return map[string]string{}, nil }

	stderr := &bytes.Buffer{}
	command := &cobra.Command{}
	command.SetErr(stderr)
	data, err := loadBranchPickerDataForForge(command, forge.GitHub, "owner/repo", "github.com")

	require.NoError(t, err)
	assert.Equal(t, []forge.BranchInfo{{Name: "feature/available"}}, data.branches)
	assert.Empty(t, data.prMap)
	assert.Contains(t, stderr.String(), "could not fetch MRs/PRs: API unavailable")
}

func TestLoadBranchPickerDataFailsWhenBranchListingFails(t *testing.T) {
	previousBranchList := branchPickerBranchList
	previousPRList := branchPickerPRList
	t.Cleanup(func() {
		branchPickerBranchList = previousBranchList
		branchPickerPRList = previousPRList
	})

	branchPickerBranchList = func(context.Context, forge.Type, string, string) ([]forge.BranchInfo, error) {
		return nil, errors.New("API unavailable")
	}
	branchPickerPRList = func(context.Context, forge.Type, string, string) ([]forge.PRInfo, error) { return nil, nil }

	_, err := loadBranchPickerDataForForge(&cobra.Command{}, forge.GitHub, "owner/repo", "github.com")

	require.EqualError(t, err, "failed to list remote branches: API unavailable")
}

func TestLoadBranchPickerDataFailsWhenLocalBranchLookupFails(t *testing.T) {
	previousBranchList := branchPickerBranchList
	previousPRList := branchPickerPRList
	previousBranchSHAs := branchPickerBranchSHAs
	t.Cleanup(func() {
		branchPickerBranchList = previousBranchList
		branchPickerPRList = previousPRList
		branchPickerBranchSHAs = previousBranchSHAs
	})

	branchPickerBranchList = func(context.Context, forge.Type, string, string) ([]forge.BranchInfo, error) {
		return []forge.BranchInfo{{Name: "feature/available"}}, nil
	}
	branchPickerPRList = func(context.Context, forge.Type, string, string) ([]forge.PRInfo, error) { return nil, nil }
	branchPickerBranchSHAs = func([]string) (map[string]string, error) {
		return nil, errors.New("git failed")
	}

	_, err := loadBranchPickerDataForForge(&cobra.Command{}, forge.GitHub, "owner/repo", "github.com")

	require.EqualError(t, err, "git failed")
}

func TestTerminalCapabilitiesAreCachedPerCommandStream(t *testing.T) {
	previousCapabilities := terminalCapabilities
	var calls int
	terminalCapabilities = func(io.Reader, io.Writer) terminal.Capabilities {
		calls++
		return terminal.Capabilities{}
	}
	t.Cleanup(func() { terminalCapabilities = previousCapabilities })

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	_ = commandRenderer(cmd)
	_ = outputRenderer(cmd)
	assert.False(t, canInteract(cmd))

	assert.Equal(t, 2, calls)
}

func TestStreamReviewRowsEmitsEachBatchAsItArrives(t *testing.T) {
	previous := reviewPickerPRBatches
	t.Cleanup(func() { reviewPickerPRBatches = previous })

	var writtenWhenPageRequested []string
	out := &bytes.Buffer{}
	reviewPickerPRBatches = func(_ context.Context, _ forge.Type, _, _ string, onBatch func([]forge.PRInfo) error) error {
		writtenWhenPageRequested = append(writtenWhenPageRequested, out.String())
		if err := onBatch([]forge.PRInfo{{Number: 41, Branch: "feature/one", Title: "First"}}); err != nil {
			return err
		}
		writtenWhenPageRequested = append(writtenWhenPageRequested, out.String())
		return onBatch([]forge.PRInfo{{Number: 42, Branch: "feature/two", Title: "Second"}})
	}

	var prs []forge.PRInfo
	render := commandRenderer(&cobra.Command{})
	count, err := streamPickerRows(context.Background(), out, render.PRHeader(), streamReviewRows(render, reviewForge{}, &prs))

	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Equal(t, []forge.PRInfo{
		{Number: 41, Branch: "feature/one", Title: "First"},
		{Number: 42, Branch: "feature/two", Title: "Second"},
	}, prs)
	assert.Contains(t, out.String(), "41")
	assert.Contains(t, out.String(), "feature/two")

	// The first batch must already be on its way to fzf when the second is fetched.
	require.Len(t, writtenWhenPageRequested, 2)
	assert.NotContains(t, writtenWhenPageRequested[0], "41")
	assert.Contains(t, writtenWhenPageRequested[1], "41")
	assert.NotContains(t, writtenWhenPageRequested[1], "42")
}

func TestStreamReviewRowsReportsListingFailure(t *testing.T) {
	previous := reviewPickerPRBatches
	t.Cleanup(func() { reviewPickerPRBatches = previous })

	reviewPickerPRBatches = func(context.Context, forge.Type, string, string, func([]forge.PRInfo) error) error {
		return assert.AnError
	}

	var prs []forge.PRInfo
	render := commandRenderer(&cobra.Command{})
	_, err := streamPickerRows(context.Background(), &bytes.Buffer{}, render.PRHeader(), streamReviewRows(render, reviewForge{}, &prs))

	assert.ErrorContains(t, err, "failed to list open PRs/MRs")
	assert.ErrorIs(t, err, assert.AnError)
}

func TestStreamBranchRowsEmitsEveryBranchWithItsReview(t *testing.T) {
	out := &bytes.Buffer{}
	render := commandRenderer(&cobra.Command{})

	count, err := streamPickerRows(context.Background(), out, render.BranchHeader(), streamBranchRows(render,
		[]forge.BranchInfo{{Name: "feature/one", Date: "today"}, {Name: "feature/two", Date: "yesterday"}},
		map[string]forge.PRInfo{"feature/two": {Number: 42}},
	))

	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Contains(t, out.String(), "feature/one")
	assert.Contains(t, out.String(), "today")
	assert.Contains(t, out.String(), "feature/two")
	assert.Contains(t, out.String(), "#42")
	assert.Equal(t, 3, len(strings.Split(strings.TrimSpace(out.String()), "\n")))
}
