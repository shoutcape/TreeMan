package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/shoutcape/treeman/internal/remote"
)

const (
	// forgePageSize is how many rows one page of a forge list asks for. It is
	// the largest page GitHub and GitLab serve, so it is also the fewest
	// requests a full list can take.
	forgePageSize = 100

	// forgeRowBudget bounds how many rows one branch or PR/MR list delivers.
	// The lists follow pagination to its end, so the one walk that does not
	// stop on its own is over a repository with more refs or reviews than a
	// picker can be used with. This is where TreeMan stops asking for more.
	forgeRowBudget = 5000

	// forgePageBudget bounds how many pages one list walks. It covers
	// forgeRowBudget rows at forgePageSize each, and it also ends a walk whose
	// pages come back short, empty, or with a next link that never clears.
	forgePageBudget = forgeRowBudget / forgePageSize
)

// rowBudget tracks how much of forgeRowBudget one list has delivered.
type rowBudget struct{ delivered int }

// take claims room for size rows and returns how many of them fit.
func (b *rowBudget) take(size int) int {
	room := forgeRowBudget - b.delivered
	if size > room {
		size = room
	}
	b.delivered += size
	return size
}

// exhausted reports whether the budget has no room left.
func (b *rowBudget) exhausted() bool { return b.delivered >= forgeRowBudget }

// PRInfo holds the metadata returned for a single PR or MR.
type PRInfo struct {
	Number int
	Title  string
	Branch string // head ref / source branch
}

// BranchInfo holds metadata for a remote branch from the forge API.
type BranchInfo struct {
	Name string
	Date string // commit date (relative or ISO, depending on forge)
}

// PRMetadata fetches metadata for a single PR/MR number via gh or glab.
func PRMetadata(ctx context.Context, forge Type, repoSlug, host string, prNumber int) (PRInfo, error) {
	switch forge {
	case GitHub:
		return githubPRMetadata(ctx, repoSlug, prNumber)
	case GitLab:
		return gitlabMRMetadata(ctx, repoSlug, host, prNumber)
	default:
		return PRInfo{}, fmt.Errorf("unsupported forge: %q", forge)
	}
}

// PRList returns all open PRs/MRs via gh or glab.
func PRList(ctx context.Context, forge Type, repoSlug, host string) ([]PRInfo, error) {
	var prs []PRInfo
	err := StreamPRBatches(ctx, forge, repoSlug, host, func(batch []PRInfo) error {
		prs = append(prs, batch...)
		return nil
	})
	return prs, err
}

// StreamPRBatches delivers open PRs/MRs in forge order so callers can render
// before the whole list arrives. Callback failure or cancellation stops
// delivery and the CLI requests still in flight.
func StreamPRBatches(ctx context.Context, forge Type, repoSlug, host string, onBatch func([]PRInfo) error) error {
	switch forge {
	case GitHub:
		return githubPRBatches(ctx, repoSlug, onBatch)
	case GitLab:
		return gitlabMRBatches(ctx, repoSlug, host, onBatch)
	default:
		return fmt.Errorf("unsupported forge: %q", forge)
	}
}

// Indirection vars so tests can stub CLI invocations.
var (
	githubAPICall     = ghAPI
	githubGraphQLCall = ghGraphQL
	glabAPICall       = glabAPI
	glabAPIStreamCall = glabAPIStream
	gitlabGraphQLCall = glabGraphQL
)

// MergedPRHead reports whether branch at sha was merged into defaultBranch.
// The forge query is scoped to one candidate so cleanup cost scales with
// current worktrees rather than repository history.
func MergedPRHead(forgeType Type, repoSlug, host, defaultBranch, branch, sha string) (bool, error) {
	switch forgeType {
	case GitHub:
		return githubMergedHead(repoSlug, defaultBranch, branch, sha)
	case GitLab:
		return gitlabMergedHead(repoSlug, host, defaultBranch, branch, sha)
	default:
		return false, fmt.Errorf("unsupported forge: %q", forgeType)
	}
}

