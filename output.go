package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	iconURL = "https://raw.githubusercontent.com/RagingEnby/RagingEnby/main/pfp.png"
	// siteDir is the GitHub Pages build. It is not committed.
	siteDir = "_site"
	// recentDays is how far back recent.json reaches.
	recentDays = 90
)

// sourceKind describes one output source.
type sourceKind struct {
	file        string // file name in the repo root or siteDir
	name        string
	identifier  string
	description string
}

var (
	kindMain = sourceKind{
		file:        "repo.json",
		name:        "RagingEnby's AIO Source",
		identifier:  "dev.ragingenby.aiorepo",
		description: "I got really sick and tired of typing in a billion different repos. So this one repo scrapes %d AltSources and combines them into one, beautiful, united repository.",
	}
	kindSpam = sourceKind{
		file:        "repo++.json",
		name:        "RagingEnby's AIO Source++",
		identifier:  "dev.ragingenby.aiorepo.spam",
		description: "RagingEnby's AIO Source but with ONLY spam apps (99% Nabzclan slop)",
	}
	kindRecent = sourceKind{
		file:        "recent.json",
		name:        "RagingEnby's AIO Source (Recent)",
		identifier:  "dev.ragingenby.aiorepo.recent",
		description: fmt.Sprintf("Apps from RagingEnby's AIO Source updated in the last %d days.", recentDays),
	}
)

func categoryKind(cat string) sourceKind {
	return sourceKind{
		file:        "category/" + cat + ".json",
		name:        "RagingEnby's AIO Source (" + cat + ")",
		identifier:  "dev.ragingenby.aiorepo." + cat,
		description: "Apps in the " + cat + " category from RagingEnby's AIO Source.",
	}
}

type buildOptions struct {
	now      time.Time
	sources  int // number of sources scraped, for the description
	news     []any
	featured []string
}

func buildSource(kind sourceKind, apps []*App, opt buildOptions) *Source {
	date := fmt.Sprintf("[%d/%d/%02d %02d:%02d]", opt.now.Month(), opt.now.Day(), opt.now.Year()%100, opt.now.Hour(), opt.now.Minute())
	desc := kind.description
	if kind == kindMain {
		desc = fmt.Sprintf(desc, opt.sources)
	}
	src := &Source{
		Name:         kind.name,
		Identifier:   kind.identifier,
		Description:  desc,
		Subtitle:     date + " Every single repo I could find, united.",
		IconURL:      iconURL,
		Website:      "https://ragingenby.dev/",
		PatreonURL:   "https://patreon.com/RagingEnby",
		FeaturedApps: opt.featured,
		News:         opt.news,
	}
	if kind != kindMain {
		src.Subtitle = date + " " + desc
	}
	sorted := append([]*App{}, apps...)
	sortApps(sorted)
	src.Apps = sorted
	if src.Apps == nil {
		src.Apps = []*App{}
	}
	if src.News == nil {
		src.News = []any{}
	}
	return src
}

// sortApps orders apps newest first, with ties broken by bundle ID so the
// output does not change between runs when nothing else did.
func sortApps(apps []*App) {
	sort.SliceStable(apps, func(i, j int) bool {
		ti, tj := apps[i].lastUpdated(), apps[j].lastUpdated()
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return apps[i].BundleIdentifier < apps[j].BundleIdentifier
	})
}

func encodeSource(src *Source, indent bool) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(src); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeIfChanged writes src to path unless the only change would be the
// timestamp in the subtitle. It reports whether it wrote.
func writeIfChanged(path string, src *Source) (bool, error) {
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err == nil {
		var prev struct {
			Subtitle string `json:"subtitle"`
		}
		if json.Unmarshal(old, &prev) == nil {
			cp := *src
			cp.Subtitle = prev.Subtitle
			same, err := encodeSource(&cp, true)
			if err != nil {
				return false, err
			}
			if bytes.Equal(same, old) {
				return false, nil
			}
		}
	}
	data, err := encodeSource(src, true)
	if err != nil {
		return false, err
	}
	return true, writeFileAtomic(path, data)
}

// writeMinified writes src without indentation, for the Pages build.
func writeMinified(path string, src *Source) error {
	data, err := encodeSource(src, false)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

// previousAppCount reads how many apps the committed file has, or -1.
func previousAppCount(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return -1
	}
	var prev struct {
		Apps []json.RawMessage `json:"apps"`
	}
	if json.Unmarshal(data, &prev) != nil {
		return -1
	}
	return len(prev.Apps)
}

// recentApps returns the apps updated within recentDays of now.
func recentApps(apps []*App, now time.Time) []*App {
	cutoff := now.AddDate(0, 0, -recentDays)
	var out []*App
	for _, a := range apps {
		if a.lastUpdated().After(cutoff) {
			out = append(out, a)
		}
	}
	return out
}

// byCategory groups apps by category. Apps without one are left out.
func byCategory(apps []*App) map[string][]*App {
	out := map[string][]*App{}
	for _, a := range apps {
		if a.Category != "" {
			out[a.Category] = append(out[a.Category], a)
		}
	}
	return out
}

// statusReport is status.json on the Pages site.
type statusReport struct {
	GeneratedAt string         `json:"generatedAt"`
	MainApps    int            `json:"mainApps"`
	SpamApps    int            `json:"spamApps"`
	Counts      map[string]int `json:"counts"`
	Sources     []sourceStatus `json:"sources"`
}

type sourceStatus struct {
	URL       string `json:"url"`
	Origin    string `json:"origin"`
	Tier      int    `json:"tier"`
	Status    string `json:"status"`
	Name      string `json:"name,omitempty"`
	Apps      int    `json:"apps"`
	Failures  int    `json:"failures,omitempty"`
	LatencyMS int64  `json:"latencyMs"`
	Error     string `json:"error,omitempty"`
}

func buildStatus(results []*sourceResult, mainCount, spamCount int, now time.Time) statusReport {
	rep := statusReport{
		GeneratedAt: now.Format(time.RFC3339),
		MainApps:    mainCount,
		SpamApps:    spamCount,
		Counts:      map[string]int{},
	}
	for _, r := range results {
		s := sourceStatus{
			URL: r.URL, Origin: r.Origin, Tier: r.Tier, Status: r.Status,
			Failures: r.Failures, LatencyMS: r.Latency.Milliseconds(), Error: r.Error,
		}
		if r.Parsed != nil {
			s.Name = r.Parsed.Name
			s.Apps = len(r.Parsed.Apps)
		}
		rep.Counts[r.Status]++
		rep.Sources = append(rep.Sources, s)
	}
	return rep
}

func writeJSON(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

// writeSite writes the minified sources, the extra sources, and status.json
// into siteDir for GitHub Pages.
func writeSite(main, spam []*App, opt buildOptions, status statusReport) error {
	if err := os.RemoveAll(siteDir); err != nil {
		return err
	}
	writes := map[sourceKind][]*App{
		kindMain:   main,
		kindSpam:   spam,
		kindRecent: recentApps(main, opt.now),
	}
	cats := byCategory(main)
	for cat, apps := range cats {
		writes[categoryKind(cat)] = apps
	}
	for kind, apps := range writes {
		o := opt
		if kind != kindMain {
			o.news, o.featured = nil, nil
		}
		if err := writeMinified(filepath.Join(siteDir, kind.file), buildSource(kind, apps, o)); err != nil {
			return err
		}
	}
	return writeJSON(filepath.Join(siteDir, "status.json"), status)
}
