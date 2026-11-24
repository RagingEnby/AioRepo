package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// rejectedFile lists sources a security audit turned down, so later
// searches skip them.
const rejectedFile = "rejected.txt"

// checkReport describes one candidate source for review.
// It holds facts only; the audit decides.
type checkReport struct {
	URL   string `json:"url"`
	Known string `json:"known,omitempty"` // list that already has it
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`

	Name        string `json:"name,omitempty"`
	FinalURL    string `json:"finalURL,omitempty"`
	HTTPS       bool   `json:"https"`
	Apps        int    `json:"apps"`
	Installable int    `json:"installable"` // has a version and is not PAL-only
	PALOnly     int    `json:"palOnly"`

	NewApps       int      `json:"newApps"` // installable bundle IDs not in repo.json or repo++.json
	NewAppSamples []string `json:"newAppSamples,omitempty"`
	// SpamMatches counts installable apps that spam.txt would send to repo++.json.
	SpamMatches int `json:"spamMatches"`

	Developers    map[string]int `json:"developers,omitempty"`
	DownloadHosts map[string]int `json:"downloadHosts,omitempty"`
	HTTPDownloads int            `json:"httpDownloads"`
	Links         []string       `json:"links,omitempty"`
}

// runCheck fetches each URL given on the command line or, with -f FILE,
// one per line in FILE, and prints a JSON array of checkReport.
func runCheck(ctx context.Context, args []string) error {
	var urls []string
	for i := 0; i < len(args); i++ {
		if args[i] == "-f" && i+1 < len(args) {
			list, err := readURLFile(args[i+1])
			if err != nil {
				return err
			}
			urls = append(urls, list...)
			i++
			continue
		}
		urls = append(urls, args[i])
	}
	if len(urls) == 0 {
		return fmt.Errorf("usage: aiorepo check URL... | aiorepo check -f FILE")
	}

	known := map[string]string{}
	for _, path := range []string{manualFile, deadFile, rejectedFile} {
		list, err := readURLFileOptional(path)
		if err != nil {
			return err
		}
		for _, u := range list {
			if _, ok := known[normalizeURL(u)]; !ok {
				known[normalizeURL(u)] = path
			}
		}
	}
	have := map[string]bool{}
	for _, path := range []string{kindMain.file, kindSpam.file} {
		ids, err := bundleIDs(path)
		if err != nil {
			return err
		}
		for _, id := range ids {
			have[id] = true
		}
	}
	spamRules, err := loadRules(spamFile)
	if err != nil {
		return err
	}

	f := newFetcher()
	p := newParser(time.Now(), nil)
	reports := make([]checkReport, len(urls))
	var wg sync.WaitGroup
	for i, u := range urls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reports[i] = checkOne(ctx, f, p, u, known[normalizeURL(u)], have, spamRules)
		}()
	}
	wg.Wait()

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(reports)
}

func checkOne(ctx context.Context, f *fetcher, p *parser, u, known string, have map[string]bool, spam ruleSet) checkReport {
	r := checkReport{URL: u, Known: known}
	resp, err := f.get(ctx, u, getOptions{attempts: 2})
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.FinalURL = resp.FinalURL
	r.HTTPS = strings.HasPrefix(resp.FinalURL, "https://")
	src, err := p.parseSource(resp.Body)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.OK = true
	r.Name = src.Name
	r.Apps = len(src.Apps)
	r.Links = src.Links
	r.Developers = map[string]int{}
	r.DownloadHosts = map[string]int{}
	var fresh []string
	for _, a := range src.Apps {
		if a.isPAL {
			r.PALOnly++
			continue
		}
		if len(a.Versions) == 0 {
			continue
		}
		r.Installable++
		r.Developers[a.DeveloperName]++
		a.sourceName = src.Name
		if spam.match(a) {
			r.SpamMatches++
		}
		if !have[a.BundleIdentifier] {
			fresh = append(fresh, a.Name+" ("+a.BundleIdentifier+")")
		}
		for _, v := range a.Versions {
			if du, err := url.Parse(v.DownloadURL); err == nil {
				r.DownloadHosts[strings.ToLower(du.Host)]++
				if du.Scheme == "http" {
					r.HTTPDownloads++
				}
			}
		}
	}
	sort.Strings(fresh)
	r.NewApps = len(fresh)
	r.NewAppSamples = fresh[:min(len(fresh), 15)]
	return r
}

func bundleIDs(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var src struct {
		Apps []struct {
			BundleIdentifier string `json:"bundleIdentifier"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(data, &src); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	ids := make([]string, len(src.Apps))
	for i, a := range src.Apps {
		ids[i] = a.BundleIdentifier
	}
	return ids, nil
}
