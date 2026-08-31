package cmd

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"

	"github.com/shoutcape/treeman/internal/forge"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
)

// The GitHub branch picker's forge and git calls, in one place so tests can
// stub them.
var (
	branchPickerPreview       = forge.GitHubBranchPRPreview
	branchPickerBranchPRs     = forge.GitHubBranchPRs
	branchPickerBranchBatches = forge.StreamBranchBatches
	branchPickerLocalBranches = git.LocalBranchNames
)

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
	stream, err := newBranchStream(render)
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

// branchStream renders branch/PR pairs into picker rows, keeping the branches
// it emitted so the selected row index can be resolved back to a branch.
//
// Rows arrive from two sources that overlap — a preview query and the enriched
// branch list — so it drops branches it has already shown.
type branchStream struct {
	render        ui.Renderer
	defaultBranch string
	localBranches map[string]struct{}
	seen          map[string]struct{}
	branches      []forge.BranchInfo
	prMap         map[string]forge.PRInfo
}

func newBranchStream(render ui.Renderer) (*branchStream, error) {
	localBranches, err := branchPickerLocalBranches()
	if err != nil {
		return nil, err
	}
	// A repository with no origin/HEAD still lists fine; it just cannot hide
	// its default branch.
	defaultBranch, _ := branchPickerDefaultBranch()
	return &branchStream{
		render:        render,
		defaultBranch: defaultBranch,
		localBranches: localBranches,
		seen:          make(map[string]struct{}),
		prMap:         make(map[string]forge.PRInfo),
	}, nil
}

// offered reports whether the picker may show a branch. It reads only fields
// fixed at construction, so the branch-list goroutine can call it to skip
// branches before they are enriched.
func (s *branchStream) offered(name string) bool {
	if name == "" || name == s.defaultBranch {
		return false
	}
	_, existsLocally := s.localBranches[name]
	return !existsLocally
}

func (s *branchStream) keep(branches []forge.BranchInfo) []forge.BranchInfo {
	kept := branches[:0:0]
	for _, branch := range branches {
		if s.offered(branch.Name) {
			kept = append(kept, branch)
		}
	}
	return kept
}

// emit writes one row per branch the picker has not shown yet.
func (s *branchStream) emit(group []forge.BranchPR, write func(string) error) error {
	for _, entry := range group {
		name := entry.Branch.Name
		if !s.offered(name) {
			continue
		}
		if _, shown := s.seen[name]; shown {
			continue
		}
		s.seen[name] = struct{}{}
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

// branchGroupBuffer lets enrichment keep running while the picker is still
// writing earlier rows.
const branchGroupBuffer = 16

// produce streams every branch the picker can offer.
//
// A preview query returns the first branches with their PRs in one round trip,
// which is what puts rows on screen before the branch list has been paginated.
// The full list is enriched concurrently, and whichever source has a group
// ready is written first, so neither can hold the other's rows back. Branches
// the preview already covered are not shown twice.
//
// Both sources run under the picker's context, so a picker that closes early
// stops them rather than leaving queries running for rows nobody will see.
func (s *branchStream) produce(repoSlug string) pickerProducer {
	return func(ctx context.Context, write func(string) error) error {
		ctx, cancel := context.WithCancel(ctx)
		// Both sources stop on the context and never block on a send once it
		// is cancelled, so cancelling and waiting joins them however this
		// returns: nothing started here outlives the picker it was feeding.
		var sources sync.WaitGroup
		sources.Add(2)
		defer func() {
			cancel()
			sources.Wait()
		}()

		// A failed preview costs nothing but the head start: the full list is
		// already being fetched and carries the same branches.
		previews := make(chan []forge.BranchPR, 1)
		go func() {
			defer sources.Done()
			defer close(previews)
			if group, err := branchPickerPreview(ctx, repoSlug); err == nil {
				previews <- group
			}
		}()

		groups := make(chan []forge.BranchPR, branchGroupBuffer)
		enriched := make(chan error, 1)
		go func() {
			defer sources.Done()
			defer close(groups)
			enriched <- branchPickerBranchPRs(ctx, repoSlug, func(batchCtx context.Context, add func([]forge.BranchInfo) error) error {
				return branchPickerBranchBatches(batchCtx, forge.GitHub, repoSlug, "", func(batch []forge.BranchInfo) error {
					return add(s.keep(batch))
				})
			}, func(group []forge.BranchPR) error {
				select {
				case groups <- group:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()

		// The enriched list is the complete one, so it decides when the
		// picker has everything; a preview still in flight when it ends can
		// only repeat branches already shown, and is stopped by the deferred
		// cancel rather than waited on.
		for groups != nil {
			var group []forge.BranchPR
			select {
			case preview, open := <-previews:
				if !open {
					// A nil channel never fires again, which leaves the
					// enriched groups as the only case.
					previews = nil
					continue
				}
				group = preview
			case enrichedGroup, open := <-groups:
				if !open {
					groups = nil
					continue
				}
				group = enrichedGroup
			}
			if err := s.emit(group, write); err != nil {
				return err
			}
		}

		if err := <-enriched; err != nil {
			return fmt.Errorf("failed to list remote branches: %w", err)
		}
		return nil
	}
}
