// Package forge provides GitHub and GitLab integration for PR/MR operations.
package forge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/shoutcape/TreeMan/internal/git"
)

// PRMetadata holds metadata about a single pull request or merge request.
type PRMetadata struct {
	Number  int    // PR/MR number
	Title   string // PR/MR title
	HeadRef string // source branch name
	Owner   string // PR author username
}

// PRSummary holds a summary of a PR/MR for listing.
type PRSummary struct {
	Number int
	Branch string
	Title  string
}

// Forge is the interface for forge-specific operations.
type Forge interface {
	Name() string
	CLITool() string
	CheckCLI() error
	FetchPRMetadata(number int) (*PRMetadata, error)
	ListOpenPRs() ([]PRSummary, error)
	FetchRef(number int) string
}

// Detect returns the appropriate Forge implementation based on the origin remote URL.
// Respects the _TREEMAN_FORGE and _TREEMAN_GH_REPO environment variable overrides.
func Detect() (Forge, error) {
	// Allow override via env var (used in tests)
	if forceForge := os.Getenv("_TREEMAN_FORGE"); forceForge != "" {
		switch forceForge {
		case "github":
			return &GitHub{}, nil
		case "gitlab":
			return &GitLab{}, nil
		default:
			return nil, fmt.Errorf("unsupported _TREEMAN_FORGE value: %q", forceForge)
		}
	}

	forgeType, err := git.DetectForge()
	if err != nil {
		return nil, err
	}

	switch forgeType {
	case git.ForgeGitHub:
		return &GitHub{}, nil
	case git.ForgeGitLab:
		return &GitLab{}, nil
	default:
		return nil, fmt.Errorf("unsupported forge")
	}
}

// repoSlug returns the owner/repo slug, respecting the _TREEMAN_GH_REPO override.
func repoSlug() (string, error) {
	if override := os.Getenv("_TREEMAN_GH_REPO"); override != "" {
		return strings.TrimRight(override, "/"), nil
	}
	return git.OriginRepoSlug()
}

// originHost returns the origin hostname.
func originHost() (string, error) {
	if override := os.Getenv("_TREEMAN_REMOTE_URL"); override != "" {
		return git.ParseRemoteHost(override)
	}
	return git.OriginHost()
}

// urlEncode URL-encodes a string (for GitLab project paths in API URLs).
func urlEncode(s string) string {
	return url.PathEscape(s)
}

// --- GitHub ---

// GitHub implements the Forge interface for GitHub repositories.
type GitHub struct{}

func (g *GitHub) Name() string    { return "github" }
func (g *GitHub) CLITool() string { return "gh" }

func (g *GitHub) CheckCLI() error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh is required for GitHub repos. Install it from https://cli.github.com/")
	}
	return nil
}

func (g *GitHub) FetchRef(number int) string {
	return fmt.Sprintf("pull/%d/head", number)
}

func (g *GitHub) FetchPRMetadata(number int) (*PRMetadata, error) {
	slug, err := repoSlug()
	if err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("repos/%s/pulls/%d", slug, number)
	output, err := runCLI("gh", "api", endpoint,
		"--jq", `[.number, .title, .head.ref, .head.repo.owner.login] | @tsv`)
	if err != nil {
		return nil, err
	}

	return parseTSVMetadata(output)
}

func (g *GitHub) ListOpenPRs() ([]PRSummary, error) {
	slug, err := repoSlug()
	if err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("repos/%s/pulls?state=open&per_page=100", slug)
	output, err := runCLI("gh", "api", endpoint,
		"--jq", `.[] | [.number, .head.ref, .title] | @tsv`)
	if err != nil {
		return nil, err
	}

	return parseTSVSummaries(output), nil
}

// --- GitLab ---

// GitLab implements the Forge interface for GitLab repositories.
type GitLab struct{}

func (gl *GitLab) Name() string    { return "gitlab" }
func (gl *GitLab) CLITool() string { return "glab" }

func (gl *GitLab) CheckCLI() error {
	if _, err := exec.LookPath("glab"); err != nil {
		return fmt.Errorf("glab is required for GitLab repos. Install it from https://gitlab.com/gitlab-org/cli")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		return fmt.Errorf("jq is required for GitLab repos. Install it from https://jqlang.github.io/jq/")
	}
	return nil
}

func (gl *GitLab) FetchRef(number int) string {
	return fmt.Sprintf("merge-requests/%d/head", number)
}

