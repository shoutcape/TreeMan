package forge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/shoutcape/treeman/internal/remote"
)

// PRInfo holds the metadata returned for a single PR or MR.
type PRInfo struct {
	Number int
	Title  string
	Branch string // head ref / source branch
	Owner  string // PR author login
}

// PRMetadata fetches metadata for a single PR/MR number via gh or glab.
//
// Mirrors _wt_pr_metadata in wt.sh:283.
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
//
// Mirrors _wt_pr_list in wt.sh:306.
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
			Ref  string `json:"ref"`
			Repo struct {
				Owner struct {
					Login string `json:"login"`
				} `json:"owner"`
			} `json:"repo"`
		} `json:"head"`
	}
	if err := json.Unmarshal(out, &data); err != nil {
		return PRInfo{}, fmt.Errorf("gh: parsing PR metadata: %w", err)
	}

	return PRInfo{
		Number: data.Number,
		Title:  data.Title,
		Branch: data.Head.Ref,
		Owner:  data.Head.Repo.Owner.Login,
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

func ghAPI(endpoint string) ([]byte, error) {
	cmd := exec.Command("gh", "api", endpoint)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh api %s: %s", endpoint, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
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
		Author struct {
			Username string `json:"username"`
		} `json:"author"`
	}
	if err := json.Unmarshal(out, &data); err != nil {
		return PRInfo{}, fmt.Errorf("glab: parsing MR metadata: %w", err)
	}

	return PRInfo{
		Number: data.IID,
		Title:  data.Title,
		Branch: data.Branch,
		Owner:  data.Author.Username,
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

func glabAPI(host, endpoint string) ([]byte, error) {
	cmd := exec.Command("glab", "api", endpoint, "--hostname", host)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("glab api %s: %s", endpoint, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// ---------------------------------------------------------------------------
// Helper: resolve forge from the current repo's origin remote
// ---------------------------------------------------------------------------

// ResolveFromRemote detects the forge type, repo slug, and host from a remote
// URL string. This is a convenience wrapper used by command handlers.
func ResolveFromRemote(remoteURL string) (forgeType Type, repoSlug, host string, err error) {
	host, err = remote.ParseHost(remoteURL)
	if err != nil {
		return "", "", "", err
	}
	forgeType, err = DetectFromHost(host)
	if err != nil {
		return "", "", "", err
	}
	repoSlug, err = remote.ParsePath(remoteURL)
	if err != nil {
		return "", "", "", err
	}
	return forgeType, repoSlug, host, nil
}

// NumberToString is a small helper used in tests and display.
func NumberToString(n int) string {
	return strconv.Itoa(n)
}
