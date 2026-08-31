package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// refBatchReply builds the response GitHub returns for one aliased ref batch.
// Branches named "<name>-pr<number>" come back carrying that open PR.
func refBatchReply(variables map[string]string) []byte {
	repository := map[string]any{}
	for i := 0; ; i++ {
		qualified, ok := variables[refVariable(i)]
		if !ok {
			break
		}
		name := strings.TrimPrefix(qualified, "refs/heads/")
		_, suffix, hasPR := strings.Cut(name, "-pr")
		if !hasPR {
			repository[refAlias(i)] = map[string]any{
				"associatedPullRequests": map[string]any{"nodes": []any{}},
			}
			continue
		}
		number := 0
		fmt.Sscanf(suffix, "%d", &number)
		repository[refAlias(i)] = map[string]any{
			"associatedPullRequests": map[string]any{
				"nodes": []any{map[string]any{"number": number, "title": "title " + name}},
			},
		}
	}
	body, err := json.Marshal(map[string]any{"data": map[string]any{"repository": repository}})
	if err != nil {
		panic(err)
	}
	return body
}

func stubRefBatches(t *testing.T, before func(variables map[string]string)) *atomic.Int64 {
	t.Helper()
	previous := githubGraphQLCall
	calls := &atomic.Int64{}
	githubGraphQLCall = func(_ context.Context, _ string, variables map[string]string) ([]byte, error) {
		calls.Add(1)
		if before != nil {
			before(variables)
		}
		return refBatchReply(variables), nil
	}
	t.Cleanup(func() { githubGraphQLCall = previous })
	return calls
}

func namedBranches(names ...string) []BranchInfo {
	branches := make([]BranchInfo, 0, len(names))
	for _, name := range names {
		branches = append(branches, BranchInfo{Name: name, Date: "1 day ago"})
	}
	return branches
}

func collectBranchPRs(t *testing.T, repoSlug string, pages [][]BranchInfo) ([]BranchPR, error) {
	t.Helper()
	var collected []BranchPR
	err := githubBranchPRs(context.Background(), repoSlug, func(_ context.Context, add func([]BranchInfo) error) error {
		for _, page := range pages {
			if err := add(page); err != nil {
				return err
			}
		}
		return nil
	}, func(group []BranchPR) error {
		collected = append(collected, group...)
		return nil
	})
	return collected, err
}

func TestGitHubBranchPRsAttachesTheOpenPROfEachBranch(t *testing.T) {
	stubRefBatches(t, func(variables map[string]string) {
		assert.Equal(t, "owner", variables["owner"])
		assert.Equal(t, "repo", variables["name"])
		assert.Equal(t, "refs/heads/feature/one", variables["ref0"])
	})

	collected, err := collectBranchPRs(t, "owner/repo", [][]BranchInfo{
		namedBranches("feature/one", "feature/two-pr42"),
	})

	require.NoError(t, err)
	require.Len(t, collected, 2)
	assert.Equal(t, BranchInfo{Name: "feature/one", Date: "1 day ago"}, collected[0].Branch)
	assert.False(t, collected[0].HasPR)
	assert.True(t, collected[1].HasPR)
	assert.Equal(t, PRInfo{Number: 42, Title: "title feature/two-pr42", Branch: "feature/two-pr42"}, collected[1].PR)
}

// The picker numbers rows as they arrive, so a batch that finishes early must
// still wait its turn.
func TestGitHubBranchPRsKeepsBranchOrderWhenLaterBatchesFinishFirst(t *testing.T) {
	var first sync.Once
	stubRefBatches(t, func(variables map[string]string) {
		if variables[refVariable(0)] == "refs/heads/branch-000" {
			first.Do(func() { time.Sleep(50 * time.Millisecond) })
		}
	})

	branches := make([]BranchInfo, 0, githubRefBatchSize*2+3)
	for i := range githubRefBatchSize*2 + 3 {
		branches = append(branches, BranchInfo{Name: fmt.Sprintf("branch-%03d", i)})
	}

	collected, err := collectBranchPRs(t, "owner/repo", [][]BranchInfo{branches})

	require.NoError(t, err)
	require.Len(t, collected, len(branches))
	for i, entry := range collected {
		assert.Equal(t, fmt.Sprintf("branch-%03d", i), entry.Branch.Name)
	}
}