func (gl *GitLab) FetchPRMetadata(number int) (*PRMetadata, error) {
	slug, err := repoSlug()
	if err != nil {
		return nil, err
	}

	host, err := originHost()
	if err != nil {
		return nil, err
	}

	encoded := urlEncode(slug)
	endpoint := fmt.Sprintf("projects/%s/merge_requests/%d", encoded, number)
	output, err := runCLI("glab", "api", endpoint, "--hostname", host)
	if err != nil {
		return nil, err
	}

	// Parse the JSON response directly (glab api returns raw JSON)
	var mr struct {
		IID          int    `json:"iid"`
		Title        string `json:"title"`
		SourceBranch string `json:"source_branch"`
		Author       struct {
			Username string `json:"username"`
		} `json:"author"`
	}

	if err := json.Unmarshal([]byte(output), &mr); err != nil {
		return nil, fmt.Errorf("parsing GitLab MR response: %w", err)
	}

	return &PRMetadata{
		Number:  mr.IID,
		Title:   mr.Title,
		HeadRef: mr.SourceBranch,
		Owner:   mr.Author.Username,
	}, nil
}

func (gl *GitLab) ListOpenPRs() ([]PRSummary, error) {
	slug, err := repoSlug()
	if err != nil {
		return nil, err
	}

	host, err := originHost()
	if err != nil {
		return nil, err
	}

	encoded := urlEncode(slug)
	endpoint := fmt.Sprintf("projects/%s/merge_requests?state=opened&per_page=100", encoded)
	output, err := runCLI("glab", "api", endpoint, "--hostname", host)
	if err != nil {
		return nil, err
	}

	// Parse the JSON array response
	var mrs []struct {
		IID          int    `json:"iid"`
		SourceBranch string `json:"source_branch"`
		Title        string `json:"title"`
	}

	if err := json.Unmarshal([]byte(output), &mrs); err != nil {
		return nil, fmt.Errorf("parsing GitLab MR list: %w", err)
	}

	summaries := make([]PRSummary, len(mrs))
	for i, mr := range mrs {
		summaries[i] = PRSummary{
			Number: mr.IID,
			Branch: mr.SourceBranch,
			Title:  mr.Title,
		}
	}

	return summaries, nil
}

// --- Helpers ---

// runCLI executes a CLI command and returns its stdout.
func runCLI(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args[:min(len(args), 2)], " "), err)
	}

	return strings.TrimSpace(stdout.String()), nil
}

// parseTSVMetadata parses a single TSV line into PRMetadata.
// Format: number\ttitle\thead_ref\towner
func parseTSVMetadata(tsv string) (*PRMetadata, error) {
	fields := strings.SplitN(strings.TrimSpace(tsv), "\t", 4)
	if len(fields) < 3 {
		return nil, fmt.Errorf("incomplete PR metadata: %q", tsv)
	}

	number, err := strconv.Atoi(fields[0])
	if err != nil {
		return nil, fmt.Errorf("invalid PR number: %q", fields[0])
	}

	meta := &PRMetadata{
		Number:  number,
		Title:   fields[1],
		HeadRef: fields[2],
	}
	if len(fields) >= 4 {
		meta.Owner = fields[3]
	}

	return meta, nil
}

// parseTSVSummaries parses TSV lines into PRSummary slices.
// Format per line: number\tbranch\ttitle
func parseTSVSummaries(tsv string) []PRSummary {
	var summaries []PRSummary
	for _, line := range strings.Split(strings.TrimSpace(tsv), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) < 3 {
			continue
		}
		number, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		summaries = append(summaries, PRSummary{
			Number: number,
			Branch: fields[1],
			Title:  fields[2],
		})
	}
	return summaries
}

// FormatPRPickerDisplay formats PR summaries for fzf display with ANSI colors.
// Returns a slice of lines including a header.
func FormatPRPickerDisplay(summaries []PRSummary) []string {
	// TreeMan palette: #F2EA72 golden yellow, #B2B644 bright olive, #C4915E warm brown
	lines := make([]string, 0, len(summaries)+1)

	// Header
	lines = append(lines, fmt.Sprintf(
		"\033[38;2;242;234;114m%-8s\033[0m \033[38;2;178;182;68m%-32s\033[0m \033[38;2;196;145;94m%s\033[0m",
		"PR/MR", "Branch", "Title"))

	for _, s := range summaries {
		branch := s.Branch
		if len(branch) > 32 {
			branch = branch[:31] + "…"
		}
		lines = append(lines, fmt.Sprintf(
			"\033[38;2;242;234;114m#%-7d\033[0m \033[38;2;178;182;68m%-32s\033[0m \033[38;2;196;145;94m%s\033[0m",
			s.Number, branch, s.Title))
	}

	return lines
}

// ValidatePRNumber checks if a string is a valid PR/MR number.
func ValidatePRNumber(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("PR/MR number must be a positive integer")
	}
	return n, nil
}
