package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Discovery searches GitHub for source files, checks that each one parses
// with at least one installable app, and appends the new ones to
// discovered.txt. It needs GITHUB_TOKEN, since code search requires
// authentication.

// discoverQueries are GitHub code search queries. Each returns at most 1000
// results, so the AltStore query is split by file name.
var discoverQueries = func() []string {
	alt := `"bundleIdentifier" "downloadURL" "apps" extension:json`
	q := []string{alt}
	for _, name := range []string{"source.json", "apps.json", "altstore.json", "repo.json", "altsource.json", "sidestore.json", "app-repo.json", "store.json"} {
		q = append(q, alt+" filename:"+name)
	}
	return append(q, `"META" "repoName" "bundleID" extension:json`)
}()

// codeSearchPause keeps code search under its limit of 10 requests a minute.
const codeSearchPause = 7 * time.Second

type githubClient struct {
	token string
	http  *http.Client
	base  string
	pause time.Duration
	last  time.Time
}

func (g *githubClient) get(ctx context.Context, path string, params url.Values, out any) error {
	if wait := g.pause - time.Since(g.last); wait > 0 {
		if err := sleepCtx(ctx, wait); err != nil {
			return err
		}
	}
	g.last = time.Now()
	u := g.base + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return &httpError{resp.StatusCode}
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// rawURL is the raw.githubusercontent.com address of a file on the default
// branch.
func rawURL(repo, path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return "https://raw.githubusercontent.com/" + repo + "/HEAD/" + strings.Join(parts, "/")
}

func (g *githubClient) searchCode(ctx context.Context, query string) ([]string, error) {
	var urls []string
	for page := 1; page <= 10; page++ {
		var res struct {
			Items []struct {
				Path       string `json:"path"`
				Repository struct {
					FullName string `json:"full_name"`
				} `json:"repository"`
			} `json:"items"`
		}
		params := url.Values{"q": {query}, "per_page": {"100"}, "page": {fmt.Sprint(page)}}
		if err := g.get(ctx, "/search/code", params, &res); err != nil {
			// Search past the 1000th result answers 422. Keep what we have.
			if len(urls) > 0 {
				return urls, nil
			}
			return nil, err
		}
		for _, it := range res.Items {
			urls = append(urls, rawURL(it.Repository.FullName, it.Path))
		}
		if len(res.Items) < 100 {
			break
		}
	}
	return urls, nil
}

// topicFiles lists the .json files at the root of repos tagged with the
// altstore-source topic.
func (g *githubClient) topicFiles(ctx context.Context) ([]string, error) {
	var res struct {
		Items []struct {
			FullName string `json:"full_name"`
		} `json:"items"`
	}
	if err := g.get(ctx, "/search/repositories", url.Values{"q": {"topic:altstore-source"}, "per_page": {"100"}}, &res); err != nil {
		return nil, err
	}
	var urls []string
	for _, repo := range res.Items {
		var files []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		}
		if err := g.get(ctx, "/repos/"+repo.FullName+"/contents/", nil, &files); err != nil {
			continue
		}
		for _, f := range files {
			if f.Type == "file" && strings.HasSuffix(strings.ToLower(f.Name), ".json") {
				urls = append(urls, rawURL(repo.FullName, f.Name))
			}
		}
	}
	return urls, nil
}

func runDiscover(ctx context.Context) error {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return fmt.Errorf("GITHUB_TOKEN is not set")
	}
	g := &githubClient{token: token, http: &http.Client{Timeout: 30 * time.Second}, base: "https://api.github.com", pause: codeSearchPause}

	known := map[string]bool{}
	for _, path := range []string{manualFile, discoveredFile, deadFile, rejectedFile} {
		urls, err := readURLFileOptional(path)
		if err != nil {
			return err
		}
		for _, u := range urls {
			known[normalizeURL(u)] = true
		}
	}

	var candidates []string
	add := func(urls []string) {
		for _, u := range urls {
			if k := normalizeURL(u); !known[k] {
				known[k] = true
				candidates = append(candidates, u)
			}
		}
	}
	for _, q := range discoverQueries {
		urls, err := g.searchCode(ctx, q)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: search %q: %v\n", q, err)
			continue
		}
		fmt.Printf("search %q: %d files\n", q, len(urls))
		add(urls)
	}
	if urls, err := g.topicFiles(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "warning: topic search: %v\n", err)
	} else {
		fmt.Printf("topic altstore-source: %d files\n", len(urls))
		add(urls)
	}
	fmt.Println("CANDIDATES:", len(candidates))

	valid := validateSources(ctx, newFetcher(), candidates)
	fmt.Println("VALID:", len(valid))
	return appendURLFile(discoveredFile, "found by GitHub search on "+time.Now().In(localTZ).Format(time.DateOnly), valid)
}

// validateSources returns the candidates that parse and have at least one
// installable, non-PAL app.
func validateSources(ctx context.Context, f *fetcher, candidates []string) []string {
	p := newParser(time.Now(), nil)
	ok := make([]bool, len(candidates))
	var wg sync.WaitGroup
	for i, u := range candidates {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := f.get(ctx, u, getOptions{attempts: 2})
			if err != nil {
				return
			}
			src, err := p.parseSource(resp.Body)
			if err != nil {
				return
			}
			for _, a := range src.Apps {
				if len(a.Versions) > 0 && !a.isPAL {
					ok[i] = true
					return
				}
			}
		}()
	}
	wg.Wait()
	var out []string
	for i, u := range candidates {
		if ok[i] {
			out = append(out, u)
		}
	}
	return out
}
