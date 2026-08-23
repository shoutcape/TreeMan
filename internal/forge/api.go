package forge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/shoutcape/treeman/internal/remote"
)

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
func PRMetadata(forge Type, repoSlug, host string, prNumber int) (PRInfo, error) {
	switch forge {
	case GitHub:
		return githubPRMetadata(repoSlug, prNumber)
	case GitLab:
		return gitlabMRMetadata(repoSlug, host, prNumber)
	default:
		return PRInfo{}, fmt.Errorf("unsupported forge: %q", forge)
	}
}

// PRList returns all open PRs/MRs via gh or glab.
func PRList(forge Type, repoSlug, host string) ([]PRInfo, error) {
	switch forge {
	case GitHub:
		return githubPRList(repoSlug)
	case GitLab:
		return gitlabMRList(repoSlug, host)
	default:
		return nil, fmt.Errorf("unsupported forge: %q", forge)
	}
}

// Indirection vars so tests can stub CLI invocations.
var (
	githubAPICall = ghAPI
	glabAPICall   = glabAPI
)

// MergedPRHeadSHAs returns the head SHAs of every merged PR/MR sourced from
// branch. Cleanup uses these to confirm squash/rebase merges: a remote-gone
// branch counts as merged only when its local tip equals one of the returned
// SHAs, so local commits made after a merge are never discarded.
//
// Cross-fork pull requests whose head lives under another owner do not match
// the GitHub head filter; such branches report no SHAs.
func MergedPRHeadSHAs(forgeType Type, repoSlug, host, branch string) ([]string, error) {
	switch forgeType {
	case GitHub:
		return githubMergedHeadSHAs(repoSlug, branch)
	case GitLab:
		return gitlabMergedHeadSHAs(repoSlug, host, branch)
	default:
		return nil, fmt.Errorf("unsupported forge: %q", forgeType)
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
func BranchList(forge Type, repoSlug, host string) ([]BranchInfo, error) {
	switch forge {
	case GitHub:
		return githubBranchList(repoSlug)
	case GitLab:
		return gitlabBranchList(repoSlug, host)
	default:
		return nil, fmt.Errorf("unsupported forge: %q", forge)
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

// ---------------------------------------------------------------------------
// GitHub
// ---------------------------------------------------------------------------

func githubPRMetadata(repoSlug string, prNumber int) (PRInfo, error) {
	endpoint := fmt.Sprintf("repos/%s/pulls/%d", repoSlug, prNumber)
	out, err := ghAPI(endpoint)
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

func githubPRList(repoSlug string) ([]PRInfo, error) {
	endpoint := fmt.Sprintf("repos/%s/pulls?state=open&per_page=100", repoSlug)
	out, err := ghAPI(endpoint)
	if err != nil {
		return nil, err
	}

	var data []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Head   struct {
			Ref string `json:"ref"`
		} `json:"head"`
	}
	if err := json.Unmarshal(out, &data); err != nil {
		return nil, fmt.Errorf("gh: parsing PR list: %w", err)
	}

	prs := make([]PRInfo, 0, len(data))
	for _, d := range data {
		prs = append(prs, PRInfo{
			Number: d.Number,
			Title:  d.Title,
			Branch: d.Head.Ref,
		})
	}
	return prs, nil
}

func githubMergedHeadSHAs(repoSlug, branch string) ([]string, error) {
	head := branch
	if idx := strings.Index(repoSlug, "/"); idx >= 0 {
		head = repoSlug[:idx] + ":" + branch
	}
	endpoint := fmt.Sprintf("repos/%s/pulls?state=closed&head=%s&per_page=100", repoSlug, url.QueryEscape(head))
	out, err := githubAPICall(endpoint)
	if err != nil {
		return nil, err
	}
	return parseGithubMergedHeadSHAs(out)
}

// parseGithubMergedHeadSHAs extracts the head SHA of each merged pull request
// in a closed-PR listing. gh api --paginate emits every page as its own JSON
// array, so pages are decoded sequentially until EOF rather than unmarshalled
// as a single document.
func parseGithubMergedHeadSHAs(data []byte) ([]string, error) {
	var shas []string
	err := decodePaginatedArrays(data, func(page json.RawMessage) error {
		var prs []struct {
			MergedAt *string `json:"merged_at"`
			Head     struct {
				SHA string `json:"sha"`
			} `json:"head"`
		}
		if err := json.Unmarshal(page, &prs); err != nil {
			return err
		}
		for _, pr := range prs {
			if pr.MergedAt != nil && pr.Head.SHA != "" {
				shas = append(shas, pr.Head.SHA)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("gh: parsing closed PR list: %w", err)
	}
	return shas, nil
}

func ghAPI(endpoint string) ([]byte, error) {
	cmd := exec.Command("gh", ghAPIArgs(endpoint)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh api %s: %s", endpoint, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func ghAPIArgs(endpoint string) []string {
	return []string{"api", endpoint, "--paginate"}
}

func githubBranchList(repoSlug string) ([]BranchInfo, error) {
	endpoint := fmt.Sprintf("repos/%s/branches?per_page=100", repoSlug)
	out, err := ghAPI(endpoint)
	if err != nil {
		return nil, err
	}

	var data []struct {
		Name   string `json:"name"`
		Commit struct {
			Commit struct {
				Committer struct {
					Date string `json:"date"`
				} `json:"committer"`
			} `json:"commit"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(out, &data); err != nil {
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

func gitlabMRMetadata(repoSlug, host string, prNumber int) (PRInfo, error) {
	encoded := remote.URLEncode(repoSlug)
	endpoint := fmt.Sprintf("projects/%s/merge_requests/%d", encoded, prNumber)
	out, err := glabAPI(host, endpoint)
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

func gitlabMRList(repoSlug, host string) ([]PRInfo, error) {
	encoded := remote.URLEncode(repoSlug)
	endpoint := fmt.Sprintf("projects/%s/merge_requests?state=opened&per_page=100", encoded)
	out, err := glabAPI(host, endpoint)
	if err != nil {
		return nil, err
	}

	var data []struct {
		IID    int    `json:"iid"`
		Title  string `json:"title"`
		Branch string `json:"source_branch"`
	}
	if err := json.Unmarshal(out, &data); err != nil {
		return nil, fmt.Errorf("glab: parsing MR list: %w", err)
	}

	mrs := make([]PRInfo, 0, len(data))
	for _, d := range data {
		mrs = append(mrs, PRInfo{
			Number: d.IID,
			Title:  d.Title,
			Branch: d.Branch,
		})
	}
	return mrs, nil
}

func gitlabMergedHeadSHAs(repoSlug, host, branch string) ([]string, error) {
	encoded := remote.URLEncode(repoSlug)
	endpoint := fmt.Sprintf("projects/%s/merge_requests?source_branch=%s&state=merged&per_page=100", encoded, url.QueryEscape(branch))
	out, err := glabAPICall(host, endpoint)
	if err != nil {
		return nil, err
	}
	return parseGitlabMergedHeadSHAs(out)
}

// parseGitlabMergedHeadSHAs extracts the source head SHA of each merged MR.
// glab api --paginate emits every page as its own JSON array, so pages are
// decoded sequentially until EOF rather than unmarshalled as a single
// document.
func parseGitlabMergedHeadSHAs(data []byte) ([]string, error) {
	var shas []string
	err := decodePaginatedArrays(data, func(page json.RawMessage) error {
		var mrs []struct {
			SHA string `json:"sha"`
		}
		if err := json.Unmarshal(page, &mrs); err != nil {
			return err
		}
		for _, mr := range mrs {
			if mr.SHA != "" {
				shas = append(shas, mr.SHA)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("glab: parsing merged MR list: %w", err)
	}
	return shas, nil
}

// decodePaginatedArrays accepts one or more concatenated JSON array documents,
// the format emitted by gh and glab with --paginate.
func decodePaginatedArrays(data []byte, consume func(json.RawMessage) error) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
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
		pages++
		if err := consume(page); err != nil {
			return err
		}
	}
}

func glabAPI(host, endpoint string) ([]byte, error) {
	cmd := exec.Command("glab", glabAPIArgs(host, endpoint)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("glab api %s: %s", endpoint, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func glabAPIArgs(host, endpoint string) []string {
	return []string{"api", endpoint, "--hostname", host, "--paginate"}
}

func gitlabBranchList(repoSlug, host string) ([]BranchInfo, error) {
	encoded := remote.URLEncode(repoSlug)
	endpoint := fmt.Sprintf("projects/%s/repository/branches?per_page=100", encoded)
	out, err := glabAPI(host, endpoint)
	if err != nil {
		return nil, err
	}

	var data []struct {
		Name   string `json:"name"`
		Commit struct {
			CommittedDate string `json:"committed_date"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(out, &data); err != nil {
		return nil, fmt.Errorf("glab: parsing branch list: %w", err)
	}

	branches := make([]BranchInfo, 0, len(data))
	for _, d := range data {
		branches = append(branches, BranchInfo{
			Name: d.Name,
			Date: formatRelativeDate(d.Commit.CommittedDate),
		})
	}
	return branches, nil
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
