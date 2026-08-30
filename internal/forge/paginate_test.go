package forge

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLinkRelationURL(t *testing.T) {
	header := `<https://api.github.com/repositories/1/branches?per_page=100&page=2>; rel="next", ` +
		`<https://api.github.com/repositories/1/branches?per_page=100&page=7>; rel="last"`

	assert.Equal(t, "https://api.github.com/repositories/1/branches?per_page=100&page=2", linkRelationURL(header, "next"))
	assert.Equal(t, "https://api.github.com/repositories/1/branches?per_page=100&page=7", linkRelationURL(header, "last"))
	assert.Equal(t, "", linkRelationURL(header, "prev"))
	assert.Equal(t, "", linkRelationURL("", "next"))
}

func TestLinkLastPage(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   int
	}{
		{
			name: "final page number",
			header: `<https://api.github.com/repositories/1/branches?per_page=100&page=2>; rel="next", ` +
				`<https://api.github.com/repositories/1/branches?per_page=100&page=7>; rel="last"`,
			want: 7,
		},
		{name: "single page carries no link header", header: "", want: 0},
		{name: "last relation without a page parameter", header: `<https://api.github.com/x>; rel="last"`, want: 0},
		{name: "next relation only", header: `<https://api.github.com/x?page=2>; rel="next"`, want: 0},
		{name: "unparseable url", header: `<https://api.github.com/x?page=%zz>; rel="last"`, want: 0},
		{name: "non-numeric page", header: `<https://api.github.com/x?page=last>; rel="last"`, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, linkLastPage(test.header))
		})
	}
}

func TestSplitHTTPResponse(t *testing.T) {
	raw := []byte("HTTP/2.0 200 OK\nLink: <https://x?page=3>; rel=\"last\"\r\nContent-Type: application/json\r\n\r\n[{\"name\":\"one\"}]")

	link, body, err := splitHTTPResponse(raw)

	require.NoError(t, err)
	assert.Equal(t, `<https://x?page=3>; rel="last"`, link)
	assert.Equal(t, `[{"name":"one"}]`, string(body))
}

func TestSplitHTTPResponseWithoutLinkHeader(t *testing.T) {
	raw := []byte("HTTP/2.0 200 OK\r\nContent-Type: application/json\r\n\r\n[]")

	link, body, err := splitHTTPResponse(raw)

	require.NoError(t, err)
	assert.Equal(t, "", link)
	assert.Equal(t, "[]", string(body))
}

func TestSplitHTTPResponseRejectsMissingHeaderBlock(t *testing.T) {
	_, _, err := splitHTTPResponse([]byte("HTTP/2.0 200 OK\r\nContent-Type: application/json\r\n"))

	require.EqualError(t, err, "gh: response is missing a header block")
}

func TestPageEndpoint(t *testing.T) {
	assert.Equal(t, "repos/owner/repo/branches?per_page=100&page=3", pageEndpoint("repos/owner/repo/branches?per_page=100", 3))
	assert.Equal(t, "repos/owner/repo/branches?page=2", pageEndpoint("repos/owner/repo/branches", 2))
}