// FetchRef returns the git refspec to fetch a PR/MR by number.
//
// GitHub: pull/<n>/head
// GitLab: merge-requests/<n>/head
func FetchRef(forge Type, prNumber int) string {
	switch forge {
	case GitHub:
		return fmt.Sprintf("pull/%d/head", prNumber)
	case GitLab:
		return fmt.Sprintf("merge-requests/%d/head", prNumber)
	default:
		return ""
	}
}

// BranchList returns all remote branches via gh or glab.
// Returns branch names and last commit dates.
func BranchList(ctx context.Context, forge Type, repoSlug, host string) ([]BranchInfo, error) {
	var branches []BranchInfo
	err := StreamBranchBatches(ctx, forge, repoSlug, host, func(batch []BranchInfo) error {
		branches = append(branches, batch...)
		return nil
	})
	return branches, err
}

// StreamBranchBatches delivers remote branches in forge order so callers can
// render before the whole list arrives. Callback failure or cancellation stops
// delivery and the CLI requests still in flight.
func StreamBranchBatches(ctx context.Context, forge Type, repoSlug, host string, onBatch func([]BranchInfo) error) error {
	switch forge {
	case GitHub:
		return githubBranchBatches(ctx, repoSlug, onBatch)
	case GitLab:
		return gitlabBranchBatches(ctx, repoSlug, host, onBatch)
	default:
		return fmt.Errorf("unsupported forge: %q", forge)
	}
}

// CLITool returns the CLI tool name for the given forge.
func CLITool(forge Type) string {
	switch forge {
	case GitHub:
		return "gh"
	case GitLab:
		return "glab"
	default:
		return ""
	}
}

func githubPRMetadata(ctx context.Context, repoSlug string, prNumber int) (PRInfo, error) {
	endpoint := fmt.Sprintf("repos/%s/pulls/%d", repoSlug, prNumber)
	out, err := githubAPICall(ctx, endpoint)
	if err != nil {
		return PRInfo{}, err
	}

	var data struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Head   struct {
			Ref string `json:"ref"`
		} `json:"head"`
	}
	if err := json.Unmarshal(out, &data); err != nil {
		return PRInfo{}, fmt.Errorf("gh: parsing PR metadata: %w", err)
	}

	return PRInfo{
		Number: data.Number,
		Title:  data.Title,
		Branch: data.Head.Ref,
	}, nil
}

func githubPRList(ctx context.Context, repoSlug string) ([]PRInfo, error) {
	var prs []PRInfo
	err := githubPRBatches(ctx, repoSlug, func(page []PRInfo) error {
		prs = append(prs, page...)
		return nil
	})
	return prs, err
}

// githubPRBatches walks GraphQL cursor pagination to the last page, handing
// each batch to onBatch as it arrives. Cursors are opaque, so batches cannot
// be requested concurrently; delivering them one at a time is what lets a
// picker show the first results while the rest are still in flight.
//
// The walk stops early at forgeRowBudget rows or forgePageBudget pages.
func githubPRBatches(ctx context.Context, repoSlug string, onBatch func([]PRInfo) error) error {
	owner, name, err := splitRepoSlug(repoSlug)
	if err != nil {
		return err
	}

	cursor := ""
	budget := rowBudget{}
	seenCursors := make(map[string]struct{})
	for requested := 0; requested < forgePageBudget; requested++ {
		variables := map[string]string{"owner": owner, "name": name}
		if cursor != "" {
			variables["cursor"] = cursor
		}
		out, err := githubGraphQLCall(ctx, githubPRListQuery, variables)
		if err != nil {
			return err
		}
		page, err := parseGitHubPRListPage(out)
		if err != nil {
			return err
		}
		if err := onBatch(page.prs[:budget.take(len(page.prs))]); err != nil {
			return err
		}
		if !page.hasNextPage || budget.exhausted() {
			return nil
		}
		if page.endCursor == "" {
			return errors.New("gh: PR list pagination cursor did not advance")
		}
		if _, seen := seenCursors[page.endCursor]; seen {
			return errors.New("gh: PR list pagination cursor did not advance")
		}
		seenCursors[page.endCursor] = struct{}{}
		cursor = page.endCursor
	}
	return nil
}

