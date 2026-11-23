package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNormalizeURL(t *testing.T) {
	cases := map[string]string{
		"https://Example.com/a/":      "https://example.com/a",
		"http://example.com:80/a":     "https://example.com/a",
		"https://example.com:443/a#x": "https://example.com/a",
		"https://example.com:8080/a/": "https://example.com:8080/a",
		" https://example.com ":       "https://example.com",
	}
	for in, want := range cases {
		if got := normalizeURL(in); got != want {
			t.Errorf("normalizeURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRules(t *testing.T) {
	rs, err := parseRules(`
# comment
subtitle prefix Injected with
developer is Holloway
name contains IPAVault
bundle regex ^com\.happymod\.
`)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		app  App
		want bool
	}{
		{App{Subtitle: "injected with Satella"}, true},
		{App{DeveloperName: "holloway"}, true},
		{App{DeveloperName: "Holloway Jr"}, false},
		{App{Name: "Vitrine IPAVault"}, true},
		{App{BundleIdentifier: "com.happymod.x"}, true},
		{App{BundleIdentifier: "com.nothappymod.x"}, false},
	}
	for _, c := range cases {
		if got := rs.match(&c.app); got != c.want {
			t.Errorf("match(%+v) = %v, want %v", c.app, got, c.want)
		}
	}
	for _, bad := range []string{"developer is", "colour is red", "name near x", "name regex ("} {
		if _, err := parseRules(bad); err == nil {
			t.Errorf("parseRules(%q) accepted a bad rule", bad)
		}
	}
}

func testApp(id, dev, version, date string) *App {
	a := &App{
		Name: id, BundleIdentifier: id, DeveloperName: dev,
		Versions: []*Version{{Version: version, Date: date, DownloadURL: "https://a.com/" + id + version + ".ipa"}},
	}
	a.syncLegacy()
	return a
}

func result(tier int, name string, apps ...*App) *sourceResult {
	return &sourceResult{
		sourceURL: sourceURL{URL: "https://" + name, Tier: tier},
		Status:    statusOK,
		Parsed:    &ParsedSource{Name: name, Apps: apps},
	}
}

func TestMergeTiers(t *testing.T) {
	trusted := testApp("com.x", "Dev", "1.0", "2024-01-01T00:00:00Z")
	hijack := testApp("com.x", "Someone", "9.0", "2025-01-01T00:00:00Z")
	update := testApp("com.x", "dev", "1.1", "2024-06-01T00:00:00Z")
	got := mergeApps([]*sourceResult{
		result(tierManual, "manual", trusted),
		result(tierFound, "appdb1", hijack),
		result(tierFound, "appdb2", update),
	}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d apps", len(got))
	}
	app := got[0]
	if app.DeveloperName != "dev" || app.Version != "1.1" {
		t.Errorf("pick = %s %s, want the same-developer update", app.DeveloperName, app.Version)
	}
	var versions []string
	for _, v := range app.Versions {
		versions = append(versions, v.Version)
	}
	if !reflect.DeepEqual(versions, []string{"1.1", "1.0"}) {
		t.Errorf("versions = %v, want [1.1 1.0] without the other developer's 9.0", versions)
	}
	if app.sourceName != "appdb2" {
		t.Errorf("sourceName = %q", app.sourceName)
	}
}

func TestMergeBlockAndCap(t *testing.T) {
	a := testApp("com.a", "Dev", "1", "2024-01-01T00:00:00Z")
	for i := 2; i <= 15; i++ {
		a.Versions = append(a.Versions, &Version{Version: strings.Repeat("v", i), Date: "2023-01-01T00:00:00Z", DownloadURL: "https://a.com/x"})
	}
	b := testApp("com.b", "Bad", "1", "2024-01-01T00:00:00Z")
	block, _ := parseRules("developer is bad")
	got := mergeApps([]*sourceResult{result(0, "s", a, b)}, block)
	if len(got) != 1 || got[0].BundleIdentifier != "com.a" {
		t.Fatalf("blocked app kept: %v", got)
	}
	if n := len(got[0].Versions); n != maxVersions {
		t.Errorf("versions = %d, want %d", n, maxVersions)
	}
}

func TestSortAppsTieBreak(t *testing.T) {
	apps := []*App{
		testApp("com.b", "", "1", "2024-01-01T00:00:00Z"),
		testApp("com.a", "", "1", "2024-01-01T00:00:00Z"),
		testApp("com.c", "", "1", "2025-01-01T00:00:00Z"),
	}
	sortApps(apps)
	var ids []string
	for _, a := range apps {
		ids = append(ids, a.BundleIdentifier)
	}
	if !reflect.DeepEqual(ids, []string{"com.c", "com.a", "com.b"}) {
		t.Errorf("order = %v", ids)
	}
}

func TestScarlet(t *testing.T) {
	seen := &seenState{Apps: map[string]string{}, Versions: map[string]string{"com.y@2": "2025-01-01T00:00:00Z"}}
	p := newParser(testNow, seen.versionDate(testNow))
	src, err := p.parseSource([]byte(`{
		"META": {"repoName": "Scarlet Test"},
		"Tweaked": [
			{"name": "Y", "version": "2", "down": "https://a.com/y.ipa", "bundleID": "com.y", "dev": "D", "icon": "https://a.com/y.png", "changelog": "fixes"},
			{"name": "Bad", "version": "1", "down": "", "bundleID": "com.bad"}
		],
		"Games": [{"name": "Z", "version": "1", "down": "https://a.com/z.ipa", "bundleID": "com.z", "screenshots": ["https://a.com/s.png"]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if src.Name != "Scarlet Test" || len(src.Apps) != 2 {
		t.Fatalf("source = %s with %d apps", src.Name, len(src.Apps))
	}
	z, y := src.Apps[0], src.Apps[1] // keys are read in sorted order
	if y.VersionDate != "2024-12-31T19:00:00-05:00" || y.VersionDescription != "fixes" || y.DeveloperName != "D" {
		t.Errorf("y = %+v", y)
	}
	if !z.lastUpdated().Equal(testNow) || seen.Versions["com.z@1"] == "" {
		t.Errorf("new scarlet version not dated now and recorded: %s %v", z.VersionDate, seen.Versions)
	}
	if z.Screenshots == nil || len(z.Screenshots.IPhone) != 1 {
		t.Errorf("screenshots = %+v", z.Screenshots)
	}
}

func TestHybridSourceUsesAltStoreHalf(t *testing.T) {
	src, err := testParser.parseSource([]byte(`{
		"name": "Hybrid", "META": {"repoName": "Hybrid"},
		"apps": [{"name": "E", "bundleIdentifier": "com.e", "bundleID": "com.e", "versions": [],
			"version": "2", "versionDate": "2026-05-18", "downloadURL": "https://a.com/e.ipa", "down": "https://a.com/e.ipa"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	a := src.Apps[0]
	if a.undated || a.VersionDate != "2026-05-18T00:00:00-04:00" {
		t.Errorf("hybrid app = undated %v, date %s; want the AltStore date", a.undated, a.VersionDate)
	}
}

func TestDatedCopyBeatsScarletCopy(t *testing.T) {
	scarlet := testApp("com.x", "D", "2", "2026-06-01T00:00:00Z")
	scarlet.undated = true
	dated := testApp("com.x", "D", "1", "2026-01-01T00:00:00Z")
	got := mergeApps([]*sourceResult{result(tierFound, "scarlet", scarlet), result(tierFound, "alt", dated)}, nil)
	if got[0].sourceName != "alt" {
		t.Errorf("pick came from %s, want the dated copy", got[0].sourceName)
	}
}

func TestSourceLinks(t *testing.T) {
	src, err := testParser.parseSource([]byte(`{
		"name": "S", "sourceURL": "https://s.com/self.json",
		"apps": [{"name": "A", "bundleIdentifier": "com.a",
			"localizedDescription": "Add altstore://source?url=https%3A%2F%2Fo.com%2Fother.json or sidestore://source?url=https://t.com/x.json)"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://s.com/self.json", "https://o.com/other.json", "https://t.com/x.json"}
	if !reflect.DeepEqual(src.Links, want) {
		t.Errorf("links = %v, want %v", src.Links, want)
	}
}

func TestInferCategory(t *testing.T) {
	cases := []struct{ name, desc, want string }{
		{"Delta", "Nintendo emulator for iOS", "games"},
		{"uYouPlus", "YouTube with no ads", "photo-video"},
		{"Discord++", "A tweaked chat client", "social"},
		{"Blank", "Nothing here", ""},
	}
	for _, c := range cases {
		if got := inferCategory(c.name, c.desc); got != c.want {
			t.Errorf("inferCategory(%q, %q) = %q, want %q", c.name, c.desc, got, c.want)
		}
	}
	a := &App{Name: "Thing", Category: "Tweaked Apps"}
	fixCategory(a)
	if a.Category != "other" {
		t.Errorf("unknown category became %q, want other", a.Category)
	}
}

func TestNews(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, localTZ)
	seen := &seenState{Apps: map[string]string{"com.old": "", "com.week": "2026-06-02"}, Versions: map[string]string{}}
	apps := []*App{{Name: "Old", BundleIdentifier: "com.old"}, {Name: "Week", BundleIdentifier: "com.week"}, {Name: "New", BundleIdentifier: "com.new"}}
	seen.markApps(apps, now)
	news := buildNews(apps, seen, now)
	if len(news) != 1 {
		t.Fatalf("news = %+v", news)
	}
	item := news[0].(NewsItem)
	if item.Identifier != "new-apps-2026-06-10" || item.Title != "1 new app" || item.Caption != "New" || item.TintColor == "" {
		t.Errorf("item = %+v", item)
	}

	seeded := &seenState{Apps: map[string]string{}, Versions: map[string]string{}, seeded: true}
	seeded.markApps(apps, now)
	if n := buildNews(apps, seeded, now); len(n) != 0 {
		t.Errorf("first run announced the whole catalog: %+v", n)
	}
}

func TestWriteIfChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repo.json")
	apps := []*App{testApp("com.a", "D", "1", "2024-01-01T00:00:00Z")}
	t1 := time.Date(2026, 1, 1, 1, 0, 0, 0, localTZ)
	if wrote, err := writeIfChanged(path, buildSource(kindMain, apps, buildOptions{now: t1})); err != nil || !wrote {
		t.Fatalf("first write: %v %v", wrote, err)
	}
	t2 := t1.Add(6 * time.Hour)
	if wrote, err := writeIfChanged(path, buildSource(kindMain, apps, buildOptions{now: t2})); err != nil || wrote {
		t.Errorf("timestamp-only change was written: %v %v", wrote, err)
	}
	apps = append(apps, testApp("com.b", "D", "1", "2024-01-01T00:00:00Z"))
	if wrote, err := writeIfChanged(path, buildSource(kindMain, apps, buildOptions{now: t2})); err != nil || !wrote {
		t.Errorf("real change not written: %v %v", wrote, err)
	}
	if n := previousAppCount(path); n != 2 {
		t.Errorf("previousAppCount = %d", n)
	}
}

func noSleep(context.Context, time.Duration) error { return nil }

func TestFetcherRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	f := newFetcher()
	f.sleep = noSleep
	resp, err := f.get(context.Background(), srv.URL, getOptions{})
	if err != nil || string(resp.Body) != "ok" || resp.ETag != `"v1"` {
		t.Fatalf("get = %+v, %v", resp, err)
	}
	if calls.Load() != 3 {
		t.Errorf("calls = %d, want 3", calls.Load())
	}

	calls.Store(0)
	notFound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.NotFound(w, r)
	}))
	defer notFound.Close()
	if _, err := f.get(context.Background(), notFound.URL, getOptions{}); err == nil || calls.Load() != 1 {
		t.Errorf("404 retried or accepted: calls=%d err=%v", calls.Load(), err)
	}
}

func TestScraperCacheAndStaleFallback(t *testing.T) {
	const body = `{"name": "S", "apps": [{"name": "A", "bundleIdentifier": "com.a", "versions": [{"version": "1", "date": "2024-01-01", "downloadURL": "https://a.com/a.ipa"}]}]}`
	var mode atomic.Value
	mode.Store("ok")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode.Load() {
		case "ok":
			if r.Header.Get("If-None-Match") == `"v1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"v1"`)
			w.Write([]byte(body))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	cache, err := loadCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	f := newFetcher()
	f.sleep = noSleep
	s := newScraper(f, cache, testParser, testNow)
	u := sourceURL{URL: srv.URL + "/s.json", Origin: "test"}

	run := func() *sourceResult { return s.scrapeAll(context.Background(), []sourceURL{u})[0] }
	if r := run(); r.Status != statusOK || len(r.Parsed.Apps) != 1 {
		t.Fatalf("first fetch: %+v", r)
	}
	if r := run(); r.Status != statusCached || r.Parsed == nil {
		t.Fatalf("conditional fetch: %+v", r)
	}
	mode.Store("down")
	if r := run(); r.Status != statusStale || r.Parsed == nil || r.Failures != 1 {
		t.Fatalf("stale fallback: %+v", r)
	}
	if err := cache.save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "index.json")); err != nil {
		t.Fatal(err)
	}

	// Past the stale limit the cached copy is no longer used.
	s.now = testNow.Add(staleLimit + time.Hour)
	if r := run(); r.Status != statusFailed || r.Parsed != nil {
		t.Errorf("expired cache still used: %+v", r)
	}
}

func TestDuplicates(t *testing.T) {
	a := testApp("com.a", "D", "1", "2024-01-01T00:00:00Z")
	r1 := result(0, "same", a)
	r2 := result(0, "same", a)
	r2.URL = "https://other"
	resolveDuplicates([]*sourceResult{r1, r2})
	if r1.Status != statusOK || r2.Status != statusDuplicate {
		t.Errorf("statuses = %s %s", r1.Status, r2.Status)
	}
}

func TestParseChocoPage(t *testing.T) {
	urls, err := parseChocoPage([]byte(`<script>const defaultRepos = ["https://a.com/x.json", "https://b.com/y.json"];</script>`))
	if err != nil || len(urls) != 2 {
		t.Errorf("urls = %v, %v", urls, err)
	}
	if _, err := parseChocoPage([]byte("<html></html>")); err == nil {
		t.Error("missing list was not an error")
	}
}

func TestCandidateQueue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidates.json")
	recent := testNow.Add(-24 * time.Hour).Format(time.RFC3339)
	stale := testNow.AddDate(-2, 0, 0).Format(time.RFC3339)
	add := []candidate{
		{URL: "https://raw.githubusercontent.com/a/popular/HEAD/s.json", Repo: "a/popular", Stars: 500, PushedAt: recent},
		{URL: "https://raw.githubusercontent.com/b/fork/HEAD/s.json", Repo: "b/fork", Stars: 500, Fork: true, PushedAt: recent},
		{URL: "https://raw.githubusercontent.com/c/old/HEAD/s.json", Repo: "c/old", Stars: 500, PushedAt: stale},
		{URL: "https://example.com/source.json", Origin: "link"},
		{URL: "https://done.example/source.json"},
	}
	known := map[string]bool{normalizeURL("https://done.example/source.json"): true}
	n, err := mergeCandidates(path, add, known, testNow)
	if err != nil || n != 4 {
		t.Fatalf("added %d, %v", n, err)
	}
	cs, _ := loadCandidates(path)
	var order []string
	for _, c := range cs {
		order = append(order, c.Repo)
	}
	if !reflect.DeepEqual(order, []string{"a/popular", "c/old", "b/fork", ""}) {
		t.Errorf("order = %v", order)
	}

	// Once audited, a source leaves the queue on the next merge.
	known[normalizeURL(add[0].URL)] = true
	if _, err := mergeCandidates(path, nil, known, testNow); err != nil {
		t.Fatal(err)
	}
	if cs, _ := loadCandidates(path); len(cs) != 3 || cs[0].Repo != "c/old" {
		t.Errorf("audited source still queued: %+v", cs)
	}
}

func TestLinkedSourcesSkipScraped(t *testing.T) {
	r := result(0, "s")
	r.Parsed.Links = []string{"https://s/", "https://other.com/x.json"}
	got := linkedSources([]*sourceResult{r}, []sourceURL{{URL: "https://s"}})
	if len(got) != 1 || got[0].URL != "https://other.com/x.json" || got[0].Origin != "link" {
		t.Errorf("links = %+v", got)
	}
}

func TestAppendURLFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.txt")
	if err := os.WriteFile(path, []byte("https://a.com/x.json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := appendURLFile(path, "c", []string{"https://A.com/x.json/", "https://b.com/y.json"}); err != nil {
		t.Fatal(err)
	}
	urls, _ := readURLFile(path)
	if !reflect.DeepEqual(urls, []string{"https://a.com/x.json", "https://b.com/y.json"}) {
		t.Errorf("urls = %v", urls)
	}
}
