package forge

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/shoutcape/treeman/internal/remote"
)

func gitlabMergedHead(repoSlug, host, defaultBranch, branch, sha string) (bool, error) {
	encoded := remote.URLEncode(repoSlug)
	endpoint := fmt.Sprintf("projects/%s/merge_requests?state=merged&source_branch=%s&target_branch=%s&per_page=100", encoded, url.QueryEscape(branch), url.QueryEscape(defaultBranch))
	out, err := glabAPICall(host, endpoint)
	if err != nil {
		return false, err
	}
	return parseGitlabMergedHead(out, defaultBranch, branch, sha)
}

// GitLabMergedHeads returns exact merged-MR matches for candidates in one
// GraphQL traversal. It deliberately reads only merge evidence; callers keep
// Git as the source of truth for current remote refs.
func GitLabMergedHeads(repoSlug, host, defaultBranch string, candidates []SnapshotCandidate) (map[string]bool, error) {
	if defaultBranch == "" {
		return nil, errors.New("GitLab default branch is required")
	}
	if len(candidates) == 0 {
		return map[string]bool{}, nil
	}

	expected := make(map[string]string, len(candidates))
	sourceBranches := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Branch == "" || candidate.SHA == "" {
			return nil, errors.New("GitLab merge candidates require branch and SHA")
		}
		if _, duplicate := expected[candidate.Branch]; duplicate {
			return nil, fmt.Errorf("duplicate GitLab merge candidate branch %q", candidate.Branch)
		}
		expected[candidate.Branch] = candidate.SHA
		sourceBranches = append(sourceBranches, candidate.Branch)
	}

	matched := make(map[string]bool, len(candidates))
	variables := map[string]any{
		"fullPath":       repoSlug,
		"sourceBranches": sourceBranches,
		"targetBranches": []string{defaultBranch},
		"endCursor":      nil,
	}
	previousCursor := ""
	for {
		out, err := gitlabGraphQLCall(host, gitlabMergedHeadsQuery, variables)
		if err != nil {
			return nil, err
		}
		page, err := parseGitLabMergedHeadsPage(out, defaultBranch, expected)
		if err != nil {
			return nil, err
		}
		for branch := range page.matched {
			matched[branch] = true
		}
		if !page.hasNextPage {
			return matched, nil
		}
		if page.endCursor == "" {
			return nil, errors.New("glab: GitLab merged-MR query has no next cursor")
		}
		if page.endCursor == previousCursor {
			return nil, fmt.Errorf("glab: GitLab merged-MR query cursor did not advance from %q", page.endCursor)
		}
		previousCursor = page.endCursor
		variables["endCursor"] = page.endCursor
	}
}

const gitlabMergedHeadsQuery = `query($fullPath: ID!, $sourceBranches: [String!], $targetBranches: [String!], $endCursor: String) {
  project(fullPath: $fullPath) {
    mergeRequests(first: 100, after: $endCursor, state: merged, sourceBranches: $sourceBranches, targetBranches: $targetBranches) {
      nodes {
        sourceBranch
        targetBranch
        diffHeadSha
      }
      pageInfo {
        endCursor
        hasNextPage
      }
    }
  }
}`

type gitlabMergedHeadsPage struct {
	matched     map[string]bool
	endCursor   string
	hasNextPage bool
}

func parseGitLabMergedHeadsPage(data []byte, defaultBranch string, expected map[string]string) (gitlabMergedHeadsPage, error) {
	var response struct {
		Data *struct {
			Project *struct {
				MergeRequests *struct {
					Nodes []*struct {
						SourceBranch string  `json:"sourceBranch"`
						TargetBranch string  `json:"targetBranch"`
						DiffHeadSHA  *string `json:"diffHeadSha"`
					} `json:"nodes"`
					PageInfo *struct {
						EndCursor   *string `json:"endCursor"`
						HasNextPage bool    `json:"hasNextPage"`
					} `json:"pageInfo"`
				} `json:"mergeRequests"`
			} `json:"project"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return gitlabMergedHeadsPage{}, fmt.Errorf("glab: parsing GitLab merged-MR query: %w", err)
	}
	if len(response.Errors) > 0 {
		return gitlabMergedHeadsPage{}, fmt.Errorf("glab: GitLab merged-MR query failed: %s", response.Errors[0].Message)
	}
	if response.Data == nil || response.Data.Project == nil || response.Data.Project.MergeRequests == nil || response.Data.Project.MergeRequests.PageInfo == nil {
		return gitlabMergedHeadsPage{}, errors.New("glab: GitLab merged-MR query lacks project data")
	}

	page := gitlabMergedHeadsPage{matched: make(map[string]bool)}
	for _, mr := range response.Data.Project.MergeRequests.Nodes {
		if mr == nil || mr.DiffHeadSHA == nil {
			continue
		}
		if sha := expected[mr.SourceBranch]; sha == *mr.DiffHeadSHA && mr.TargetBranch == defaultBranch {
			page.matched[mr.SourceBranch] = true
		}
	}
	page.hasNextPage = response.Data.Project.MergeRequests.PageInfo.HasNextPage
	if response.Data.Project.MergeRequests.PageInfo.EndCursor != nil {
		page.endCursor = *response.Data.Project.MergeRequests.PageInfo.EndCursor
	}
	return page, nil
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
