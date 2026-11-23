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

// Discovery searches GitHub for source files, keeps the ones that parse with
// at least one installable app, looks up the stars and activity of the repo
// that hosts each one, and queues them in candidates.json for a security
// audit. It never adds a source to the scrape. It needs GITHUB_TOKEN, since
// code search requires authentication.

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

	mu    sync.Mutex
	pause time.Duration
	last  time.Time
}

func (g *githubClient) wait(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if wait := g.pause - time.Since(g.last); wait > 0 {
		if err := sleepCtx(ctx, wait); err != nil {
			return err
		}
	}
	g.last = time.Now()
	return nil
}

func (g *githubClient) get(ctx context.Context, path string, params url.Values, out any) error {
	if err := g.wait(ctx); err != nil {
		return err
	}
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

func (g *githubClient) searchCode(ctx context.Context, query string) ([]candidate, error) {
	var out []candidate
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
			if len(out) > 0 {
				return out, nil
			}
			return nil, err
		}
		for _, it := range res.Items {
			out = append(out, candidate{URL: rawURL(it.Repository.FullName, it.Path), Origin: "github-search", Repo: it.Repository.FullName})
		}
		if len(res.Items) < 100 {
			break
		}
	}
	return out, nil
}

// topicFiles lists the .json files at the root of repos tagged with the
// altstore-source topic.
func (g *githubClient) topicFiles(ctx context.Context) ([]candidate, error) {
	var res struct {
		Items []struct {
			FullName string `json:"full_name"`
		} `json:"items"`
	}
	if err := g.get(ctx, "/search/repositories", url.Values{"q": {"topic:altstore-source"}, "per_page": {"100"}}, &res); err != nil {
		return nil, err
	}
	var out []candidate
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
				out = append(out, candidate{URL: rawURL(repo.FullName, f.Name), Origin: "github-topic", Repo: repo.FullName})
			}
		}
	}
	return out, nil
}

type repoInfo struct {
	Stars    int    `json:"stargazers_count"`
	Fork     bool   `json:"fork"`
	Archived bool   `json:"archived"`
	PushedAt string `json:"pushed_at"`
}

// repoFacts fills in stars and activity for each candidate's repo. The core
// API allows 5000 requests an hour, so a few hundred repos are fine.
func repoFacts(ctx context.Context, token string, cs []candidate) {
	g := &githubClient{token: token, http: &http.Client{Timeout: 30 * time.Second}, base: "https://api.github.com"}
	repos := map[string]*repoInfo{}
	for _, c := range cs {
		if c.Repo != "" {
			repos[c.Repo] = nil
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for repo := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var info repoInfo
			if err := g.get(ctx, "/repos/"+repo, nil, &info); err != nil {
				return
			}
			mu.Lock()
			repos[repo] = &info
			mu.Unlock()
		}()
	}
	wg.Wait()
	for i := range cs {
		if info := repos[cs[i].Repo]; info != nil {
			cs[i].Stars, cs[i].Fork, cs[i].Archived, cs[i].PushedAt = info.Stars, info.Fork, info.Archived, info.PushedAt
		}
	}
}

func runDiscover(ctx context.Context) error {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return fmt.Errorf("GITHUB_TOKEN is not set")
	}
	g := &githubClient{token: token, http: &http.Client{Timeout: 30 * time.Second}, base: "https://api.github.com", pause: codeSearchPause}

	known, err := knownSources()
	if err != nil {
		return err
	}
	queued, err := loadCandidates(candidatesFile)
	if err != nil {
		return err
	}
	inQueue := map[string]bool{}
	for _, c := range queued {
		inQueue[normalizeURL(c.URL)] = true
	}

	seen := map[string]bool{}
	var found []candidate
	add := func(cs []candidate) {
		for _, c := range cs {
			if k := normalizeURL(c.URL); !known[k] && !seen[k] {
				seen[k] = true
				found = append(found, c)
			}
		}
	}
	for _, q := range discoverQueries {
		cs, err := g.searchCode(ctx, q)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: search %q: %v\n", q, err)
			continue
		}
		fmt.Printf("search %q: %d files\n", q, len(cs))
		add(cs)
	}
	if cs, err := g.topicFiles(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "warning: topic search: %v\n", err)
	} else {
		fmt.Printf("topic altstore-source: %d files\n", len(cs))
		add(cs)
	}
	fmt.Println("FOUND:", len(found))

	// Only new files need the slow fetch. Queued ones just get fresh facts.
	var fresh, again []candidate
	for _, c := range found {
		if inQueue[normalizeURL(c.URL)] {
			again = append(again, c)
		} else {
			fresh = append(fresh, c)
		}
	}
	valid := validateSources(ctx, newFetcher(), fresh)
	fmt.Println("NEW AND VALID:", len(valid))
	all := append(valid, again...)
	repoFacts(ctx, token, all)
	added, err := mergeCandidates(candidatesFile, all, known, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("QUEUED %d new candidates in %s\n", added, candidatesFile)
	return nil
}

// validateSources returns the candidates that parse and have at least one
// installable, non-PAL app.
func validateSources(ctx context.Context, f *fetcher, cs []candidate) []candidate {
	p := newParser(time.Now(), nil)
	ok := make([]bool, len(cs))
	var wg sync.WaitGroup
	for i, c := range cs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := f.get(ctx, c.URL, getOptions{attempts: 2})
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
	var out []candidate
	for i, c := range cs {
		if ok[i] {
			out = append(out, c)
		}
	}
	return out
}
