package forge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchRef(t *testing.T) {
	assert.Equal(t, "pull/42/head", FetchRef(GitHub, 42))
	assert.Equal(t, "merge-requests/7/head", FetchRef(GitLab, 7))
}

func TestCLITool(t *testing.T) {
	assert.Equal(t, "gh", CLITool(GitHub))
	assert.Equal(t, "glab", CLITool(GitLab))
	assert.Equal(t, "", CLITool(Type("unknown")))
}

func TestForgeAPIArgs(t *testing.T) {
	assert.Equal(t, []string{"api", "repos/owner/repo/branches?per_page=100", "--paginate"},
		ghAPIArgs("repos/owner/repo/branches?per_page=100"))
	assert.Equal(t, []string{"api", "projects/group%2Frepo/merge_requests?per_page=100", "--hostname", "gitlab.example", "--paginate"},
		glabAPIArgs("gitlab.example", "projects/group%2Frepo/merge_requests?per_page=100"))
	assert.Equal(t, []string{"api", "projects/group%2Frepo/merge_requests?per_page=100", "--hostname", "gitlab.example", "--paginate", "--output", "ndjson"},
		glabAPIStreamArgs("gitlab.example", "projects/group%2Frepo/merge_requests?per_page=100"))
	assert.Equal(t, []string{"api", "graphql", "-f", "query=query", "-f", "name=repo", "-f", "owner=owner"},
		ghGraphQLArgs("query", map[string]string{"owner": "owner", "name": "repo"}))
	assert.Equal(t, []string{"api", "graphql", "--hostname", "gitlab.example", "-H", "Content-Type: application/json", "--input", "-"},
		glabGraphQLArgs("gitlab.example"))
}

func TestResolveFromRemote(t *testing.T) {
	t.Run("github", func(t *testing.T) {
		forgeType, repoSlug, host, err := ResolveFromRemote("git@github.com:owner/my-repo.git")
		require.NoError(t, err)
		assert.Equal(t, GitHub, forgeType)
		assert.Equal(t, "owner/my-repo", repoSlug)
		assert.Equal(t, "github.com", host)
	})
	t.Run("gitlab", func(t *testing.T) {
		forgeType, repoSlug, host, err := ResolveFromRemote("https://gitlab.com/mygroup/myproject.git")
		require.NoError(t, err)
		assert.Equal(t, GitLab, forgeType)
		assert.Equal(t, "mygroup/myproject", repoSlug)
		assert.Equal(t, "gitlab.com", host)
	})
	t.Run("unsupported", func(t *testing.T) {
		_, _, _, err := ResolveFromRemote("https://bitbucket.org/owner/repo.git")
		assert.Error(t, err)
	})
}

func TestParseGithubMergedHead(t *testing.T) {
	payload := []byte(`[{"merged_at":"2026-08-01T10:00:00Z","base":{"ref":"main"},"head":{"ref":"feature","sha":"aaa111","repo":{"owner":{"login":"contributor"}}}}]`)
	merged, err := parseGithubMergedHead(payload, "main", "feature", "aaa111")
	require.NoError(t, err)
	assert.True(t, merged, "fork PRs must be accepted when branch and SHA match")

	merged, err = parseGithubMergedHead(payload, "main", "feature", "other")
	require.NoError(t, err)
	assert.False(t, merged)

	_, err = parseGithubMergedHead([]byte(`not-json`), "main", "feature", "aaa111")
	assert.Error(t, err)

	merged, err = parseGithubMergedHead([]byte(`[{"merged_at":null,"base":{"ref":"main"},"head":{"ref":"feature","sha":"aaa111"}}]`), "main", "feature", "aaa111")
	require.NoError(t, err)
	assert.False(t, merged)

	_, err = parseGithubMergedHead([]byte(`[{"merged_at":"","base":{"ref":"main"},"head":{"ref":"feature","sha":"aaa111"}}]`), "main", "feature", "aaa111")
	assert.Error(t, err)

	_, err = parseGithubMergedHead([]byte(`[{"merged_at":"not-a-date","base":{"ref":"main"},"head":{"ref":"feature","sha":"aaa111"}}]`), "main", "feature", "aaa111")
	assert.Error(t, err)

	merged, err = parseGithubMergedHead([]byte(`[{"merged_at":"2026-08-01T10:00:00Z","base":{"ref":"main"},"head":{"ref":"other","sha":"aaa111"}}][{"merged_at":"2026-08-01T10:00:00Z","base":{"ref":"main"},"head":{"ref":"feature","sha":"aaa111"}}]`), "main", "feature", "aaa111")
	require.NoError(t, err)
	assert.True(t, merged)

	for _, payload := range [][]byte{[]byte(``), []byte(`null`), []byte(`{}`)} {
		_, err = parseGithubMergedHead(payload, "main", "feature", "aaa111")
		assert.Error(t, err)
	}
}