func TestGitHubBranchPRsSplitsBranchesIntoBatches(t *testing.T) {
	sizes := make(chan int, 8)
	stubRefBatches(t, func(variables map[string]string) {
		// owner and name are not refs.
		sizes <- len(variables) - 2
	})

	branches := make([]BranchInfo, 0, githubRefBatchSize+4)
	for i := range githubRefBatchSize + 4 {
		branches = append(branches, BranchInfo{Name: fmt.Sprintf("branch-%03d", i)})
	}

	_, err := collectBranchPRs(t, "owner/repo", [][]BranchInfo{branches})
	require.NoError(t, err)
	close(sizes)

	var observed []int
	for size := range sizes {
		observed = append(observed, size)
	}
	assert.ElementsMatch(t, []int{githubRefBatchSize, 4}, observed)
}

// Branches arrive while the REST branch list is still paginating, so partial
// pages must be buffered into full batches rather than queried one page at a
// time.
func TestGitHubBranchPRsBatchesAcrossSeveralPages(t *testing.T) {
	calls := stubRefBatches(t, nil)

	pages := make([][]BranchInfo, 0, githubRefBatchSize)
	for i := range githubRefBatchSize {
		pages = append(pages, namedBranches(fmt.Sprintf("branch-%03d", i)))
	}

	collected, err := collectBranchPRs(t, "owner/repo", pages)

	require.NoError(t, err)
	assert.Len(t, collected, githubRefBatchSize)
	assert.Equal(t, int64(1), calls.Load(), "one full batch should cost one query")
}

func TestGitHubBranchPRsStopsQueryingWhenTheConsumerFails(t *testing.T) {
	calls := stubRefBatches(t, func(map[string]string) { time.Sleep(10 * time.Millisecond) })

	// More batches than the dispatch queue holds, so the ones behind the
	// failure have not been handed to a goroutine yet when it happens.
	batches := githubRefBatchQueue + 40
	branches := make([]BranchInfo, 0, githubRefBatchSize*batches)
	for i := range githubRefBatchSize * batches {
		branches = append(branches, BranchInfo{Name: fmt.Sprintf("branch-%04d", i)})
	}

	stop := errors.New("picker closed")
	err := githubBranchPRs(context.Background(), "owner/repo", func(_ context.Context, add func([]BranchInfo) error) error {
		return add(branches)
	}, func([]BranchPR) error { return stop })

	assert.ErrorIs(t, err, stop)
	assert.Positive(t, calls.Load())
	assert.Less(t, calls.Load(), int64(batches), "batches queued behind the failure should be abandoned")
}

func TestGitHubBranchPRsReportsAProducerFailure(t *testing.T) {
	stubRefBatches(t, nil)

	broken := errors.New("branch list failed")
	err := githubBranchPRs(context.Background(), "owner/repo", func(_ context.Context, add func([]BranchInfo) error) error {
		if err := add(namedBranches("feature/one")); err != nil {
			return err
		}
		return broken
	}, func([]BranchPR) error { return nil })

	assert.ErrorIs(t, err, broken)
}

func TestGitHubBranchPRsRejectsAnInvalidRepository(t *testing.T) {
	for _, slug := range []string{"", "owner", "owner/", "/repo", "owner/repo/extra"} {
		err := githubBranchPRs(context.Background(), slug, func(context.Context, func([]BranchInfo) error) error { return nil }, func([]BranchPR) error { return nil })
		assert.ErrorContains(t, err, "invalid GitHub repository", "slug %q", slug)
	}
}

func TestGitHubRefBatchQueryPassesRefNamesAsVariables(t *testing.T) {
	query := githubRefBatchQuery(2)

	assert.Contains(t, query, "query($owner: String!, $name: String!, $ref0: String!, $ref1: String!)")
	assert.Contains(t, query, "branch0: ref(qualifiedName: $ref0)")
	assert.Contains(t, query, "branch1: ref(qualifiedName: $ref1)")
	assert.Contains(t, query, "associatedPullRequests(first: 1, states: OPEN)")
	// A branch name must never reach the query text, where it could alter it.
	assert.NotContains(t, query, "refs/heads/")
}

// A ref that resolved to null still names a branch the picker should offer.
func TestParseGitHubRefBatchKeepsBranchesWithoutARef(t *testing.T) {
	batch := namedBranches("gone", "kept-pr7")
	data := []byte(`{"data":{"repository":{"branch0":null,"branch1":{"associatedPullRequests":{"nodes":[{"number":7,"title":"seven"}]}}}}}`)

	group, err := parseGitHubRefBatch(data, batch)

	require.NoError(t, err)
	require.Len(t, group, 2)
	assert.Equal(t, "gone", group[0].Branch.Name)
	assert.False(t, group[0].HasPR)
	assert.True(t, group[1].HasPR)
	assert.Equal(t, 7, group[1].PR.Number)
}

