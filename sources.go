package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Source tiers. A lower tier wins a bundle ID collision. See mergeApps.
const (
	tierManual = iota // urls.txt, picked by hand
	tierChoco         // the Choco IPA library list
	tierFound         // appdb, discovered.txt, and links found in sources
)

const (
	manualFile     = "urls.txt"
	discoveredFile = "discovered.txt"
	deadFile       = "dead.txt"
)

// sourceURL is a source to fetch and where it came from.
type sourceURL struct {
	URL    string
	Origin string
	Tier   int
}

type urlList struct {
	origin string
	tier   int
	fetch  func(context.Context, *fetcher) ([]string, error)
	local  bool // a local file, so a failure is fatal
}

func urlLists() []urlList {
	return []urlList{
		{"urls.txt", tierManual, func(context.Context, *fetcher) ([]string, error) { return readURLFile(manualFile) }, true},
		{"choco", tierChoco, chocoRepos, false},
		{"appdb", tierFound, appdbRepos, false},
		{"discovered", tierFound, func(context.Context, *fetcher) ([]string, error) { return readURLFileOptional(discoveredFile) }, true},
	}
}

// repoURLs gathers source URLs from every list, drops dead ones, and removes
// duplicates. When a URL is in more than one list, the lowest tier is kept.
// The result is sorted by tier and then URL so runs are reproducible.
func repoURLs(ctx context.Context, f *fetcher) ([]sourceURL, error) {
	dead, err := readURLFileOptional(deadFile)
	if err != nil {
		return nil, err
	}
	deadSet := map[string]bool{}
	for _, u := range dead {
		deadSet[normalizeURL(u)] = true
	}

	lists := urlLists()
	type result struct {
		list urlList
		urls []string
		err  error
	}
	results := make([]result, len(lists))
	done := make(chan struct{})
	for i, l := range lists {
		go func() {
			urls, err := l.fetch(ctx, f)
			results[i] = result{l, urls, err}
			done <- struct{}{}
		}()
	}
	for range lists {
		<-done
	}

	best := map[string]sourceURL{}
	for _, r := range results {
		if r.err != nil {
			if r.list.local {
				return nil, fmt.Errorf("%s: %w", r.list.origin, r.err)
			}
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", r.list.origin, r.err)
			continue
		}
		fmt.Printf("%s: %d urls\n", r.list.origin, len(r.urls))
		for _, u := range r.urls {
			u = strings.TrimSpace(u)
			if u == "" {
				continue
			}
			key := normalizeURL(u)
			if deadSet[key] {
				continue
			}
			if prev, ok := best[key]; !ok || r.list.tier < prev.Tier {
				best[key] = sourceURL{URL: u, Origin: r.list.origin, Tier: r.list.tier}
			}
		}
	}
	out := make([]sourceURL, 0, len(best))
	for _, s := range best {
		out = append(out, s)
	}
	sortSourceURLs(out)
	return out, nil
}

func sortSourceURLs(s []sourceURL) {
	sort.Slice(s, func(i, j int) bool {
		if s[i].Tier != s[j].Tier {
			return s[i].Tier < s[j].Tier
		}
		return s[i].URL < s[j].URL
	})
}

func appdbRepos(ctx context.Context, f *fetcher) ([]string, error) {
	resp, err := f.get(ctx, "https://api.dbservices.to/v1.7/get_repos/?lang=en&lt=&st=&is_public=1&brand=appdb", getOptions{})
	if err != nil {
		return nil, err
	}
	var data struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body, &data); err != nil {
		return nil, err
	}
	urls := make([]string, 0, len(data.Data))
	for _, d := range data.Data {
		urls = append(urls, d.URL)
	}
	return urls, nil
}

var (
	chocoReposRe = regexp.MustCompile(`const\s+defaultRepos\s*=\s*(\[[\s\S]*?\]);`)
	looseURLRe   = regexp.MustCompile(`https?://[^\s"'\]]+`)
)

func chocoRepos(ctx context.Context, f *fetcher) ([]string, error) {
	// The page is on a free host that can take a minute to wake up.
	resp, err := f.get(ctx, "https://choco-ipa-library-v2.onrender.com/", getOptions{timeout: 90 * time.Second})
	if err != nil {
		return nil, err
	}
	return parseChocoPage(resp.Body)
}

func parseChocoPage(body []byte) ([]string, error) {
	m := chocoReposRe.FindSubmatch(body)
	if m == nil {
		return nil, fmt.Errorf("defaultRepos list not found in page (%d bytes)", len(body))
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

// readURLFile reads one URL per line and skips blank lines and # comments.
func readURLFile(path string) ([]string, error) {
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

// readURLFileOptional is readURLFile, but a missing file is an empty list.
func readURLFileOptional(path string) ([]string, error) {
	urls, err := readURLFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return urls, err
}

// appendURLFile adds urls that are not already in path, sorted, under a
// dated comment.
func appendURLFile(path, comment string, urls []string) error {
	if len(urls) == 0 {
		return nil
	}
	existing, err := readURLFileOptional(path)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, u := range existing {
		have[normalizeURL(u)] = true
	}
	var add []string
	for _, u := range urls {
		if k := normalizeURL(u); !have[k] {
			have[k] = true
			add = append(add, u)
		}
	}
	if len(add) == 0 {
		return nil
	}
	sort.Strings(add)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	var b strings.Builder
	if info, err := f.Stat(); err == nil && info.Size() > 0 {
		b.WriteString("\n")
	}
	b.WriteString("# " + comment + "\n")
	for _, u := range add {
		b.WriteString(u + "\n")
	}
	if _, err := f.WriteString(b.String()); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
