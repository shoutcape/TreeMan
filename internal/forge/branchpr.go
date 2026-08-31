package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// BranchPR pairs a remote branch with the open PR whose head it is.
type BranchPR struct {
	Branch BranchInfo
	PR     PRInfo
	HasPR  bool
}

// branchGroupBuffer lets enrichment keep running while the consumer is still
// rendering earlier groups.
const branchGroupBuffer = 16

// StreamGitHubBranchPRs delivers every branch of a GitHub repository that the
// caller wants to see, each paired with the open PR whose head it is, in
// groups as they resolve. No branch is delivered twice.
//
// keep decides which branches are worth showing. It is asked before a branch
// is enriched, so a branch the caller will not render costs no query, and it
// is called from more than one goroutine — it must be safe to call
// concurrently and must not read state onGroup writes.
//
// Two sources feed the stream. A preview query returns the first branches
// with their PRs in one round trip, which is what puts rows on screen before
// the branch list has been paginated; the full branch list is paginated and
// enriched concurrently. Whichever source has a group ready is delivered
// first, so neither holds the other's rows back. A failed preview costs
// nothing but the head start: the full list carries the same branches.
//
// Everything this starts runs under ctx and is joined before it returns, so a
// caller that stops reading stops the queries it was waiting on. onGroup runs
// on the calling goroutine; returning an error from it ends the stream.
func StreamGitHubBranchPRs(ctx context.Context, repoSlug string, keep func(BranchInfo) bool, onGroup func([]BranchPR) error) error {
	ctx, cancel := context.WithCancel(ctx)
	// Both sources stop on the context and never block on a send once it is
	// cancelled, so cancelling and waiting joins them however this returns:
	// nothing started here outlives the call it was feeding.
	var sources sync.WaitGroup
	sources.Add(2)
	defer func() {
		cancel()
		sources.Wait()
	}()

	previews := make(chan []BranchPR, 1)
	go func() {
		defer sources.Done()
		defer close(previews)
		if group, err := githubBranchPRPreview(ctx, repoSlug); err == nil {
			previews <- group
		}
	}()

	groups := make(chan []BranchPR, branchGroupBuffer)
	enriched := make(chan error, 1)
	go func() {
		defer sources.Done()
		defer close(groups)
		enriched <- githubBranchPRs(ctx, repoSlug, func(batchCtx context.Context, add func([]BranchInfo) error) error {
			return StreamBranchBatches(batchCtx, GitHub, repoSlug, "", func(batch []BranchInfo) error {
				return add(keptBranches(batch, keep))
			})
		}, func(group []BranchPR) error {
			select {
			case groups <- group:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()

	// The enriched list is the complete one, so it decides when the caller has
	// everything; a preview still in flight when it ends can only repeat
	// branches already delivered, and is stopped by the deferred cancel rather
	// than waited on.
	seen := make(map[string]struct{})
	for groups != nil {
		var group []BranchPR
		select {
		case preview, open := <-previews:
			if !open {
				// A nil channel never fires again, which leaves the enriched
				// groups as the only case.
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
		fresh := unseenBranchPRs(group, keep, seen)
		if len(fresh) == 0 {
			continue
		}
		if err := onGroup(fresh); err != nil {
			return err
		}
	}
	return <-enriched
}

// keptBranches drops the branches the caller does not want before they are
// enriched.
func keptBranches(branches []BranchInfo, keep func(BranchInfo) bool) []BranchInfo {
	kept := branches[:0:0]
	for _, branch := range branches {
		if keep(branch) {
			kept = append(kept, branch)
		}
	}
	return kept
}

// unseenBranchPRs returns the entries of group the caller may still be shown,
// recording them as delivered. The two sources overlap, so this is what keeps
// a branch from being delivered twice.
func unseenBranchPRs(group []BranchPR, keep func(BranchInfo) bool, seen map[string]struct{}) []BranchPR {
	fresh := group[:0:0]
	for _, entry := range group {
		name := entry.Branch.Name
		if _, delivered := seen[name]; delivered || name == "" || !keep(entry.Branch) {
			continue
		}
		seen[name] = struct{}{}
		fresh = append(fresh, entry)
	}
	return fresh
}

const (
	// githubBranchPreviewLimit is how many branches the preview query returns.
	// It exists to put rows on screen before the REST branch list lands, so it
	// is sized to stay inside one fast round trip (~0.6s).
	githubBranchPreviewLimit = 20

	// githubRefBatchSize is how many refs one enrichment query asks about.
	// Query time grows with the ref count — against payloadcms/payload it is
	// roughly 0.35s fixed plus 17ms per ref — so smaller batches reach the
	// picker sooner. 25 refs lands the first batch in ~0.7s.
	githubRefBatchSize = 25

	// githubRefBatchQueue bounds how many dispatched batches may wait for
	// their turn to be delivered, so a slow consumer pushes back on the
	// branch list instead of letting queries pile up unboundedly.
	githubRefBatchQueue = 64

	// githubRefBatchConcurrency bounds how many enrichment queries are in
	// flight. Batches overlap almost perfectly, so this sets completion time:
	// enriching 649 branches takes ~3.4s at 8, ~1.7s at 16 and ~1.2s
	// unbounded. Time to the first batch is flat at ~0.7s either way.
	githubRefBatchConcurrency = 16
)

// githubGraphQLFunc is the shape of the stubbable GraphQL entry point.
type githubGraphQLFunc func(ctx context.Context, query string, variables map[string]string) ([]byte, error)

// githubBranchPRPreview returns the alphabetically first branches together
// with their open PRs in a single query.
//
// It exists so the branch picker can show rows without waiting for the REST
// branch list. GitHub orders refs and the REST branch list the same way, so
// the result is a prefix of the first REST page and the caller can drop the
// duplicates as the full list arrives.
func githubBranchPRPreview(ctx context.Context, repoSlug string) ([]BranchPR, error) {
	owner, name, err := splitRepoSlug(repoSlug)
	if err != nil {
		return nil, err
	}
	out, err := githubGraphQLCall(ctx, githubBranchPreviewQuery, map[string]string{"owner": owner, "name": name})
	if err != nil {
		return nil, err
	}
	return parseGitHubBranchPreview(out)
}

var githubBranchPreviewQuery = fmt.Sprintf(`query($owner: String!, $name: String!) {
  repository(owner: $owner, name: $name) {
    refs(refPrefix: "refs/heads/", first: %d, orderBy: {field: ALPHABETICAL, direction: ASC}) {
      nodes {
        name
        target { ... on Commit { committedDate } }
        associatedPullRequests(first: 1, states: OPEN) { nodes { number title } }
      }
    }
  }
}`, githubBranchPreviewLimit)

func parseGitHubBranchPreview(data []byte) ([]BranchPR, error) {
	var response struct {
		Data *struct {
			Repository *struct {
				Refs *struct {
					Nodes []*struct {
						Name   string `json:"name"`
						Target *struct {
							CommittedDate string `json:"committedDate"`
						} `json:"target"`
						AssociatedPullRequests associatedPRs `json:"associatedPullRequests"`
					} `json:"nodes"`
				} `json:"refs"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("gh: parsing branch preview: %w", err)
	}
	if len(response.Errors) > 0 {
		return nil, fmt.Errorf("gh: branch preview failed: %s", response.Errors[0].Message)
	}
	if response.Data == nil || response.Data.Repository == nil || response.Data.Repository.Refs == nil {
		return nil, errors.New("gh: branch preview lacks repository data")
	}

	previews := make([]BranchPR, 0, len(response.Data.Repository.Refs.Nodes))
	for _, node := range response.Data.Repository.Refs.Nodes {
		if node == nil || node.Name == "" {
			return nil, errors.New("gh: branch preview contains an unnamed ref")
		}
		date := ""
		if node.Target != nil {
			date = formatRelativeDate(node.Target.CommittedDate)
		}
		previews = append(previews, newBranchPR(BranchInfo{Name: node.Name, Date: date}, node.AssociatedPullRequests))
	}
	return previews, nil
}

// githubBranchPRs pairs each branch with the open PR whose head it is, handing
// groups to onGroup in the order produce emitted them.
//
// The PR column used to come from the whole open-PR list, which meant no row
// could render until that list had been walked, and which mis-attributed a
// fork's PR to a same-named branch in this repository. Asking each ref for its
// own PRs avoids both problems. One query per ref would be far too slow, so
// refs are batched and the batches run concurrently.
//
// produce feeds branches in as they arrive — its caller is usually still
// paginating the REST branch list — so enriching the first branches overlaps
// fetching the last ones. It is handed the context this call derives, so the
// branch list it is walking stops with the enrichment it feeds.
func githubBranchPRs(ctx context.Context, repoSlug string, produce func(ctx context.Context, add func([]BranchInfo) error) error, onGroup func([]BranchPR) error) error {
	owner, name, err := splitRepoSlug(repoSlug)
	if err != nil {
		return err
	}

	// Every goroutine started here runs under this context, so returning —
	// however it happens — kills the queries still in flight instead of
	// fetching PRs for branches nobody will see.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Resolving the client once keeps the batch goroutines off package state.
	graphql := githubGraphQLCall

	type batchResult struct {
		group []BranchPR
		err   error
	}
	// Each dispatched batch queues its own result channel, so groups reach
	// onGroup in branch order however the queries interleave.
	queued := make(chan chan batchResult, githubRefBatchQueue)
	limiter := make(chan struct{}, githubRefBatchConcurrency)
	var batches sync.WaitGroup

	dispatch := func(batch []BranchInfo) error {
		// Buffered so a batch whose result is never read still finishes.
		result := make(chan batchResult, 1)
		select {
		case queued <- result:
		case <-ctx.Done():
			return ctx.Err()
		}
		batches.Add(1)
		go func() {
			defer batches.Done()
			select {
			case limiter <- struct{}{}:
			case <-ctx.Done():
				result <- batchResult{err: ctx.Err()}
				return
			}
			defer func() { <-limiter }()
			group, err := githubRefBatch(ctx, graphql, owner, name, batch)
			result <- batchResult{group: group, err: err}
		}()
		return nil
	}

	produced := make(chan error, 1)
	go func() {
		defer close(queued)
		var buffer []BranchInfo
		err := produce(ctx, func(branches []BranchInfo) error {
			buffer = append(buffer, branches...)
			for len(buffer) >= githubRefBatchSize {
				batch := make([]BranchInfo, githubRefBatchSize)
				copy(batch, buffer[:githubRefBatchSize])
				if err := dispatch(batch); err != nil {
					return err
				}
				buffer = buffer[githubRefBatchSize:]
			}
			return nil
		})
		if err == nil && len(buffer) > 0 {
			err = dispatch(buffer)
		}
		produced <- err
	}()

	// Every dispatched batch either delivers into its own buffered channel or
	// gives up on ctx, so this can only block on work that is still coming.
	var deliverErr error
	for result := range queued {
		batch := <-result
		if batch.err != nil {
			deliverErr = batch.err
			break
		}
		if err := onGroup(batch.group); err != nil {
			deliverErr = err
			break
		}
	}
	// Stop the producer and the batches still running, then drain the queue so
	// the producer is never left blocked on a slot, and join it: no goroutine
	// started here outlives this call.
	cancel()
	for range queued {
	}
	produceErr := <-produced
	batches.Wait()
	if deliverErr != nil {
		return deliverErr
	}
	return produceErr
}

// githubRefBatch asks for one batch of refs in a single aliased query. Ref
// names travel as GraphQL variables rather than being spliced into the query
// text, so a branch name can never alter the query.
func githubRefBatch(ctx context.Context, graphql githubGraphQLFunc, owner, name string, batch []BranchInfo) ([]BranchPR, error) {
	variables := map[string]string{"owner": owner, "name": name}
	for i, branch := range batch {
		variables[refVariable(i)] = "refs/heads/" + branch.Name
	}
	out, err := graphql(ctx, githubRefBatchQuery(len(batch)), variables)
	if err != nil {
		return nil, err
	}
	return parseGitHubRefBatch(out, batch)
}

func refVariable(index int) string { return "ref" + strconv.Itoa(index) }
func refAlias(index int) string    { return "branch" + strconv.Itoa(index) }

func githubRefBatchQuery(size int) string {
	var query strings.Builder
	query.WriteString("query($owner: String!, $name: String!")
	for i := range size {
		fmt.Fprintf(&query, ", $%s: String!", refVariable(i))
	}
	query.WriteString(") {\n  repository(owner: $owner, name: $name) {\n")
	for i := range size {
		fmt.Fprintf(&query, "    %s: ref(qualifiedName: $%s) { ...openPRs }\n", refAlias(i), refVariable(i))
	}
	query.WriteString("  }\n}\n")
	query.WriteString("fragment openPRs on Ref { associatedPullRequests(first: 1, states: OPEN) { nodes { number title } } }")
	return query.String()
}

type associatedPRs struct {
	Nodes []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
	} `json:"nodes"`
}

func newBranchPR(branch BranchInfo, prs associatedPRs) BranchPR {
	result := BranchPR{Branch: branch}
	for _, node := range prs.Nodes {
		if node.Number > 0 {
			result.PR = PRInfo{Number: node.Number, Title: node.Title, Branch: branch.Name}
			result.HasPR = true
			break
		}
	}
	return result
}

// parseGitHubRefBatch maps each alias back to the branch it was built from, so
// the group keeps the caller's order. A ref that resolved to null is kept
// without a PR: the branch exists in the list even if the ref lookup missed.
func parseGitHubRefBatch(data []byte, batch []BranchInfo) ([]BranchPR, error) {
	var response struct {
		Data *struct {
			Repository map[string]*struct {
				AssociatedPullRequests associatedPRs `json:"associatedPullRequests"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("gh: parsing branch PR lookup: %w", err)
	}
	if len(response.Errors) > 0 {
		return nil, fmt.Errorf("gh: branch PR lookup failed: %s", response.Errors[0].Message)
	}
	if response.Data == nil || response.Data.Repository == nil {
		return nil, errors.New("gh: branch PR lookup lacks repository data")
	}

	group := make([]BranchPR, 0, len(batch))
	for i, branch := range batch {
		node, ok := response.Data.Repository[refAlias(i)]
		if !ok {
			return nil, fmt.Errorf("gh: branch PR lookup is missing a result for %q", branch.Name)
		}
		if node == nil {
			group = append(group, BranchPR{Branch: branch})
			continue
		}
		group = append(group, newBranchPR(branch, node.AssociatedPullRequests))
	}
	return group, nil
}

// splitRepoSlug splits an "owner/name" repository slug. A slug carrying any
// further path segment is rejected: it does not name a repository.
func splitRepoSlug(repoSlug string) (owner, name string, err error) {
	owner, name, ok := strings.Cut(repoSlug, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", "", fmt.Errorf("invalid GitHub repository %q", repoSlug)
	}
	return owner, name, nil
}
