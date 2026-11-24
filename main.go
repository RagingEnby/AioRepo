// Command aiorepo scrapes many AltStore sources and merges their apps into
// repo.json (main source) and repo++.json (spam apps only).
//
// Usage:
//
//	aiorepo            build the sources
//	aiorepo discover   search GitHub for new sources and queue them in candidates.json
//	aiorepo candidates [-n N]
//	                   print the top N queued sources that are not audited yet
//	aiorepo check URL... | -f FILE
//	                   print facts about candidate sources as JSON, for an audit
package main

import (
	"context"
	"fmt"
	"os"
	"time"
)

// minKeepRatio stops a run that would shrink repo.json below this share of
// its last size, which usually means a scraping bug or a large outage. Set
// AIOREPO_FORCE=1 to write anyway.
const minKeepRatio = 0.7

func main() {
	ctx := context.Background()
	var err error
	if len(os.Args) > 1 && os.Args[1] == "discover" {
		err = runDiscover(ctx)
	} else if len(os.Args) > 1 && os.Args[1] == "candidates" {
		err = runCandidates(os.Args[2:])
	} else if len(os.Args) > 1 && os.Args[1] == "check" {
		err = runCheck(ctx, os.Args[2:])
	} else if len(os.Args) > 1 {
		err = fmt.Errorf("unknown command %q", os.Args[1])
	} else {
		err = run(ctx)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	now := time.Now().In(localTZ).Truncate(time.Second)

	spamRules, err := loadRules(spamFile)
	if err != nil {
		return err
	}
	blockRules, err := loadRules(blockFile)
	if err != nil {
		return err
	}
	seen, err := loadSeen(seenFile)
	if err != nil {
		return err
	}
	cache, err := loadCache(cacheDir)
	if err != nil {
		return err
	}

	f := newFetcher()
	urls, err := repoURLs(ctx, f)
	if err != nil {
		return err
	}
	fmt.Println("REPO URLS:", len(urls))

	s := newScraper(f, cache, newParser(now, seen.versionDate(now)), now)
	results := s.scrapeAll(ctx, urls)
	if err := cache.save(); err != nil {
		return fmt.Errorf("save cache: %w", err)
	}

	usable := 0
	var dead []string
	for _, r := range results {
		if r.usable() && r.Status != statusDuplicate {
			usable++
		}
		if r.Status == statusFailed && r.Failures >= deadAfter {
			dead = append(dead, r.URL)
		}
	}
	fmt.Println("SOURCES OK:", usable)
	if usable == 0 {
		return fmt.Errorf("no sources could be fetched, refusing to overwrite output")
	}

	var mainApps, spam []*App
	for _, app := range mergeApps(results, blockRules) {
		fixCategory(app)
		app.attribute()
		if spamRules.match(app) {
			spam = append(spam, app)
		} else {
			mainApps = append(mainApps, app)
		}
	}

	if prev := previousAppCount(kindMain.file); prev > 0 && float64(len(mainApps)) < minKeepRatio*float64(prev) && os.Getenv("AIOREPO_FORCE") != "1" {
		return fmt.Errorf("repo.json would drop from %d to %d apps; set AIOREPO_FORCE=1 to write it anyway", prev, len(mainApps))
	}

	seen.markApps(mainApps, now)
	feat, err := featured(mainApps)
	if err != nil {
		return err
	}
	opt := buildOptions{now: now, sources: usable, news: buildNews(mainApps, seen, now), featured: feat}

	for _, w := range []struct {
		kind sourceKind
		apps []*App
		opt  buildOptions
	}{
		{kindMain, mainApps, opt},
		{kindSpam, spam, buildOptions{now: now, sources: usable}},
	} {
		wrote, err := writeIfChanged(w.kind.file, buildSource(w.kind, w.apps, w.opt))
		if err != nil {
			return err
		}
		if wrote {
			fmt.Printf("WROTE %s (%d apps)\n", w.kind.file, len(w.apps))
		} else {
			fmt.Printf("UNCHANGED %s (%d apps)\n", w.kind.file, len(w.apps))
		}
	}

	if err := seen.save(seenFile); err != nil {
		return err
	}
	if err := appendURLFile(deadFile, fmt.Sprintf("failed %d runs in a row as of %s", deadAfter, now.Format(time.DateOnly)), dead); err != nil {
		return err
	}
	// Links to other sources wait in the audit queue. They are not scraped
	// until the audit moves them to urls.txt.
	known, err := knownSources()
	if err != nil {
		return err
	}
	queue, err := loadCandidates(candidatesFile)
	if err != nil {
		return err
	}
	skip := map[string]bool{}
	for k := range known {
		skip[k] = true
	}
	for _, c := range queue {
		skip[normalizeURL(c.URL)] = true
	}
	queued, err := mergeCandidates(candidatesFile, linkedSources(results, urls, skip), known, now)
	if err != nil {
		return err
	}
	fmt.Printf("QUEUED %d linked sources for audit, DEAD %d\n", queued, len(dead))

	status := buildStatus(results, len(mainApps), len(spam), now)
	return writeSite(mainApps, spam, opt, status)
}