func TestParseGitlabMergedHead(t *testing.T) {
	payload := []byte(`[{"state":"merged","source_branch":"feature","target_branch":"main","sha":"aaa111"}]`)
	merged, err := parseGitlabMergedHead(payload, "main", "feature", "aaa111")
	require.NoError(t, err)
	assert.True(t, merged)

	merged, err = parseGitlabMergedHead([]byte(`[]`), "main", "feature", "aaa111")
	require.NoError(t, err)
	assert.False(t, merged)

	_, err = parseGitlabMergedHead([]byte(`oops`), "main", "feature", "aaa111")
	assert.Error(t, err)

	merged, err = parseGitlabMergedHead([]byte(`[{"state":"opened","source_branch":"feature","target_branch":"main","sha":"aaa111"}]`), "main", "feature", "aaa111")
	require.NoError(t, err)
	assert.False(t, merged)

	merged, err = parseGitlabMergedHead([]byte(`[{"source_branch":"feature","target_branch":"main","sha":"aaa111"}]`), "main", "feature", "aaa111")
	require.NoError(t, err)
	assert.False(t, merged)

	merged, err = parseGitlabMergedHead([]byte(`[{"state":"merged","source_branch":"other","target_branch":"main","sha":"aaa111"}][{"state":"merged","source_branch":"feature","target_branch":"main","sha":"aaa111"}]`), "main", "feature", "aaa111")
	require.NoError(t, err)
	assert.True(t, merged)

	for _, payload := range [][]byte{[]byte(``), []byte(`null`), []byte(`{}`)} {
		_, err = parseGitlabMergedHead(payload, "main", "feature", "aaa111")
		assert.Error(t, err)
	}
}

func TestDecodePaginated(t *testing.T) {
	type value struct {
		Name string `json:"name"`
	}

	values, err := decodePaginated[value]([]byte(`[{"name":"first"}][{"name":"second"}]`))
	require.NoError(t, err)
	assert.Equal(t, []value{{Name: "first"}, {Name: "second"}}, values)

	_, err = decodePaginated[value]([]byte(`[{"name":"first"}]invalid`))
	assert.Error(t, err)
}

