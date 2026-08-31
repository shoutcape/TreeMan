package cmd

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/shoutcape/treeman/internal/forge"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
)

// branchSources are the queries one branch stream runs.
//
// They are values a stream is built with rather than package state it reads,
// so a test hands a stream its own sources instead of reassigning globals that
// the stream's goroutines are still reading.
type branchSources struct {
	localBranches func() (map[string]struct{}, error)
	defaultBranch func() (string, error)
	branchPRs     func(ctx context.Context, repoSlug string, keep func(forge.BranchInfo) bool, onGroup func([]forge.BranchPR) error) error
}

// githubBranchSources is what the picker runs against a real repository.
func githubBranchSources() branchSources {
	return branchSources{
		localBranches: git.LocalBranchNames,
		defaultBranch: branchPickerDefaultBranch,
		branchPRs:     forge.StreamGitHubBranchPRs,
	}
}

// pickGitHubBranch opens the picker before the branch list exists and feeds it
// rows as they are enriched with their open PR.
func pickGitHubBranch(cmd *cobra.Command, repoSlug, query string) (branchSelection, error) {
	if !canInteract(cmd) {
		return branchSelection{}, fmt.Errorf("interactive selection is unavailable; pass an exact branch name")
	}
	if _, err := exec.LookPath("fzf"); err != nil {
		if query != "" {
			return branchSelection{}, fmt.Errorf("no exact match for %q and fzf is not installed for interactive selection", query)
		}
		return branchSelection{}, fmt.Errorf("fzf is required to pick a remote branch; pass an exact branch name or install fzf")
	}

	render := commandRenderer(cmd)
	stream, err := newBranchStream(render, githubBranchSources())
	if err != nil {
		return branchSelection{}, err
	}
	index, count, err := runStreamingPicker(cmd, pickerRequest{
		label:  " remote branches ",
		prompt: "branch > ",
		query:  query,
		header: render.BranchHeader(),
	}, stream.produce(repoSlug))
	if err != nil {
		if errors.Is(err, errPickerCancelled) && count == 0 {
			return branchSelection{}, fmt.Errorf("no remote branches available (all already exist locally or only default branch found)")
		}
		return branchSelection{}, err
	}
	return branchSelection{branch: stream.branches[index], prMap: stream.prMap}, nil
}

// branchStream renders the branch/PR pairs the forge delivers into picker
// rows, keeping the branches it emitted so the selected row index can be
// resolved back to a branch.
//
// It decides which branches are worth showing; assembling them is the forge's
// job, so nothing here starts a query or a goroutine of its own.
type branchStream struct {
	render        ui.Renderer
	branchPRs     func(ctx context.Context, repoSlug string, keep func(forge.BranchInfo) bool, onGroup func([]forge.BranchPR) error) error
	defaultBranch string
	localBranches map[string]struct{}
	branches      []forge.BranchInfo
	prMap         map[string]forge.PRInfo
}

func newBranchStream(render ui.Renderer, sources branchSources) (*branchStream, error) {
	localBranches, err := sources.localBranches()
	if err != nil {
		return nil, err
	}
	// A repository with no origin/HEAD still lists fine; it just cannot hide
	// its default branch.
	defaultBranch, _ := sources.defaultBranch()
	return &branchStream{
		render:        render,
		branchPRs:     sources.branchPRs,
		defaultBranch: defaultBranch,
		localBranches: localBranches,
		prMap:         make(map[string]forge.PRInfo),
	}, nil
}

// offered reports whether the picker may show a branch. It reads only fields
// fixed at construction, so the forge's branch-list goroutine can call it to
// skip branches before they are enriched.
func (s *branchStream) offered(branch forge.BranchInfo) bool {
	if branch.Name == "" || branch.Name == s.defaultBranch {
		return false
	}
	_, existsLocally := s.localBranches[branch.Name]
	return !existsLocally
}

// emit writes one row per branch in the group.
func (s *branchStream) emit(group []forge.BranchPR, write func(string) error) error {
	for _, entry := range group {
		name := entry.Branch.Name
		s.branches = append(s.branches, entry.Branch)

		number := 0
		if entry.HasPR {
			s.prMap[name] = entry.PR
			number = entry.PR.Number
		}
		if err := write(s.render.BranchRow(name, entry.Branch.Date, number)); err != nil {
			return err
		}
	}
	return nil
}

// produce streams every branch the picker can offer.
//
// The forge delivers each branch once, already paired with its open PR, under
// the picker's context — so a picker that closes early stops the queries
// rather than leaving them running for rows nobody will see.
func (s *branchStream) produce(repoSlug string) pickerProducer {
	return func(ctx context.Context, write func(string) error) error {
		// A row that could not be written is the picker closing, not the forge
		// failing, so it is reported as itself rather than as a list failure.
		var writeErr error
		err := s.branchPRs(ctx, repoSlug, s.offered, func(group []forge.BranchPR) error {
			writeErr = s.emit(group, write)
			return writeErr
		})
		if writeErr != nil {
			return writeErr
		}
		if err != nil {
			return fmt.Errorf("failed to list remote branches: %w", err)
		}
		return nil
	}
}
