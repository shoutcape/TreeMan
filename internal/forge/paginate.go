package forge

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// githubPageConcurrency bounds how many pages of one paginated endpoint are
// requested at the same time.
const githubPageConcurrency = 8

// githubPageCall is the indirection point so tests can stub page requests.
var githubPageCall = ghAPIPage

// ghAPIPage requests a single page and returns its Link header with the body.
func ghAPIPage(ctx context.Context, endpoint string) (string, []byte, error) {
	raw, err := runForgeCLI(ctx, "gh", ghAPIPageArgs(endpoint), nil, "gh api "+endpoint)
	if err != nil {
		return "", nil, err
	}
	return splitHTTPResponse(raw)
}

func ghAPIPageArgs(endpoint string) []string {
	return []string{"api", endpoint, "--include"}
}

// fetchGitHubPages delivers every page of a paginated REST endpoint to onPage
// in request order.
//
// GitHub advertises the final page number in the Link header of the first
// response, so the remaining pages are requested concurrently instead of
// walking one "next" link at a time. Endpoints that omit rel="last" fall back
// to that sequential walk.
func fetchGitHubPages(ctx context.Context, endpoint string, onPage func(page []byte) error) error {
	link, body, err := githubPageCall(ctx, endpoint)
	if err != nil {
		return err
	}
	if err := onPage(body); err != nil {
		return err
	}

	if last := linkLastPage(link); last >= 2 {
		return fetchGitHubPageRange(ctx, endpoint, last, onPage)
	}

	for next := linkRelationURL(link, "next"); next != ""; next = linkRelationURL(link, "next") {
		link, body, err = githubPageCall(ctx, next)
		if err != nil {
			return err
		}
		if err := onPage(body); err != nil {
			return err
		}
	}
	return nil
}

// fetchGitHubPageRange requests pages 2..last with a fixed pool of workers
// while still handing them to onPage in page order.
//
// The page count comes from the forge, so nothing here may scale with it. The
// scheduling window advances only when the next ordered page is delivered,
// bounding active requests and retained out-of-order responses. Returning
// cancels and joins every worker.
func fetchGitHubPageRange(ctx context.Context, endpoint string, last int, onPage func(page []byte) error) error {
	type pageResult struct {
		page int
		body []byte
		err  error
	}

	ctx, cancel := context.WithCancel(ctx)
	workers := min(githubPageConcurrency, last-1)
	requests := make(chan int)
	results := make(chan pageResult, workers)

	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			for page := range requests {
				_, body, err := githubPageCall(ctx, pageEndpoint(endpoint, page))
				select {
				case results <- pageResult{page: page, body: body, err: err}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		group.Wait()
		close(results)
	}()
	// Unblocks every worker and then waits for them, so no request outlives
	// this call however it returns.
	defer func() {
		cancel()
		close(requests)
		for range results {
		}
	}()

	pending := make(map[int][]byte)
	next := 2
	nextToSchedule := 2
	for nextToSchedule < 2+workers {
		select {
		case requests <- nextToSchedule:
			nextToSchedule++
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for result := range results {
		if result.err != nil {
			return result.err
		}
		pending[result.page] = result.body
		for {
			body, buffered := pending[next]
			if !buffered {
				break
			}
			delete(pending, next)
			next++
			if err := onPage(body); err != nil {
				return err
			}
			if nextToSchedule <= last {
				select {
				case requests <- nextToSchedule:
					nextToSchedule++
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
		if next > last {
			return nil
		}
	}
	if next <= last {
		// The workers stopped without delivering every page, which only the
		// caller's context can cause.
		return ctx.Err()
	}
	return nil
}

func pageEndpoint(endpoint string, page int) string {
	separator := "?"
	if strings.Contains(endpoint, "?") {
		separator = "&"
	}
	return endpoint + separator + "page=" + strconv.Itoa(page)
}

// splitHTTPResponse separates the Link header from the body in the raw
// response that "gh api --include" prints.
func splitHTTPResponse(raw []byte) (string, []byte, error) {
	separator := bytes.Index(raw, []byte("\r\n\r\n"))
	width := 4
	if plain := bytes.Index(raw, []byte("\n\n")); plain >= 0 && (separator < 0 || plain < separator) {
		separator, width = plain, 2
	}
	if separator < 0 {
		return "", nil, errors.New("gh: response is missing a header block")
	}

	link := ""
	for _, line := range strings.Split(string(raw[:separator]), "\n") {
		name, value, found := strings.Cut(strings.TrimRight(line, "\r"), ":")
		if found && strings.EqualFold(strings.TrimSpace(name), "link") {
			link = strings.TrimSpace(value)
			break
		}
	}
	return link, raw[separator+width:], nil
}

// linkLastPage returns the page number of the rel="last" relation, or 0 when
// the header does not advertise one.
func linkLastPage(header string) int {
	target := linkRelationURL(header, "last")
	if target == "" {
		return 0
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return 0
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return 0
	}
	page, err := strconv.Atoi(query.Get("page"))
	if err != nil || page < 1 {
		return 0
	}
	return page
}

// linkRelationURL returns the URL carrying the given relation in an RFC 8288
// Link header, or an empty string when it is absent.
func linkRelationURL(header, relation string) string {
	for _, entry := range strings.Split(header, ",") {
		target, parameters, found := strings.Cut(strings.TrimSpace(entry), ";")
		if !found {
			continue
		}
		target = strings.TrimSpace(target)
		if !strings.HasPrefix(target, "<") || !strings.HasSuffix(target, ">") {
			continue
		}
		for _, parameter := range strings.Split(parameters, ";") {
			name, value, found := strings.Cut(strings.TrimSpace(parameter), "=")
			if !found || strings.TrimSpace(name) != "rel" {
				continue
			}
			if strings.Trim(strings.TrimSpace(value), `"'`) == relation {
				return target[1 : len(target)-1]
			}
		}
	}
	return ""
}