func TestParseGitHubRefBatchRejectsIncompleteResponses(t *testing.T) {
	batch := namedBranches("one", "two")

	_, err := parseGitHubRefBatch([]byte(`{"data":{"repository":{"branch0":null}}}`), batch)
	assert.ErrorContains(t, err, `missing a result for "two"`)

	_, err = parseGitHubRefBatch([]byte(`{"errors":[{"message":"rate limited"}]}`), batch)
	assert.ErrorContains(t, err, "rate limited")

	_, err = parseGitHubRefBatch([]byte(`{"data":{}}`), batch)
	assert.ErrorContains(t, err, "lacks repository data")

	_, err = parseGitHubRefBatch([]byte(`not json`), batch)
	assert.ErrorContains(t, err, "parsing branch PR lookup")
}

func TestGitHubBranchPRPreviewReturnsBranchesWithTheirPRs(t *testing.T) {
	previous := githubGraphQLCall
	githubGraphQLCall = func(_ context.Context, query string, variables map[string]string) ([]byte, error) {
		assert.Contains(t, query, fmt.Sprintf("first: %d", githubBranchPreviewLimit))
		assert.Contains(t, query, "orderBy: {field: ALPHABETICAL, direction: ASC}")
		assert.Equal(t, map[string]string{"owner": "owner", "name": "repo"}, variables)
		return []byte(`{"data":{"repository":{"refs":{"nodes":[
			{"name":"alpha","target":{"committedDate":"2020-01-02T03:04:05Z"},"associatedPullRequests":{"nodes":[]}},
			{"name":"beta","target":{"committedDate":"2020-01-02T03:04:05Z"},"associatedPullRequests":{"nodes":[{"number":9,"title":"nine"}]}}
		]}}}}`), nil
	}
	t.Cleanup(func() { githubGraphQLCall = previous })

	preview, err := githubBranchPRPreview(context.Background(), "owner/repo")

	require.NoError(t, err)
	require.Len(t, preview, 2)
	assert.Equal(t, "alpha", preview[0].Branch.Name)
	assert.False(t, preview[0].HasPR)
	assert.NotEmpty(t, preview[0].Branch.Date)
	assert.Equal(t, PRInfo{Number: 9, Title: "nine", Branch: "beta"}, preview[1].PR)
}

func TestParseGitHubBranchPreviewRejectsIncompleteResponses(t *testing.T) {
	_, err := parseGitHubBranchPreview([]byte(`{"errors":[{"message":"boom"}]}`))
	assert.ErrorContains(t, err, "boom")

	_, err = parseGitHubBranchPreview([]byte(`{"data":{"repository":{}}}`))
	assert.ErrorContains(t, err, "lacks repository data")

	_, err = parseGitHubBranchPreview([]byte(`{"data":{"repository":{"refs":{"nodes":[{"name":""}]}}}}`))
	assert.ErrorContains(t, err, "unnamed ref")
}

// A picker the user closes cancels its context; the enrichment queries it
// started must end with it rather than run on for rows nobody will see.
func TestGitHubBranchPRsStopsWhenTheContextIsCancelled(t *testing.T) {
	previous := githubGraphQLCall
	t.Cleanup(func() { githubGraphQLCall = previous })

	var active atomic.Int64
	started := make(chan struct{}, 1)
	githubGraphQLCall = func(ctx context.Context, _ string, _ map[string]string) ([]byte, error) {
		active.Add(1)
		defer active.Add(-1)
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}

	branches := make([]BranchInfo, 0, githubRefBatchSize*4)
	for i := range githubRefBatchSize * 4 {
		branches = append(branches, BranchInfo{Name: fmt.Sprintf("branch-%03d", i)})
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- githubBranchPRs(ctx, "owner/repo", func(_ context.Context, add func([]BranchInfo) error) error {
			return add(branches)
		}, func([]BranchPR) error { return nil })
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("no enrichment batch started")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling did not stop the enrichment")
	}
	assert.Zero(t, active.Load(), "no batch worker may outlive GitHubBranchPRs")
}