func TestFetchGitHubPagesDeliversEveryPageInRequestOrder(t *testing.T) {
	previous := githubPageCall
	t.Cleanup(func() { githubPageCall = previous })

	// Later pages resolve first so ordered delivery cannot come from timing.
	release := make([]chan struct{}, 4)
	for index := range release {
		release[index] = make(chan struct{})
	}
	githubPageCall = func(endpoint string) (string, []byte, error) {
		switch endpoint {
		case "repos/owner/repo/branches?per_page=100":
			return `<https://api.github.com/x?page=2>; rel="next", <https://api.github.com/x?page=4>; rel="last"`,
				[]byte(`[{"name":"one"}]`), nil
		case "repos/owner/repo/branches?per_page=100&page=2":
			<-release[2]
			return "", []byte(`[{"name":"two"}]`), nil
		case "repos/owner/repo/branches?per_page=100&page=3":
			<-release[3]
			close(release[2])
			return "", []byte(`[{"name":"three"}]`), nil
		case "repos/owner/repo/branches?per_page=100&page=4":
			close(release[3])
			return "", []byte(`[{"name":"four"}]`), nil
		}
		return "", nil, fmt.Errorf("unexpected endpoint %q", endpoint)
	}

	var pages []string
	err := fetchGitHubPages("repos/owner/repo/branches?per_page=100", func(page []byte) error {
		pages = append(pages, string(page))
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, []string{
		`[{"name":"one"}]`,
		`[{"name":"two"}]`,
		`[{"name":"three"}]`,
		`[{"name":"four"}]`,
	}, pages)
}

func TestFetchGitHubPagesRequestsRemainingPagesConcurrently(t *testing.T) {
	previous := githubPageCall
	t.Cleanup(func() { githubPageCall = previous })

	var waitGroup sync.WaitGroup
	waitGroup.Add(3)
	githubPageCall = func(endpoint string) (string, []byte, error) {
		if endpoint == "repos/owner/repo/branches?per_page=100" {
			return `<https://api.github.com/x?page=4>; rel="last"`, []byte(`[]`), nil
		}
		// Blocks until every remaining page is in flight; a sequential
		// implementation deadlocks here and fails the test by timeout.
		waitGroup.Done()
		waitGroup.Wait()
		return "", []byte(`[]`), nil
	}

	err := fetchGitHubPages("repos/owner/repo/branches?per_page=100", func([]byte) error { return nil })

	require.NoError(t, err)
}

func TestFetchGitHubPagesReportsPageFailure(t *testing.T) {
	previous := githubPageCall
	t.Cleanup(func() { githubPageCall = previous })

	githubPageCall = func(endpoint string) (string, []byte, error) {
		if endpoint == "repos/owner/repo/branches?per_page=100" {
			return `<https://api.github.com/x?page=2>; rel="last"`, []byte(`[]`), nil
		}
		return "", nil, assert.AnError
	}

	err := fetchGitHubPages("repos/owner/repo/branches?per_page=100", func([]byte) error { return nil })

	assert.ErrorIs(t, err, assert.AnError)
}

func TestFetchGitHubPagesStopsWhenTheConsumerFails(t *testing.T) {
	previous := githubPageCall
	t.Cleanup(func() { githubPageCall = previous })

	githubPageCall = func(string) (string, []byte, error) { return "", []byte(`[]`), nil }

	err := fetchGitHubPages("repos/owner/repo/branches?per_page=100", func([]byte) error { return assert.AnError })

	assert.ErrorIs(t, err, assert.AnError)
}

func TestFetchGitHubPagesWalksNextLinksWhenNoFinalPageIsAdvertised(t *testing.T) {
	previous := githubPageCall
	t.Cleanup(func() { githubPageCall = previous })

	githubPageCall = func(endpoint string) (string, []byte, error) {
		switch endpoint {
		case "repos/owner/repo/branches?per_page=100":
			return `<https://api.github.com/x?page=2>; rel="next"`, []byte(`[{"name":"one"}]`), nil
		case "https://api.github.com/x?page=2":
			return `<https://api.github.com/x?page=3>; rel="next"`, []byte(`[{"name":"two"}]`), nil
		case "https://api.github.com/x?page=3":
			return "", []byte(`[{"name":"three"}]`), nil
		}
		return "", nil, fmt.Errorf("unexpected endpoint %q", endpoint)
	}

	var pages []string
	err := fetchGitHubPages("repos/owner/repo/branches?per_page=100", func(page []byte) error {
		pages = append(pages, string(page))
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, []string{`[{"name":"one"}]`, `[{"name":"two"}]`, `[{"name":"three"}]`}, pages)
}

func TestGitHubPageArgsRequestResponseHeaders(t *testing.T) {
	assert.Equal(t, []string{"api", "repos/owner/repo/branches?per_page=100", "--include"},
		ghAPIPageArgs("repos/owner/repo/branches?per_page=100"))
}

func TestBranchListPagesStreamsEachGitHubPage(t *testing.T) {
	previous := githubPageCall
	t.Cleanup(func() { githubPageCall = previous })

	githubPageCall = func(endpoint string) (string, []byte, error) {
		if endpoint == "repos/owner/repo/branches?per_page=100" {
			return `<https://api.github.com/x?page=2>; rel="last"`,
				[]byte(`[{"name":"one","commit":{"commit":{"committer":{"date":"2026-08-01T10:00:00Z"}}}}]`), nil
		}
		return "", []byte(`[{"name":"two","commit":{"commit":{"committer":{"date":"2026-08-02T10:00:00Z"}}}}]`), nil
	}

	var pages [][]string
	err := BranchListPages(GitHub, "owner/repo", "github.com", func(branches []BranchInfo) error {
		names := make([]string, 0, len(branches))
		for _, branch := range branches {
			names = append(names, branch.Name)
		}
		pages = append(pages, names)
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, [][]string{{"one"}, {"two"}}, pages)
}

func TestPRListPagesStreamsEachGitHubPage(t *testing.T) {
	previous := githubGraphQLCall
	t.Cleanup(func() { githubGraphQLCall = previous })

	githubGraphQLCall = func(_ string, variables map[string]string) ([]byte, error) {
		if variables["cursor"] == "" {
			return []byte(`{"data":{"repository":{"pullRequests":{"nodes":[{"number":1,"title":"first","headRefName":"one"}],"pageInfo":{"hasNextPage":true,"endCursor":"c1"}}}}}`), nil
		}
		return []byte(`{"data":{"repository":{"pullRequests":{"nodes":[{"number":2,"title":"second","headRefName":"two"}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`), nil
	}

	var pages [][]PRInfo
	err := PRListPages(GitHub, "owner/repo", "github.com", func(prs []PRInfo) error {
		pages = append(pages, prs)
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, [][]PRInfo{
		{{Number: 1, Title: "first", Branch: "one"}},
		{{Number: 2, Title: "second", Branch: "two"}},
	}, pages)
}

func TestPRListPagesStopsWhenTheConsumerFails(t *testing.T) {
	previous := githubGraphQLCall
	t.Cleanup(func() { githubGraphQLCall = previous })

	calls := 0
	githubGraphQLCall = func(string, map[string]string) ([]byte, error) {
		calls++
		return []byte(`{"data":{"repository":{"pullRequests":{"nodes":[{"number":1,"title":"first","headRefName":"one"}],"pageInfo":{"hasNextPage":true,"endCursor":"c1"}}}}}`), nil
	}

	err := PRListPages(GitHub, "owner/repo", "github.com", func([]PRInfo) error { return assert.AnError })

	assert.ErrorIs(t, err, assert.AnError)
	assert.Equal(t, 1, calls)
}

func TestBranchListPagesStreamsGitLabAsASinglePage(t *testing.T) {
	previous := glabAPICall
	t.Cleanup(func() { glabAPICall = previous })

	glabAPICall = func(string, string) ([]byte, error) {
		return []byte(`[{"name":"one","commit":{"committed_date":"2026-08-01T10:00:00Z"}}][{"name":"two","commit":{"committed_date":"2026-08-02T10:00:00Z"}}]`), nil
	}

	pages := 0
	var branches []BranchInfo
	err := BranchListPages(GitLab, "group/repo", "gitlab.example", func(page []BranchInfo) error {
		pages++
		branches = append(branches, page...)
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 1, pages)
	assert.Equal(t, []string{"one", "two"}, []string{branches[0].Name, branches[1].Name})
}

func TestBranchListPagesRejectsUnknownForge(t *testing.T) {
	err := BranchListPages(Type("bitbucket"), "owner/repo", "bitbucket.org", func([]BranchInfo) error { return nil })
	assert.Error(t, err)

	err = PRListPages(Type("bitbucket"), "owner/repo", "bitbucket.org", func([]PRInfo) error { return nil })
	assert.Error(t, err)
}