var githubPRListQuery = fmt.Sprintf(`query($owner: String!, $name: String!, $cursor: String) {
  repository(owner: $owner, name: $name) {
    pullRequests(first: %d, after: $cursor, states: OPEN, orderBy: {field: CREATED_AT, direction: DESC}) {
      nodes { number title headRefName }
      pageInfo { hasNextPage endCursor }
    }
  }
}`, forgePageSize)

type githubPRListPage struct {
	prs         []PRInfo
	hasNextPage bool
	endCursor   string
}

func parseGitHubPRListPage(data []byte) (githubPRListPage, error) {
	var response struct {
		Data *struct {
			Repository *struct {
				PullRequests *struct {
					Nodes *[]*struct {
						Number *int    `json:"number"`
						Title  *string `json:"title"`
						Branch *string `json:"headRefName"`
					} `json:"nodes"`
					PageInfo *struct {
						HasNextPage *bool   `json:"hasNextPage"`
						EndCursor   *string `json:"endCursor"`
					} `json:"pageInfo"`
				} `json:"pullRequests"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return githubPRListPage{}, fmt.Errorf("gh: parsing PR list: %w", err)
	}
	if len(response.Errors) > 0 {
		return githubPRListPage{}, fmt.Errorf("gh: PR list failed: %s", response.Errors[0].Message)
	}
	if response.Data == nil || response.Data.Repository == nil || response.Data.Repository.PullRequests == nil {
		return githubPRListPage{}, errors.New("gh: PR list lacks repository data")
	}
	connection := response.Data.Repository.PullRequests
	if connection.PageInfo == nil {
		return githubPRListPage{}, errors.New("gh: PR list lacks pagination data")
	}
	if connection.Nodes == nil {
		return githubPRListPage{}, errors.New("gh: PR list lacks nodes")
	}
	if connection.PageInfo.HasNextPage == nil {
		return githubPRListPage{}, errors.New("gh: PR list lacks has-next-page data")
	}

	prs := make([]PRInfo, 0, len(*connection.Nodes))
	for _, node := range *connection.Nodes {
		if node == nil || node.Number == nil || *node.Number <= 0 || node.Title == nil || *node.Title == "" || node.Branch == nil || *node.Branch == "" {
			return githubPRListPage{}, errors.New("gh: PR list contains incomplete PR data")
		}
		prs = append(prs, PRInfo{Number: *node.Number, Title: *node.Title, Branch: *node.Branch})
	}
	page := githubPRListPage{prs: prs, hasNextPage: *connection.PageInfo.HasNextPage}
	if connection.PageInfo.EndCursor != nil {
		page.endCursor = *connection.PageInfo.EndCursor
	}
	return page, nil
}

func githubMergedHead(repoSlug, defaultBranch, branch, sha string) (bool, error) {
	endpoint := fmt.Sprintf("repos/%s/commits/%s/pulls?per_page=100", repoSlug, url.PathEscape(sha))
	// Cleanup verification is a single short request outside any interactive
	// picker, so it carries no cancellation of its own.
	out, err := githubAPICall(context.Background(), endpoint)
	if err != nil {
		return false, err
	}
	merged, err := parseGithubMergedHead(out, defaultBranch, branch, sha)
	return merged, err
}

func parseGithubMergedHead(data []byte, defaultBranch, branch, sha string) (bool, error) {
	prs, err := decodePaginated[struct {
		MergedAt *time.Time `json:"merged_at"`
		Head     struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	}](data)
	if err != nil {
		return false, fmt.Errorf("gh: parsing associated PR list: %w", err)
	}
	for _, pr := range prs {
		if pr.MergedAt != nil && pr.Base.Ref == defaultBranch && pr.Head.Ref == branch && pr.Head.SHA == sha {
			return true, nil
		}
	}
	return false, nil
}

func ghAPI(ctx context.Context, endpoint string) ([]byte, error) {
	return runForgeCLI(ctx, "gh", ghAPIArgs(endpoint), nil, "gh api "+endpoint)
}

func ghAPIArgs(endpoint string) []string {
	return []string{"api", endpoint, "--paginate"}
}

func ghGraphQL(ctx context.Context, query string, variables map[string]string) ([]byte, error) {
	return runForgeCLI(ctx, "gh", ghGraphQLArgs(query, variables), nil, "gh api graphql")
}

func ghGraphQLArgs(query string, variables map[string]string) []string {
	keys := make([]string, 0, len(variables))
	for key := range variables {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	args := []string{"api", "graphql", "-f", "query=" + query}
	for _, key := range keys {
		args = append(args, "-f", key+"="+variables[key])
	}
	return args
}

func githubBranchList(ctx context.Context, repoSlug string) ([]BranchInfo, error) {
	var branches []BranchInfo
	err := githubBranchBatches(ctx, repoSlug, func(page []BranchInfo) error {
		branches = append(branches, page...)
		return nil
	})
	return branches, err
}

// githubBranchBatches hands each ordered REST-derived batch to onBatch. Batches
// after the first are requested concurrently by fetchGitHubPages, which walks
// the whole branch list within forgePageBudget pages.
func githubBranchBatches(ctx context.Context, repoSlug string, onBatch func([]BranchInfo) error) error {
	endpoint := fmt.Sprintf("repos/%s/branches?per_page=%d", repoSlug, forgePageSize)
	return fetchGitHubPages(ctx, endpoint, func(raw []byte) error {
		branches, err := parseGitHubBranchPage(raw)
		if err != nil {
			return err
		}
		return onBatch(branches)
	})
}

func parseGitHubBranchPage(raw []byte) ([]BranchInfo, error) {
	data, err := decodePaginated[struct {
		Name   string `json:"name"`
		Commit struct {
			Commit struct {
				Committer struct {
					Date string `json:"date"`
				} `json:"committer"`
			} `json:"commit"`
		} `json:"commit"`
	}](raw)
	if err != nil {
		return nil, fmt.Errorf("gh: parsing branch list: %w", err)
	}

	branches := make([]BranchInfo, 0, len(data))
	for _, d := range data {
		branches = append(branches, BranchInfo{
			Name: d.Name,
			Date: formatRelativeDate(d.Commit.Commit.Committer.Date),
		})
	}
	return branches, nil
}

// ---------------------------------------------------------------------------
// GitLab
// ---------------------------------------------------------------------------

func gitlabMRMetadata(ctx context.Context, repoSlug, host string, prNumber int) (PRInfo, error) {
	encoded := remote.URLEncode(repoSlug)
	endpoint := fmt.Sprintf("projects/%s/merge_requests/%d", encoded, prNumber)
	out, err := glabAPICall(ctx, host, endpoint)
	if err != nil {
		return PRInfo{}, err
	}

	var data struct {
		IID    int    `json:"iid"`
		Title  string `json:"title"`
		Branch string `json:"source_branch"`
	}
	if err := json.Unmarshal(out, &data); err != nil {
		return PRInfo{}, fmt.Errorf("glab: parsing MR metadata: %w", err)
	}

	return PRInfo{
		Number: data.IID,
		Title:  data.Title,
		Branch: data.Branch,
	}, nil
}

// gitlabMRBatches hands each NDJSON record from glab to onBatch immediately.
// glab walks the pagination itself, so forgeRowBudget is enforced here: the
// record that does not fit stops glab instead of being delivered.
func gitlabMRBatches(ctx context.Context, repoSlug, host string, onBatch func([]PRInfo) error) error {
	encoded := remote.URLEncode(repoSlug)
	endpoint := fmt.Sprintf("projects/%s/merge_requests?state=opened&per_page=%d", encoded, forgePageSize)
	budget := rowBudget{}
	return glabAPIStreamCall(ctx, host, endpoint, func(out io.Reader) error {
		return decodeNDJSONStream(out, func(record struct {
			IID    int    `json:"iid"`
			Title  string `json:"title"`
			Branch string `json:"source_branch"`
		}) error {
			if budget.take(1) == 0 {
				return errStopStream
			}
			return onBatch([]PRInfo{{Number: record.IID, Title: record.Title, Branch: record.Branch}})
		})
	})
}

// decodePaginated reads consecutive JSON arrays emitted by aggregate CLI calls.
func decodePaginated[T any](data []byte) ([]T, error) {
	var values []T
	err := decodePaginatedStream(bytes.NewReader(data), func(page []T) error {
		values = append(values, page...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return values, nil
}

// decodeNDJSONStream delivers each complete JSON record as soon as it arrives.
// Empty output is a valid empty result set.
func decodeNDJSONStream[T any](reader io.Reader, onRecord func(T) error) error {
	decoder := json.NewDecoder(reader)
	for {
		var record T
		if err := decoder.Decode(&record); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if err := onRecord(record); err != nil {
			return err
		}
	}
}

// decodePaginatedStream decodes the same consecutive JSON arrays from a reader,
// handing each one to onPage as soon as it is complete. Reading a CLI's stdout
// this way delivers a page while the CLI is still fetching the next.
func decodePaginatedStream[T any](reader io.Reader, onPage func([]T) error) error {
	decoder := json.NewDecoder(reader)
	pages := 0
	for {
		var page json.RawMessage
		if err := decoder.Decode(&page); err != nil {
			if errors.Is(err, io.EOF) {
				if pages == 0 {
					return errors.New("empty JSON output")
				}
				return nil
			}
			return err
		}
		page = bytes.TrimSpace(page)
		if bytes.Equal(page, []byte("null")) {
			return errors.New("expected JSON array, got null")
		}
		if len(page) == 0 || page[0] != '[' {
			return errors.New("expected JSON array")
		}
		var values []T
		if err := json.Unmarshal(page, &values); err != nil {
			return err
		}
		pages++
		if err := onPage(values); err != nil {
			return err
		}
	}
}

func glabAPI(ctx context.Context, host, endpoint string) ([]byte, error) {
	return runForgeCLI(ctx, "glab", glabAPIArgs(host, endpoint), nil, "glab api "+endpoint)
}

// glabAPIStream emits NDJSON while glab is still walking its pagination.
func glabAPIStream(ctx context.Context, host, endpoint string, consume func(io.Reader) error) error {
	return runForgeCLIStream(ctx, "glab", glabAPIStreamArgs(host, endpoint), "glab api "+endpoint, consume)
}

func glabGraphQL(ctx context.Context, host, query string, variables map[string]any) ([]byte, error) {
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return nil, fmt.Errorf("glab: encoding GraphQL request: %w", err)
	}
	return runForgeCLI(ctx, "glab", glabGraphQLArgs(host), body, "glab api graphql")
}

func glabGraphQLArgs(host string) []string {
	return []string{"api", "graphql", "--hostname", host, "-H", "Content-Type: application/json", "--input", "-"}
}

func glabAPIArgs(host, endpoint string) []string {
	return []string{"api", endpoint, "--hostname", host, "--paginate"}
}

func glabAPIStreamArgs(host, endpoint string) []string {
	return []string{"api", endpoint, "--hostname", host, "--paginate", "--output", "ndjson"}
}

// gitlabBranchBatches hands each NDJSON record from glab to onBatch
// immediately, within the same forgeRowBudget as gitlabMRBatches.
func gitlabBranchBatches(ctx context.Context, repoSlug, host string, onBatch func([]BranchInfo) error) error {
	encoded := remote.URLEncode(repoSlug)
	endpoint := fmt.Sprintf("projects/%s/repository/branches?per_page=%d", encoded, forgePageSize)
	budget := rowBudget{}
	return glabAPIStreamCall(ctx, host, endpoint, func(out io.Reader) error {
		return decodeNDJSONStream(out, func(record struct {
			Name   string `json:"name"`
			Commit struct {
				CommittedDate string `json:"committed_date"`
			} `json:"commit"`
		}) error {
			if budget.take(1) == 0 {
				return errStopStream
			}
			return onBatch([]BranchInfo{{
				Name: record.Name,
				Date: formatRelativeDate(record.Commit.CommittedDate),
			}})
		})
	})
}

// ---------------------------------------------------------------------------
// Helper: resolve forge from the current repo's origin remote
// ---------------------------------------------------------------------------

// formatRelativeDate converts an ISO 8601 date string to a human-friendly
// relative format (e.g. "3 days ago", "2 weeks ago"). Falls back to the raw
// string if parsing fails.
func formatRelativeDate(isoDate string) string {
	if isoDate == "" {
		return ""
	}

	// Try common ISO formats.
	layouts := []string{
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05.000-07:00",
		"2006-01-02T15:04:05+00:00",
	}

	var t time.Time
	var err error
	for _, layout := range layouts {
		t, err = time.Parse(layout, isoDate)
		if err == nil {
			break
		}
	}
	if err != nil {
		// Return raw date trimmed to date portion if possible.
		if len(isoDate) >= 10 {
			return isoDate[:10]
		}
		return isoDate
	}

	duration := time.Since(t)

	switch {
	case duration < time.Minute:
		return "just now"
	case duration < time.Hour:
		m := int(duration.Minutes())
		if m == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", m)
	case duration < 24*time.Hour:
		h := int(duration.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	case duration < 7*24*time.Hour:
		d := int(duration.Hours() / 24)
		if d == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", d)
	case duration < 30*24*time.Hour:
		w := int(duration.Hours() / (24 * 7))
		if w == 1 {
			return "1 week ago"
		}
		return fmt.Sprintf("%d weeks ago", w)
	case duration < 365*24*time.Hour:
		m := int(duration.Hours() / (24 * 30))
		if m == 1 {
			return "1 month ago"
		}
		return fmt.Sprintf("%d months ago", m)
	default:
		y := int(duration.Hours() / (24 * 365))
		if y == 1 {
			return "1 year ago"
		}
		return fmt.Sprintf("%d years ago", y)
	}
}

// ResolveFromRemote detects the forge type, repo slug, and host from a remote
// URL string. This is a convenience wrapper used by command handlers.
//
// Environment overrides support command tests.
//   - _TREEMAN_FORGE    — override forge detection ("github" or "gitlab")
//   - _TREEMAN_GH_REPO  — override repo slug (e.g. "owner/repo")
func ResolveFromRemote(remoteURL string) (forgeType Type, repoSlug, host string, err error) {
	host, err = remote.ParseHost(remoteURL)
	if err != nil {
		// If _TREEMAN_FORGE is set, we can proceed even with an unparse-able URL.
		if os.Getenv("_TREEMAN_FORGE") == "" {
			return "", "", "", err
		}
		host = "override"
	}

	forgeType, err = DetectFromHost(host)
	if err != nil {
		return "", "", "", err
	}

	if slug := os.Getenv("_TREEMAN_GH_REPO"); slug != "" {
		repoSlug = strings.TrimRight(slug, "/")
	} else {
		repoSlug, err = remote.ParsePath(remoteURL)
		if err != nil {
			return "", "", "", err
		}
	}

	return forgeType, repoSlug, host, nil
}