// The branch list feeding enrichment runs under the context enrichment
// derives, so a failed batch stops the pagination that was still producing.
func TestGitHubBranchPRsStopsTheProducerAfterABatchFailure(t *testing.T) {
	previous := githubGraphQLCall
	t.Cleanup(func() { githubGraphQLCall = previous })
	githubGraphQLCall = func(context.Context, string, map[string]string) ([]byte, error) {
		return nil, assert.AnError
	}

	produced := make(chan struct{})
	err := githubBranchPRs(context.Background(), "owner/repo", func(ctx context.Context, add func([]BranchInfo) error) error {
		defer close(produced)
		for {
			if err := add(namedBranches("branch")); err != nil {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
	}, func([]BranchPR) error { return nil })

	require.ErrorIs(t, err, assert.AnError)
	<-produced
}

// previewReply builds the response the preview query returns. Branches named
// "<name>-pr<number>" come back carrying that open PR.
func previewReply(names []string) []byte {
	nodes := make([]any, 0, len(names))
	for _, name := range names {
		prs := []any{}
		if _, suffix, hasPR := strings.Cut(name, "-pr"); hasPR {
			number := 0
			fmt.Sscanf(suffix, "%d", &number)
			prs = append(prs, map[string]any{"number": number, "title": "title " + name})
		}
		nodes = append(nodes, map[string]any{
			"name":                   name,
			"target":                 map[string]any{"committedDate": "2026-08-01T10:00:00Z"},
			"associatedPullRequests": map[string]any{"nodes": prs},
		})
	}
	body, err := json.Marshal(map[string]any{
		"data": map[string]any{"repository": map[string]any{"refs": map[string]any{"nodes": nodes}}},
	})
	if err != nil {
		panic(err)
	}
	return body
}

// branchPageReply builds one REST branch-list page.
func branchPageReply(names []string) []byte {
	page := make([]any, 0, len(names))
	for _, name := range names {
		page = append(page, map[string]any{
			"name":   name,
			"commit": map[string]any{"commit": map[string]any{"committer": map[string]any{"date": "2026-08-01T10:00:00Z"}}},
		})
	}
	body, err := json.Marshal(page)
	if err != nil {
		panic(err)
	}
	return body
}

// stubBranchStreamSources answers every request StreamGitHubBranchPRs makes:
// the preview query, the REST branch pages, and the per-ref enrichment
// batches. Pages are walked with rel="next", so they are requested one at a
// time from the caller's goroutine.
func stubBranchStreamSources(t *testing.T, preview []string, previewErr error, pages [][]string) {
	t.Helper()
	previousGraphQL := githubGraphQLCall
	previousPage := githubPageCall
	t.Cleanup(func() {
		githubGraphQLCall = previousGraphQL
		githubPageCall = previousPage
	})

	githubGraphQLCall = func(_ context.Context, query string, variables map[string]string) ([]byte, error) {
		if strings.Contains(query, "refs(refPrefix") {
			if previewErr != nil {
				return nil, previewErr
			}
			return previewReply(preview), nil
		}
		return refBatchReply(variables), nil
	}

	remaining := pages
	githubPageCall = func(_ context.Context, _ string) (string, []byte, error) {
		if len(remaining) == 0 {
			return "", []byte(`[]`), nil
		}
		page := remaining[0]
		remaining = remaining[1:]
		link := ""
		if len(remaining) > 0 {
			link = `<https://api.github.com/next>; rel="next"`
		}
		return link, branchPageReply(page), nil
	}
}

func collectBranchStream(ctx context.Context, keep func(BranchInfo) bool) ([]BranchPR, error) {
	if keep == nil {
		keep = func(BranchInfo) bool { return true }
	}
	var delivered []BranchPR
	err := StreamGitHubBranchPRs(ctx, "owner/repo", keep, func(group []BranchPR) error {
		delivered = append(delivered, group...)
		return nil
	})
	return delivered, err
}

func deliveredNames(delivered []BranchPR) []string {
	names := make([]string, 0, len(delivered))
	for _, entry := range delivered {
		names = append(names, entry.Branch.Name)
	}
	return names
}

// The preview and the branch list overlap on purpose, so the same branch must
// not reach the caller from both.
func TestStreamGitHubBranchPRsDeliversEachBranchOnceWithItsPR(t *testing.T) {
	stubBranchStreamSources(t, []string{"alpha", "beta-pr7"}, nil,
		[][]string{{"alpha", "beta-pr7", "gamma-pr9"}})

	delivered, err := collectBranchStream(context.Background(), nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"alpha", "beta-pr7", "gamma-pr9"}, deliveredNames(delivered))
	assert.False(t, delivered[0].HasPR)
	assert.Equal(t, 7, delivered[1].PR.Number)
	assert.Equal(t, 9, delivered[2].PR.Number)
	assert.NotEmpty(t, delivered[0].Branch.Date)
}

// A branch the caller will not show must cost no enrichment query, whichever
// source it came from.
func TestStreamGitHubBranchPRsSkipsBranchesTheCallerRejects(t *testing.T) {
	stubBranchStreamSources(t, []string{"skipped", "wanted"}, nil,
		[][]string{{"skipped", "wanted", "other"}})

	var enriched sync.Mutex
	var asked []string
	previous := githubGraphQLCall
	githubGraphQLCall = func(ctx context.Context, query string, variables map[string]string) ([]byte, error) {
		if strings.Contains(query, "ref(qualifiedName") {
			enriched.Lock()
			for i := 0; ; i++ {
				qualified, ok := variables[refVariable(i)]
				if !ok {
					break
				}
				asked = append(asked, strings.TrimPrefix(qualified, "refs/heads/"))
			}
			enriched.Unlock()
		}
		return previous(ctx, query, variables)
	}

	delivered, err := collectBranchStream(context.Background(), func(branch BranchInfo) bool {
		return branch.Name != "skipped"
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"wanted", "other"}, deliveredNames(delivered))
	enriched.Lock()
	defer enriched.Unlock()
	assert.NotContains(t, asked, "skipped", "a rejected branch was enriched anyway")
}

// The preview is only a head start; losing it must not lose the list.
func TestStreamGitHubBranchPRsFallsBackToTheBranchListWhenThePreviewFails(t *testing.T) {
	stubBranchStreamSources(t, nil, errors.New("preview unavailable"),
		[][]string{{"alpha"}, {"beta-pr3"}})

	delivered, err := collectBranchStream(context.Background(), nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"alpha", "beta-pr3"}, deliveredNames(delivered))
	assert.Equal(t, 3, delivered[1].PR.Number)
}

