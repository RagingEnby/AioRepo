package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// candidatesFile is the queue of sources waiting for a security audit.
// Discovery and link crawling only ever write here. A source is scraped only
// after the find-altstore-sources audit moves it to urls.txt.
const candidatesFile = "candidates.json"

type candidate struct {
	URL       string  `json:"url"`
	Origin    string  `json:"origin"` // github-search, github-topic, or link
	Repo      string  `json:"repo,omitempty"`
	Stars     int     `json:"stars"`
	Fork      bool    `json:"fork,omitempty"`
	Archived  bool    `json:"archived,omitempty"`
	PushedAt  string  `json:"pushedAt,omitempty"`
	Score     float64 `json:"score"`
	FirstSeen string  `json:"firstSeen"`
}

// scoreCandidate ranks how likely a source is to be trustworthy, from what
// GitHub says about the repo that hosts it. Stars count most. Forks,
// archived repos, and repos untouched for a year rank lower. Sources outside
// GitHub score 0 and wait behind every ranked one.
func scoreCandidate(c candidate, now time.Time) float64 {
	if c.Repo == "" {
		return 0
	}
	s := math.Log2(float64(c.Stars) + 2)
	if c.Fork {
		s *= 0.25
	}
	if c.Archived {
		s *= 0.5
	}
	if t, err := time.Parse(time.RFC3339, c.PushedAt); err == nil && now.Sub(t) > 365*24*time.Hour {
		s *= 0.5
	}
	return math.Round(s*100) / 100
}

func loadCandidates(path string) ([]candidate, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cs []candidate
	if err := json.Unmarshal(data, &cs); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cs, nil
}

// mergeCandidates adds new candidates to path and refreshes the GitHub facts
// of ones already there. URLs in known (urls.txt, rejected.txt, dead.txt)
// are dropped, so audited sources leave the queue. The file stays sorted by
// score, then URL.
func mergeCandidates(path string, add []candidate, known map[string]bool, now time.Time) (int, error) {
	cur, err := loadCandidates(path)
	if err != nil {
		return 0, err
	}
	byKey := map[string]*candidate{}
	var out []*candidate
	for i := range cur {
		c := &cur[i]
		k := normalizeURL(c.URL)
		if known[k] || byKey[k] != nil {
			continue
		}
		byKey[k] = c
		out = append(out, c)
	}
	added := 0
	for _, c := range add {
		k := normalizeURL(c.URL)
		if known[k] {
			continue
		}
		if prev := byKey[k]; prev != nil {
			if c.Repo != "" {
				prev.Repo, prev.Stars, prev.Fork, prev.Archived, prev.PushedAt = c.Repo, c.Stars, c.Fork, c.Archived, c.PushedAt
			}
			continue
		}
		c.FirstSeen = now.In(localTZ).Format(time.DateOnly)
		cp := c
		byKey[k] = &cp
		out = append(out, &cp)
		added++
	}
	for _, c := range out {
		c.Score = scoreCandidate(*c, now)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].URL < out[j].URL
	})
	list := make([]candidate, len(out))
	for i, c := range out {
		list[i] = *c
	}
	data, err := json.MarshalIndent(list, "", " ")
	if err != nil {
		return 0, err
	}
	return added, writeFileAtomic(path, append(data, '\n'))
}

// knownSources returns the normalized URLs of every source that is already
// decided: scraped, rejected by the audit, or dead.
func knownSources() (map[string]bool, error) {
	known := map[string]bool{}
	for _, path := range []string{manualFile, deadFile, rejectedFile} {
		urls, err := readURLFileOptional(path)
		if err != nil {
			return nil, err
		}
		for _, u := range urls {
			known[normalizeURL(u)] = true
		}
	}
	return known, nil
}

// runCandidates prints the top N queued URLs, one per line, for the audit.
func runCandidates(args []string) error {
	n := 25
	for i := 0; i < len(args); i++ {
		if args[i] == "-n" && i+1 < len(args) {
			v, err := strconv.Atoi(args[i+1])
			if err != nil || v <= 0 {
				return fmt.Errorf("-n wants a positive number")
			}
			n = v
			i++
		}
	}
	cs, err := loadCandidates(candidatesFile)
	if err != nil {
		return err
	}
	known, err := knownSources()
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, c := range cs {
		if n == 0 {
			break
		}
		if known[normalizeURL(c.URL)] {
			continue
		}
		b.WriteString(c.URL + "\n")
		n--
	}
	_, err = os.Stdout.WriteString(b.String())
	return err
}