func TestPaginatedLists(t *testing.T) {
	t.Run("github PRs", func(t *testing.T) {
		previous := githubGraphQLCall
		githubGraphQLCall = func(_ context.Context, query string, variables map[string]string) ([]byte, error) {
			assert.Contains(t, query, "pullRequests(first: 100, after: $cursor, states: OPEN")
			assert.Contains(t, query, "nodes { number title headRefName }")
			assert.NotContains(t, query, "body")
			switch variables["cursor"] {
			case "":
				assert.Equal(t, map[string]string{"owner": "owner", "name": "repo"}, variables)
				return []byte(`{"data":{"repository":{"pullRequests":{"nodes":[{"number":1,"title":"first","headRefName":"one"}],"pageInfo":{"hasNextPage":true,"endCursor":"cursor-1"}}}}}`), nil
			case "cursor-1":
				return []byte(`{"data":{"repository":{"pullRequests":{"nodes":[{"number":2,"title":"second","headRefName":"two"}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`), nil
			default:
				return nil, fmt.Errorf("unexpected cursor %q", variables["cursor"])
			}
		}
		t.Cleanup(func() { githubGraphQLCall = previous })

		prs, err := githubPRList(context.Background(), "owner/repo")
		require.NoError(t, err)
		assert.Equal(t, []PRInfo{{Number: 1, Title: "first", Branch: "one"}, {Number: 2, Title: "second", Branch: "two"}}, prs)
	})

	t.Run("github branches", func(t *testing.T) {
		previous := githubPageCall
		githubPageCall = func(_ context.Context, endpoint string) (string, []byte, error) {
			if endpoint == "repos/owner/repo/branches?per_page=100" {
				return `<https://api.github.com/x?page=2>; rel="last"`,
					[]byte(`[{"name":"one","commit":{"commit":{"committer":{"date":"2026-08-01T10:00:00Z"}}}}]`), nil
			}
			return "", []byte(`[{"name":"two","commit":{"commit":{"committer":{"date":"2026-08-02T10:00:00Z"}}}}]`), nil
		}
		t.Cleanup(func() { githubPageCall = previous })

		branches, err := githubBranchList(context.Background(), "owner/repo")
		require.NoError(t, err)
		assert.Len(t, branches, 2)
		assert.Equal(t, []string{"one", "two"}, []string{branches[0].Name, branches[1].Name})
	})

	t.Run("gitlab MRs arrive one record at a time", func(t *testing.T) {
		previous := glabAPIStreamCall
		glabAPIStreamCall = func(_ context.Context, host, endpoint string, consume func(io.Reader) error) error {
			assert.Equal(t, "gitlab.example", host)
			assert.Equal(t, "projects/group%2Frepo/merge_requests?state=opened&per_page=100", endpoint)
			return consume(strings.NewReader("{\"iid\":1,\"title\":\"first\",\"source_branch\":\"one\"}\n{\"iid\":2,\"title\":\"second\",\"source_branch\":\"two\"}\n"))
		}
		t.Cleanup(func() { glabAPIStreamCall = previous })

		var batches [][]PRInfo
		err := gitlabMRBatches(context.Background(), "group/repo", "gitlab.example", func(batch []PRInfo) error {
			batches = append(batches, batch)
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, [][]PRInfo{
			{{Number: 1, Title: "first", Branch: "one"}},
			{{Number: 2, Title: "second", Branch: "two"}},
		}, batches)
	})

	t.Run("gitlab branches arrive one record at a time", func(t *testing.T) {
		previous := glabAPIStreamCall
		glabAPIStreamCall = func(_ context.Context, _, endpoint string, consume func(io.Reader) error) error {
			assert.Equal(t, "projects/group%2Frepo/repository/branches?per_page=100", endpoint)
			return consume(strings.NewReader("{\"name\":\"one\",\"commit\":{\"committed_date\":\"2026-08-01T10:00:00Z\"}}\n{\"name\":\"two\",\"commit\":{\"committed_date\":\"2026-08-02T10:00:00Z\"}}\n"))
		}
		t.Cleanup(func() { glabAPIStreamCall = previous })

		var pages [][]BranchInfo
		err := gitlabBranchBatches(context.Background(), "group/repo", "gitlab.example", func(page []BranchInfo) error {
			pages = append(pages, page)
			return nil
		})
		require.NoError(t, err)
		require.Len(t, pages, 2)
		assert.Equal(t, "one", pages[0][0].Name)
		assert.Equal(t, "two", pages[1][0].Name)
	})

	t.Run("gitlab records stop at the first consumer error", func(t *testing.T) {
		previous := glabAPIStreamCall
		glabAPIStreamCall = func(_ context.Context, _, _ string, consume func(io.Reader) error) error {
			return consume(strings.NewReader("{\"iid\":1,\"title\":\"first\",\"source_branch\":\"one\"}\n{\"iid\":2,\"title\":\"second\",\"source_branch\":\"two\"}\n"))
		}
		t.Cleanup(func() { glabAPIStreamCall = previous })

		pages := 0
		err := gitlabMRBatches(context.Background(), "group/repo", "gitlab.example", func([]PRInfo) error {
			pages++
			return assert.AnError
		})
		require.ErrorIs(t, err, assert.AnError)
		assert.Equal(t, 1, pages)
	})
}

func TestGitHubPRListRejectsInvalidGraphQLPages(t *testing.T) {
	for _, payload := range [][]byte{
		[]byte(`not-json`),
		[]byte(`{"errors":[{"message":"forbidden"}]}`),
		[]byte(`{"data":{"repository":null}}`),
		[]byte(`{"data":{"repository":{"pullRequests":null}}}`),
		[]byte(`{"data":{"repository":{"pullRequests":{"nodes":[],"pageInfo":null}}}}`),
		[]byte(`{"data":{"repository":{"pullRequests":{"nodes":null,"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`),
		[]byte(`{"data":{"repository":{"pullRequests":{"nodes":[null],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`),
		[]byte(`{"data":{"repository":{"pullRequests":{"nodes":[{}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`),
		[]byte(`{"data":{"repository":{"pullRequests":{"nodes":[{"number":1,"title":"first"}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`),
		[]byte(`{"data":{"repository":{"pullRequests":{"nodes":[{"number":1,"headRefName":"one"}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`),
		[]byte(`{"data":{"repository":{"pullRequests":{"nodes":[{"title":"first","headRefName":"one"}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`),
		[]byte(`{"data":{"repository":{"pullRequests":{"nodes":[{"number":1,"title":"","headRefName":"one"}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`),
		[]byte(`{"data":{"repository":{"pullRequests":{"nodes":[],"pageInfo":{"endCursor":null}}}}}`),
		[]byte(`{"data":{"repository":{"pullRequests":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":null}}}}}`),
	} {
		previous := githubGraphQLCall
		githubGraphQLCall = func(context.Context, string, map[string]string) ([]byte, error) { return payload, nil }
		_, err := githubPRList(context.Background(), "owner/repo")
		githubGraphQLCall = previous
		assert.Error(t, err)
	}
}

func TestGitHubPRListRejectsRepeatedCursor(t *testing.T) {
	previous := githubGraphQLCall
	githubGraphQLCall = func(_ context.Context, _ string, variables map[string]string) ([]byte, error) {
		switch variables["cursor"] {
		case "":
			return []byte(`{"data":{"repository":{"pullRequests":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"one"}}}}}`), nil
		case "one":
			return []byte(`{"data":{"repository":{"pullRequests":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"two"}}}}}`), nil
		case "two":
			return []byte(`{"data":{"repository":{"pullRequests":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"one"}}}}}`), nil
		default:
			return nil, fmt.Errorf("unexpected cursor %q", variables["cursor"])
		}
	}
	t.Cleanup(func() { githubGraphQLCall = previous })

	_, err := githubPRList(context.Background(), "owner/repo")

	require.EqualError(t, err, "gh: PR list pagination cursor did not advance")
}

func TestMergedPRHeadGitHub(t *testing.T) {
	t.Run("sha match", func(t *testing.T) {
		previousAPI := githubAPICall
		calls := 0
		githubAPICall = func(_ context.Context, endpoint string) ([]byte, error) {
			calls++
			assert.Equal(t, "repos/owner/repo/commits/aaa111/pulls?per_page=100", endpoint)
			return []byte(`[{"merged_at":"2026-08-01T10:00:00Z","base":{"ref":"main"},"head":{"ref":"feature/x","sha":"aaa111"}}]`), nil
		}
		t.Cleanup(func() { githubAPICall = previousAPI })

		merged, err := MergedPRHead(GitHub, "owner/repo", "github.com", "main", "feature/x", "aaa111")
		require.NoError(t, err)
		assert.True(t, merged)
		assert.Equal(t, 1, calls, "should not fall back when SHA matches")
	})

	t.Run("sha mismatch remains unmerged", func(t *testing.T) {
		previousAPI := githubAPICall
		calls := 0
		githubAPICall = func(_ context.Context, endpoint string) ([]byte, error) {
			calls++
			assert.Equal(t, "repos/owner/repo/commits/aaa111/pulls?per_page=100", endpoint)
			return []byte(`[{"merged_at":"2026-08-01T10:00:00Z","base":{"ref":"main"},"head":{"ref":"feature/x","sha":"old111"}}]`), nil
		}
		t.Cleanup(func() { githubAPICall = previousAPI })

		merged, err := MergedPRHead(GitHub, "owner/repo", "github.com", "main", "feature/x", "aaa111")
		require.NoError(t, err)
		assert.False(t, merged)
		assert.Equal(t, 1, calls, "exact cleanup verification must not use branch-name history")
	})

	t.Run("no merged PR found", func(t *testing.T) {
		previousAPI := githubAPICall
		githubAPICall = func(_ context.Context, endpoint string) ([]byte, error) {
			return []byte(`[]`), nil
		}
		t.Cleanup(func() { githubAPICall = previousAPI })

		merged, err := MergedPRHead(GitHub, "owner/repo", "github.com", "main", "feature/x", "aaa111")
		require.NoError(t, err)
		assert.False(t, merged)
	})

	t.Run("sha lookup error does not fall back", func(t *testing.T) {
		previousAPI := githubAPICall
		calls := 0
		githubAPICall = func(_ context.Context, endpoint string) ([]byte, error) {
			calls++
			return nil, assert.AnError
		}
		t.Cleanup(func() { githubAPICall = previousAPI })

		_, err := MergedPRHead(GitHub, "owner/repo", "github.com", "main", "feature/x", "aaa111")
		assert.ErrorIs(t, err, assert.AnError)
		assert.Equal(t, 1, calls, "should not fall back on error")
	})
}

func TestMergedPRHeadGitLab(t *testing.T) {
	previousAPI := glabAPICall
	glabAPICall = func(_ context.Context, host, endpoint string) ([]byte, error) {
		assert.Equal(t, "gitlab.example", host)
		assert.Equal(t, "projects/group%2Fproj/merge_requests?state=merged&source_branch=feature%2Fx&target_branch=main&per_page=100", endpoint)
		return []byte(`[{"state":"merged","source_branch":"feature/x","target_branch":"main","sha":"aaa111"}]`), nil
	}
	t.Cleanup(func() { glabAPICall = previousAPI })

	merged, err := MergedPRHead(GitLab, "group/proj", "gitlab.example", "main", "feature/x", "aaa111")
	require.NoError(t, err)
	assert.True(t, merged)
}

func TestMergedPRHeadError(t *testing.T) {
	previousAPI := githubAPICall
	githubAPICall = func(context.Context, string) ([]byte, error) { return nil, assert.AnError }
	t.Cleanup(func() { githubAPICall = previousAPI })

	_, err := MergedPRHead(GitHub, "owner/repo", "github.com", "main", "feature", "aaa111")
	assert.ErrorIs(t, err, assert.AnError)

	_, err = MergedPRHead(Type("bitbucket"), "owner/repo", "bitbucket.org", "main", "feature", "aaa111")
	assert.Error(t, err)
}

// A busy repository has more branches and reviews than any picker session
// works through, so the walk that follows pagination to its end needs a stop
// of its own. These cover where each forge's walk stops.
func TestGitHubPRListStopsAtTheRowBudget(t *testing.T) {
	previous := githubGraphQLCall
	t.Cleanup(func() { githubGraphQLCall = previous })

	nodes := make([]string, 0, forgePageSize)
	for number := 1; number <= forgePageSize; number++ {
		nodes = append(nodes, fmt.Sprintf(`{"number":%d,"title":"pr","headRefName":"branch-%d"}`, number, number))
	}
	full := strings.Join(nodes, ",")

	// A forge that always has one more page: only the budget ends this walk.
	calls := 0
	githubGraphQLCall = func(context.Context, string, map[string]string) ([]byte, error) {
		calls++
		return fmt.Appendf(nil,
			`{"data":{"repository":{"pullRequests":{"nodes":[%s],"pageInfo":{"hasNextPage":true,"endCursor":"cursor-%d"}}}}}`,
			full, calls), nil
	}

	prs, err := githubPRList(context.Background(), "owner/repo")

	require.NoError(t, err)
	assert.Len(t, prs, forgeRowBudget)
	assert.Equal(t, forgePageBudget, calls)
}

func TestGitHubPRListStopsAtThePageBudgetWhenPagesAreEmpty(t *testing.T) {
	previous := githubGraphQLCall
	t.Cleanup(func() { githubGraphQLCall = previous })

	// Empty pages spend no budget, so the page count is what has to end this.
	calls := 0
	githubGraphQLCall = func(context.Context, string, map[string]string) ([]byte, error) {
		calls++
		return fmt.Appendf(nil,
			`{"data":{"repository":{"pullRequests":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"cursor-%d"}}}}}`,
			calls), nil
	}

	prs, err := githubPRList(context.Background(), "owner/repo")

	require.NoError(t, err)
	assert.Empty(t, prs)
	assert.Equal(t, forgePageBudget, calls)
}

// endlessNDJSON prints one record without end, standing in for a glab that
// keeps paginating. Nothing but the row budget can end a read of it, so a test
// that stops is a test whose budget worked.
type endlessNDJSON struct {
	record  string
	pending string
}

func (r *endlessNDJSON) Read(out []byte) (int, error) {
	if r.pending == "" {
		r.pending = r.record
	}
	count := copy(out, r.pending)
	r.pending = r.pending[count:]
	return count, nil
}

// stubGlabStream stands in for runForgeCLIStream, which stops the CLI and
// reports success when its consumer is done.
func stubGlabStream(t *testing.T, record string) {
	t.Helper()
	previous := glabAPIStreamCall
	t.Cleanup(func() { glabAPIStreamCall = previous })
	glabAPIStreamCall = func(_ context.Context, _, _ string, consume func(io.Reader) error) error {
		err := consume(&endlessNDJSON{record: record})
		if errors.Is(err, errStopStream) {
			return nil
		}
		return err
	}
}

func TestGitLabMRBatchesStopAtTheRowBudget(t *testing.T) {
	stubGlabStream(t, "{\"iid\":1,\"title\":\"first\",\"source_branch\":\"one\"}\n")

	delivered := 0
	err := gitlabMRBatches(context.Background(), "group/repo", "gitlab.example", func(batch []PRInfo) error {
		delivered += len(batch)
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, forgeRowBudget, delivered)
}

func TestGitLabBranchBatchesStopAtTheRowBudget(t *testing.T) {
	stubGlabStream(t, "{\"name\":\"one\",\"commit\":{\"committed_date\":\"2026-08-01T10:00:00Z\"}}\n")

	delivered := 0
	err := gitlabBranchBatches(context.Background(), "group/repo", "gitlab.example", func(batch []BranchInfo) error {
		delivered += len(batch)
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, forgeRowBudget, delivered)
}
