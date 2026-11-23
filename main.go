// Command aiorepo scrapes many AltStore sources and merges their apps into
// repo.json (main source) and repo++.json (spam apps only).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	iconURL     = "https://github.com/RagingEnby/RagingEnby/blob/main/pfp.png?raw=true"
	concurrency = 32
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	urls, err := repoURLs(ctx)
	if err != nil {
		return err
	}
	fmt.Println("REPO URLS:", len(urls))

	sources := fetchSources(ctx, urls)
	fmt.Println("SOURCES OK:", len(sources))
	if len(sources) == 0 {
		return fmt.Errorf("no sources could be fetched, refusing to overwrite output")
	}

	// Keep the most recently updated copy of each bundle ID.
	byID := map[string]*App{}
	var order []string
	for _, src := range sources {
		for _, app := range src.Apps {
			if len(app.Versions) == 0 || app.isPAL {
				continue
			}
			prev, ok := byID[app.BundleIdentifier]
			if !ok {
				order = append(order, app.BundleIdentifier)
				byID[app.BundleIdentifier] = app
			} else if app.lastUpdated().After(prev.lastUpdated()) {
				byID[app.BundleIdentifier] = app
			}
		}
	}

	var mainApps, spam []*App
	for _, id := range order {
		if app := byID[id]; isSpam(app) {
			spam = append(spam, app)
		} else {
			mainApps = append(mainApps, app)
		}
	}

	now := time.Now().In(localTZ)
	if err := writeSource("repo.json", buildSource(mainApps, false, now)); err != nil {
		return err
	}
	if err := writeSource("repo++.json", buildSource(spam, true, now)); err != nil {
		return err
	}
	fmt.Printf("WROTE repo.json (%d apps), repo++.json (%d apps)\n", len(mainApps), len(spam))
	return nil
}

func fetchSources(ctx context.Context, urls []string) []*ParsedSource {
	results := make([]*ParsedSource, len(urls))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, u := range urls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			body, err := get(ctx, u)
			if err != nil {
				fmt.Println("Error processing", u, err)
				return
			}
			src, err := parseSource(body)
			if err != nil {
				fmt.Println("Error processing", u, err)
				return
			}
			fmt.Println("GOT SOURCE:", src.Name, "APPS:", len(src.Apps))
			results[i] = src
		}()
	}
	wg.Wait()

	var out []*ParsedSource
	for _, s := range results {
		if s != nil {
			out = append(out, s)
		}
	}
	return out
}

func isSpam(app *App) bool {
	switch {
	case strings.HasPrefix(app.Subtitle, "Injected with "):
		return true
	case app.DeveloperName == "Holloway", app.DeveloperName == "lscs3", app.DeveloperName == "polo":
		return true
	case strings.Contains(app.IconURL, "nabzclan"):
		return true
	case strings.Contains(app.DeveloperName, "CyPwn"):
		return true
	}
	return false
}

func buildSource(apps []*App, spam bool, now time.Time) *Source {
	date := fmt.Sprintf("[%d/%d/%02d %02d:%02d]", now.Month(), now.Day(), now.Year()%100, now.Hour(), now.Minute())
	src := &Source{
		Name:        "RagingEnby's AIO Source",
		Identifier:  "dev.ragingenby.aiorepo",
		Description: "I got really sick and tired of typing in a billion different repos. So this one repo scrapes 200-300 AltSources and combines them into one, beautiful, united repository.",
		Subtitle:    date + " Every single repo I could find, united.",
		IconURL:     iconURL,
		Website:     "https://ragingenby.dev/",
		PatreonURL:  "https://patreon.com/RagingEnby",
		News:        []any{},
	}
	if spam {
		src.Name += "++"
		src.Identifier += ".spam"
		src.Description = "RagingEnby's AIO Source but with ONLY spam apps (99% Nabzclan slop)"
		src.Subtitle = date + " " + src.Description
	}
	sort.SliceStable(apps, func(i, j int) bool { return apps[i].lastUpdated().After(apps[j].lastUpdated()) })
	src.Apps = apps
	if src.Apps == nil {
		src.Apps = []*App{}
	}
	return src
}

func writeSource(path string, src *Source) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(src); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
