package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseDate(t *testing.T) {
	cases := map[string]string{
		"2023-02-17":                "2023-02-17T00:00:00-05:00",
		"2023-2-7":                  "2023-02-07T00:00:00-05:00",
		"2023-02-17T12:00:00-06:00": "2023-02-17T13:00:00-05:00",
		"2023-02-17T12:00:00Z":      "2023-02-17T07:00:00-05:00",
		"2023-02-17T12:00:00.123Z":  "2023-02-17T07:00:00-05:00",
		"2023-02-17T12:00:00+0000":  "2023-02-17T07:00:00-05:00",
		"2023-07-17 12:00:00":       "2023-07-17T12:00:00-04:00",
		"17-02-2023":                "2023-02-17T00:00:00-05:00",
		"2023-17-02":                "2023-02-17T00:00:00-05:00",
	}
	for in, want := range cases {
		got, err := parseDate(in)
		if err != nil {
			t.Errorf("parseDate(%q): %v", in, err)
			continue
		}
		if s := got.Format(time.RFC3339); s != want {
			t.Errorf("parseDate(%q) = %s, want %s", in, s, want)
		}
	}
	if got, err := parseDate(json.Number("1676635200")); err != nil || !got.Equal(time.Unix(1676635200, 0)) {
		t.Errorf("unix seconds: %v %v", got, err)
	}
	if _, err := parseDate("not a date"); err == nil {
		t.Error("expected error for garbage date")
	}
}

func TestCleanURL(t *testing.T) {
	cases := map[string]string{
		"https://a.com/x.ipa":         "https://a.com/x.ipa",
		" https://a.com/my app.ipa ":  "https://a.com/my%20app.ipa",
		"https://a.com/x?n=a b&c=é":   "https://a.com/x?n=a%20b&c=%C3%A9",
		"https://a.com/%40user/x.ipa": "https://a.com/%40user/x.ipa",
	}
	for in, want := range cases {
		got, ok := cleanURL(in)
		if !ok || got != want {
			t.Errorf("cleanURL(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "ftp://a.com/x", "/relative/path", "itms-services://?x", "https://"} {
		if got, ok := cleanURL(bad); ok {
			t.Errorf("cleanURL(%q) = %q, want rejection", bad, got)
		}
	}
}

func TestTintColor(t *testing.T) {
	cases := map[string]string{
		"#abc": "#ABC", "ff00aa": "#FF00AA", "#80FF00AA": "#FF00AA",
		"": "", "red": "", "#12345": "", "#GGGGGG": "",
	}
	for in, want := range cases {
		if got := tintColor(in); got != want {
			t.Errorf("tintColor(%q) = %q, want %q", in, got, want)
		}
	}
}

func decode(t *testing.T, s string) object {
	t.Helper()
	var o object
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	if err := dec.Decode(&o); err != nil {
		t.Fatal(err)
	}
	return o
}

func TestParseAppLegacyLayout(t *testing.T) {
	app := parseApp(decode(t, `{
		"name": "Old", "bundleIdentifier": "com.old", "iconURL": "",
		"version": 1.5, "versionDate": "2020-1-2", "downloadURL": "https://a.com/old.ipa",
		"size": "1234", "versionDescription": "notes",
		"screenshotURLs": ["https://a.com/1.png", 5]
	}`))
	if app == nil {
		t.Fatal("app dropped")
	}
	if len(app.Versions) != 1 || app.Versions[0].Version != "1.5" || app.Versions[0].Size != 1234 {
		t.Fatalf("versions = %+v", app.Versions[0])
	}
	if app.DeveloperName != "Unknown" || app.LocalizedDescription != "Old" || app.IconURL != iconURL {
		t.Errorf("defaults not applied: %+v", app)
	}
	if app.VersionDescription != "notes" || app.DownloadURL != "https://a.com/old.ipa" {
		t.Errorf("legacy mirror fields: %+v", app)
	}
	if app.Screenshots == nil || len(app.Screenshots.IPhone) != 1 {
		t.Errorf("screenshots = %+v", app.Screenshots)
	}
}

func TestParseAppVersions(t *testing.T) {
	app := parseApp(decode(t, `{
		"name": "A", "bundleIdentifier": "com.a", "developerName": "Dev",
		"localizedDescription": "d", "iconURL": "https://a.com/i.png", "tintColor": "nope",
		"versions": [
			{"version": "1.0", "date": "2024-01-01", "downloadURL": "https://a.com/1.ipa", "size": 10},
			{"version": "2.0", "date": "2024-03-01", "downloadURL": "https://a.com/2.ipa", "size": 20.0},
			{"version": "2.0", "date": "2024-02-01", "downloadURL": "https://a.com/2old.ipa", "size": 20},
			{"version": "3.0", "date": "2024-04-01", "downloadURL": "", "size": 30},
			{"version": "4.0", "date": "garbage", "downloadURL": "https://a.com/4.ipa", "size": 40}
		],
		"appPermissions": {"entitlements": [{"name": "com.apple.x"}, "com.apple.y"], "privacy": [{"name": "Camera", "usageDescription": "cam"}]},
		"screenshots": {"ipad": [{"imageURL": "https://a.com/s.png", "width": 99999, "height": 10}]}
	}`))
	if app == nil {
		t.Fatal("app dropped")
	}
	var got []string
	for _, v := range app.Versions {
		got = append(got, v.Version+" "+v.DownloadURL)
	}
	want := []string{"2.0 https://a.com/2.ipa", "1.0 https://a.com/1.ipa"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("versions = %v, want %v", got, want)
	}
	if app.TintColor != "" {
		t.Errorf("bad tint color kept: %q", app.TintColor)
	}
	if !reflect.DeepEqual(app.AppPermissions.Entitlements, []string{"com.apple.x", "com.apple.y"}) {
		t.Errorf("entitlements = %v", app.AppPermissions.Entitlements)
	}
	if app.AppPermissions.Privacy["NSCameraUsageDescription"] != "cam" {
		t.Errorf("privacy = %v", app.AppPermissions.Privacy)
	}
	if s := app.Screenshots.IPad[0]; s.Width != 0 || s.Height != 0 {
		t.Errorf("out-of-range screenshot size kept: %+v", s)
	}
}

func TestParseAppRejects(t *testing.T) {
	for _, s := range []string{
		`{"bundleIdentifier": "com.a"}`,
		`{"name": "A"}`,
		`{"name": null, "bundleIdentifier": "com.a"}`,
	} {
		if app := parseApp(decode(t, s)); app != nil {
			t.Errorf("parseApp(%s) kept the app", s)
		}
	}
}

func TestParseSourceErrors(t *testing.T) {
	if _, err := parseSource([]byte(`{"apps": [{}]}`)); err != errNoName {
		t.Errorf("no name: %v", err)
	}
	if _, err := parseSource([]byte(`{"name": "x", "apps": []}`)); err != errNoApps {
		t.Errorf("no apps: %v", err)
	}
	if _, err := parseSource([]byte("\xEF\xBB\xBF" + `{"name": "x", "apps": [{"name": "a"}]}`)); err != nil {
		t.Errorf("bom: %v", err)
	}
}