// The preview is a head start, not a gate: rows the branch list already has
// must reach the caller while the preview is still in flight.
func TestStreamGitHubBranchPRsDoesNotLetASlowPreviewHoldBackEnrichedRows(t *testing.T) {
	stubBranchStreamSources(t, nil, nil, [][]string{{"alpha-pr4"}})
	previous := githubGraphQLCall
	githubGraphQLCall = func(ctx context.Context, query string, variables map[string]string) ([]byte, error) {
		if strings.Contains(query, "refs(refPrefix") {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return previous(ctx, query, variables)
	}

	groups := make(chan []BranchPR, 4)
	done := make(chan error, 1)
	go func() {
		done <- StreamGitHubBranchPRs(context.Background(), "owner/repo",
			func(BranchInfo) bool { return true },
			func(group []BranchPR) error {
				groups <- group
				return nil
			})
	}()

	select {
	case group := <-groups:
		assert.Equal(t, []string{"alpha-pr4"}, deliveredNames(group))
	case <-time.After(5 * time.Second):
		t.Fatal("the preview held back a row the branch list already had")
	}
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("the stream waited for a preview that adds nothing")
	}
}

// A caller that stops reading cancels the context, which has to end both
// sources rather than block on whichever is slowest.
func TestStreamGitHubBranchPRsStopsWhenTheContextIsCancelled(t *testing.T) {
	previousGraphQL := githubGraphQLCall
	previousPage := githubPageCall
	t.Cleanup(func() {
		githubGraphQLCall = previousGraphQL
		githubPageCall = previousPage
	})
	githubGraphQLCall = func(ctx context.Context, _ string, _ map[string]string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	githubPageCall = func(ctx context.Context, _ string) (string, []byte, error) {
		<-ctx.Done()
		return "", nil, ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := collectBranchStream(ctx, nil)
		done <- err
	}()

	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling did not stop the branch stream")
	}
}

func TestStreamGitHubBranchPRsReportsABranchListFailure(t *testing.T) {
	stubBranchStreamSources(t, nil, errors.New("no preview"), nil)
	previous := githubPageCall
	t.Cleanup(func() { githubPageCall = previous })
	githubPageCall = func(context.Context, string) (string, []byte, error) {
		return "", nil, errors.New("gh exploded")
	}

	_, err := collectBranchStream(context.Background(), nil)

	assert.ErrorContains(t, err, "gh exploded")
}

// A consumer that fails ends the stream with its own error, not a forge one.
func TestStreamGitHubBranchPRsReportsAConsumerFailure(t *testing.T) {
	stubBranchStreamSources(t, []string{"alpha"}, nil, [][]string{{"alpha", "beta"}})

	err := StreamGitHubBranchPRs(context.Background(), "owner/repo",
		func(BranchInfo) bool { return true },
		func([]BranchPR) error { return errors.New("consumer is gone") })

	assert.ErrorContains(t, err, "consumer is gone")
}
