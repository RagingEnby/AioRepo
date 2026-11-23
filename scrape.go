package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

// Fetch status of one source in a run.
const (
	statusOK        = "ok"        // fetched and parsed
	statusCached    = "cached"    // unchanged since last run (304)
	statusStale     = "stale"     // failed, so last run's copy was used
	statusFailed    = "failed"    // failed with no usable copy
	statusDuplicate = "duplicate" // same content as another URL in this run
)

// maxLinks caps how many links found inside sources are queued per run.
const maxLinks = 200

type sourceResult struct {
	sourceURL
	Status   string
	Error    string
	Latency  time.Duration
	Failures int
	Parsed   *ParsedSource
}

func (r *sourceResult) usable() bool {
	return r.Parsed != nil && (r.Status == statusOK || r.Status == statusCached || r.Status == statusStale)
}

type scraper struct {
	f     *fetcher
	cache *diskCache
	p     *parser
	now   time.Time
}

func newScraper(f *fetcher, cache *diskCache, p *parser, now time.Time) *scraper {
	return &scraper{f: f, cache: cache, p: p, now: now}
}

// scrapeAll fetches every source and returns one result per URL, in input
// order.
func (s *scraper) scrapeAll(ctx context.Context, urls []sourceURL) []*sourceResult {
	results := s.scrapeBatch(ctx, urls)
	// Results are in input order, so the same URL wins a duplicate every run.
	resolveDuplicates(results)
	return results
}

// linkedSources returns the source URLs mentioned inside the scraped sources
// that are not scraped themselves. They go to the audit queue, never
// straight into the scrape.
func linkedSources(results []*sourceResult, scraped []sourceURL) []candidate {
	known := map[string]bool{}
	for _, u := range scraped {
		known[normalizeURL(u.URL)] = true
	}
	var out []candidate
	for _, r := range results {
		if !r.usable() {
			continue
		}
		for _, link := range r.Parsed.Links {
			if k := normalizeURL(link); !known[k] && len(out) < maxLinks {
				known[k] = true
				out = append(out, candidate{URL: link, Origin: "link"})
			}
		}
	}
	return out
}

func (s *scraper) scrapeBatch(ctx context.Context, urls []sourceURL) []*sourceResult {
	results := make([]*sourceResult, len(urls))
	var wg sync.WaitGroup
	for i, u := range urls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = s.scrapeOne(ctx, u)
		}()
	}
	wg.Wait()
	return results
}

func (s *scraper) scrapeOne(ctx context.Context, u sourceURL) *sourceResult {
	r := &sourceResult{sourceURL: u}
	key := normalizeURL(u.URL)
	entry := s.cache.entry(key)
	var cond *validators
	if entry.HasBody {
		cond = &validators{ETag: entry.ETag, LastModified: entry.LastModified}
	}

	start := time.Now()
	resp, err := s.f.getUpgrading(ctx, u.URL, getOptions{cond: cond})
	r.Latency = time.Since(start)

	var body []byte
	if err == nil {
		if resp.NotModified {
			body, _ = s.cache.body(key)
			r.Status = statusCached
		} else {
			body = resp.Body
			r.Status = statusOK
		}
	}
	if body != nil {
		src, perr := s.p.parseSource(body)
		if perr == nil {
			r.Parsed = src
			var stored []byte
			if r.Status == statusOK {
				stored = body
			}
			if cerr := s.cache.success(key, stored, resp.ETag, resp.LastModified, s.now); cerr != nil {
				fmt.Println("cache write failed for", u.URL, cerr)
			}
			fmt.Println("GOT SOURCE:", src.Name, "APPS:", len(src.Apps))
			return r
		}
		err = perr
	} else if err == nil {
		err = fmt.Errorf("server sent 304 but no cached copy exists")
	}

	r.Error = err.Error()
	r.Failures = s.cache.failure(key)
	r.Status = statusFailed
	if entry.HasBody && s.now.Sub(entry.LastOK) < staleLimit {
		if old, ok := s.cache.body(key); ok {
			if src, perr := s.p.parseSource(old); perr == nil {
				r.Parsed = src
				r.Status = statusStale
			}
		}
	}
	fmt.Println("Error processing", u.URL, err, "->", r.Status)
	return r
}

// resolveDuplicates marks a result as a duplicate when an earlier result
// parsed to the same apps. Comparing parsed output catches the
// same file served from two URLs or behind a redirect.
func resolveDuplicates(results []*sourceResult) {
	seen := map[string]string{}
	for _, r := range results {
		if !r.usable() {
			continue
		}
		fp := fingerprint(r.Parsed)
		if first, ok := seen[fp]; ok {
			r.Status = statusDuplicate
			r.Error = "same content as " + first
			continue
		}
		seen[fp] = r.URL
	}
}

// fingerprint identifies a parsed source by its name and every app version.
func fingerprint(p *ParsedSource) string {
	h := sha256.New()
	fmt.Fprintln(h, p.Name, len(p.Apps))
	for _, a := range p.Apps {
		fmt.Fprintln(h, a.BundleIdentifier)
		for _, v := range a.Versions {
			fmt.Fprintln(h, v.Version, v.DownloadURL)
		}
	}
	return string(h.Sum(nil))
}
