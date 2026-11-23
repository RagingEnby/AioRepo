package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	userAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36"
	maxBodyBytes = 64 << 20
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

func get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json,text/html;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	return body, nil
}

// repoURLs gathers source URLs from appdb, the Choco IPA library, and urls.txt.
func repoURLs(ctx context.Context) ([]string, error) {
	type result struct {
		name string
		urls []string
		err  error
	}
	fetchers := map[string]func(context.Context) ([]string, error){
		"appdb":    appdbRepos,
		"choco":    chocoRepos,
		"urls.txt": func(context.Context) ([]string, error) { return manualRepos("urls.txt") },
	}
	ch := make(chan result, len(fetchers))
	for name, f := range fetchers {
		go func() {
			urls, err := f(ctx)
			ch <- result{name, urls, err}
		}()
	}
	seen := map[string]bool{}
	var all []string
	for range fetchers {
		r := <-ch
		if r.err != nil {
			// urls.txt is local, so a failure there is a real error.
			if r.name == "urls.txt" {
				return nil, r.err
			}
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", r.name, r.err)
			continue
		}
		fmt.Printf("%s: %d urls\n", r.name, len(r.urls))
		for _, u := range r.urls {
			if u = strings.TrimSpace(u); u != "" && !seen[u] {
				seen[u] = true
				all = append(all, u)
			}
		}
	}
	return all, nil
}

func appdbRepos(ctx context.Context) ([]string, error) {
	body, err := get(ctx, "https://api.dbservices.to/v1.7/get_repos/?lang=en&lt=&st=&is_public=1&brand=appdb")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	urls := make([]string, 0, len(resp.Data))
	for _, d := range resp.Data {
		urls = append(urls, d.URL)
	}
	return urls, nil
}

var (
	chocoReposRe = regexp.MustCompile(`const\s+defaultRepos\s*=\s*(\[[\s\S]*?\]);`)
	looseURLRe   = regexp.MustCompile(`https?://[^\s"'\]]+`)
)

func chocoRepos(ctx context.Context) ([]string, error) {
	body, err := get(ctx, "https://choco-ipa-library-v2.onrender.com/")
	if err != nil {
		return nil, err
	}
	m := chocoReposRe.FindSubmatch(body)
	if m == nil {
		return nil, nil
	}
	var items []any
	if json.Unmarshal(m[1], &items) == nil {
		var urls []string
		for _, it := range items {
			if s, ok := it.(string); ok {
				urls = append(urls, s)
			}
		}
		return urls, nil
	}
	return looseURLRe.FindAllString(string(m[1]), -1), nil
}

func manualRepos(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var urls []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" && !strings.HasPrefix(line, "#") {
			urls = append(urls, line)
		}
	}
	return urls, sc.Err()
}
