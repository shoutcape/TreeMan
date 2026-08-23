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
	out, err := githubAPICall(endpoint)
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
	out, err := githubAPICall(endpoint)
	if err != nil {
		return nil, err
	}

	data, err := decodePaginated[struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Head   struct {
			Ref string `json:"ref"`
		} `json:"head"`
	}](out)
	if err != nil {
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

func githubMergedHead(repoSlug, defaultBranch, branch, sha string) (bool, error) {
	endpoint := fmt.Sprintf("repos/%s/commits/%s/pulls?per_page=100", repoSlug, url.PathEscape(sha))
	out, err := githubAPICall(endpoint)
	if err != nil {
		return false, err
	}
	return parseGithubMergedHead(out, defaultBranch, branch, sha)
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

func ghAPI(endpoint string) ([]byte, error) {
	return runGHAPI(endpoint, ghAPIArgs(endpoint))
}

func runGHAPI(endpoint string, args []string) ([]byte, error) {
	cmd := exec.Command("gh", args...)
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
	out, err := githubAPICall(endpoint)
	if err != nil {
		return nil, err
	}

	data, err := decodePaginated[struct {
		Name   string `json:"name"`
		Commit struct {
			Commit struct {
				Committer struct {
					Date string `json:"date"`
				} `json:"committer"`
			} `json:"commit"`
		} `json:"commit"`
	}](out)
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

func gitlabMRMetadata(repoSlug, host string, prNumber int) (PRInfo, error) {
	encoded := remote.URLEncode(repoSlug)
	endpoint := fmt.Sprintf("projects/%s/merge_requests/%d", encoded, prNumber)
	out, err := glabAPICall(host, endpoint)
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
	out, err := glabAPICall(host, endpoint)
	if err != nil {
		return nil, err
	}

	data, err := decodePaginated[struct {
		IID    int    `json:"iid"`
		Title  string `json:"title"`
		Branch string `json:"source_branch"`
	}](out)
	if err != nil {
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

func gitlabMergedHead(repoSlug, host, defaultBranch, branch, sha string) (bool, error) {
	encoded := remote.URLEncode(repoSlug)
	endpoint := fmt.Sprintf("projects/%s/merge_requests?state=merged&source_branch=%s&target_branch=%s&per_page=100", encoded, url.QueryEscape(branch), url.QueryEscape(defaultBranch))
	out, err := glabAPICall(host, endpoint)
	if err != nil {
		return false, err
	}
	return parseGitlabMergedHead(out, defaultBranch, branch, sha)
}

func parseGitlabMergedHead(data []byte, defaultBranch, branch, sha string) (bool, error) {
	mrs, err := decodePaginated[struct {
		State        string `json:"state"`
		Branch       string `json:"source_branch"`
		TargetBranch string `json:"target_branch"`
		SHA          string `json:"sha"`
	}](data)
	if err != nil {
		return false, fmt.Errorf("glab: parsing merged MR list: %w", err)
	}
	for _, mr := range mrs {
		if mr.State == "merged" && mr.TargetBranch == defaultBranch && mr.Branch == branch && mr.SHA == sha {
			return true, nil
		}
	}
	return false, nil
}

// decodePaginated reads the consecutive JSON arrays emitted by gh and glab
// when --paginate is used.
func decodePaginated[T any](data []byte) ([]T, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	pages := 0
	var values []T
	for {
		var page json.RawMessage
		if err := decoder.Decode(&page); err != nil {
			if errors.Is(err, io.EOF) {
				if pages == 0 {
					return nil, errors.New("empty JSON output")
				}
				return values, nil
			}
			return nil, err
		}
		page = bytes.TrimSpace(page)
		if bytes.Equal(page, []byte("null")) {
			return nil, errors.New("expected JSON array, got null")
		}
		if len(page) == 0 || page[0] != '[' {
			return nil, errors.New("expected JSON array")
		}
		var pageValues []T
		if err := json.Unmarshal(page, &pageValues); err != nil {
			return nil, err
		}
		pages++
		values = append(values, pageValues...)
	}
}

func glabAPI(host, endpoint string) ([]byte, error) {
	return runGlabAPI(host, endpoint, glabAPIArgs(host, endpoint))
}

func runGlabAPI(host, endpoint string, args []string) ([]byte, error) {
	cmd := exec.Command("glab", args...)
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
	out, err := glabAPICall(host, endpoint)
	if err != nil {
		return nil, err
	}

	data, err := decodePaginated[struct {
		Name   string `json:"name"`
		Commit struct {
			CommittedDate string `json:"committed_date"`
		} `json:"commit"`
	}](out)
	if err != nil {
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
