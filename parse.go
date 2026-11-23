package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Input parsing. Third-party sources disagree on types (numbers for version
// strings, strings for sizes, several screenshot and permission layouts), so
// everything is read from generic JSON and converted to the strict output
// types in model.go. Bad apps and versions are skipped, not fatal.

var (
	errNoName = errors.New("source has no name")
	errNoApps = errors.New("source has no apps")
)

type object = map[string]any

// ParsedSource is a third-party source after its apps were normalized.
type ParsedSource struct {
	Name string
	Apps []*App
}

func parseSource(data []byte) (*ParsedSource, error) {
	data = trimBOM(data)
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode json: %w", err)
	}
	obj, ok := raw.(object)
	if !ok {
		return nil, errors.New("source is not a json object")
	}
	name := str(obj["name"])
	if name == "" {
		return nil, errNoName
	}
	rawApps, _ := obj["apps"].([]any)
	if len(rawApps) == 0 {
		return nil, errNoApps
	}
	src := &ParsedSource{Name: name}
	for _, ra := range rawApps {
		ao, ok := ra.(object)
		if !ok {
			continue
		}
		if app := parseApp(ao); app != nil {
			src.Apps = append(src.Apps, app)
		}
	}
	return src, nil
}

func trimBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}

// parseApp returns nil when the app cannot be made valid for AltStore.
func parseApp(o object) *App {
	name := str(o["name"])
	bundleID := str(o["bundleIdentifier"])
	if name == "" || bundleID == "" {
		return nil
	}
	app := &App{
		Name:                 name,
		BundleIdentifier:     bundleID,
		DeveloperName:        str(o["developerName"]),
		Subtitle:             str(o["subtitle"]),
		LocalizedDescription: str(o["localizedDescription"]),
		Category:             strings.ToLower(str(o["category"])),
		TintColor:            tintColor(str(o["tintColor"])),
		Beta:                 o["beta"] == true,
		isPAL:                str(o["marketplaceID"]) != "",
	}
	if app.DeveloperName == "" {
		app.DeveloperName = "Unknown"
	}
	if app.LocalizedDescription == "" {
		app.LocalizedDescription = name
	}
	if icon, ok := cleanURL(str(o["iconURL"])); ok {
		app.IconURL = icon
	} else {
		app.IconURL = iconURL
	}

	app.Screenshots = parseScreenshots(o["screenshots"])
	if app.Screenshots == nil {
		app.Screenshots = parseScreenshots(o["screenshotURLs"])
	}
	app.AppPermissions = parsePermissions(o["appPermissions"])
	app.Patreon = parsePatreon(o["patreon"])

	var versions []*Version
	if list, ok := o["versions"].([]any); ok {
		for _, rv := range list {
			if vo, ok := rv.(object); ok {
				if v := parseVersion(vo, "date"); v != nil {
					versions = append(versions, v)
				}
			}
		}
	} else if v := parseVersion(o, "versionDate"); v != nil {
		// Legacy single-version layout with fields on the app itself.
		if v.LocalizedDescription == "" {
			v.LocalizedDescription = str(o["versionDescription"])
		}
		versions = append(versions, v)
	}
	app.Versions = dedupeVersions(versions)
	if len(app.Versions) > 0 {
		latest := app.Versions[0]
		app.Version = latest.Version
		app.VersionDate = latest.Date
		app.VersionDescription = latest.LocalizedDescription
		app.DownloadURL = latest.DownloadURL
		app.Size = latest.Size
	}
	return app
}

func parseVersion(o object, dateKey string) *Version {
	version := str(o["version"])
	if version == "" {
		return nil
	}
	date, err := parseDate(o[dateKey])
	if err != nil {
		return nil
	}
	dl, ok := cleanURL(str(o["downloadURL"]))
	if !ok {
		return nil
	}
	size, _ := toInt64(o["size"])
	if size < 0 {
		size = 0
	}
	v := &Version{
		Version:              version,
		BuildVersion:         str(o["buildVersion"]),
		MarketingVersion:     str(o["marketingVersion"]),
		Date:                 date.Format(time.RFC3339),
		LocalizedDescription: str(o["localizedDescription"]),
		DownloadURL:          dl,
		Size:                 size,
		MinOSVersion:         str(o["minOSVersion"]),
		MaxOSVersion:         str(o["maxOSVersion"]),
	}
	if sha := strings.ToLower(str(o["sha256"])); isHex(sha) && len(sha) == 64 {
		v.SHA256 = sha
	}
	return v
}

// dedupeVersions sorts newest first and keeps the newest entry for each
// version string.
func dedupeVersions(vs []*Version) []*Version {
	sort.SliceStable(vs, func(i, j int) bool { return versionTime(vs[i]).After(versionTime(vs[j])) })
	seen := make(map[string]bool, len(vs))
	out := vs[:0]
	for _, v := range vs {
		if seen[v.Version] {
			continue
		}
		seen[v.Version] = true
		out = append(out, v)
	}
	return out
}

func versionTime(v *Version) time.Time {
	t, _ := time.Parse(time.RFC3339, v.Date)
	return t
}

// lastUpdated is the date of the newest version, or the zero time.
func (a *App) lastUpdated() time.Time {
	if len(a.Versions) == 0 {
		return time.Time{}
	}
	return versionTime(a.Versions[0])
}

func parseScreenshots(v any) *Screenshots {
	var s Screenshots
	switch t := v.(type) {
	case []any:
		s.IPhone = screenshotList(t)
	case object:
		if l, ok := t["iphone"].([]any); ok {
			s.IPhone = screenshotList(l)
		}
		if l, ok := t["ipad"].([]any); ok {
			s.IPad = screenshotList(l)
		}
	}
	if len(s.IPhone) == 0 && len(s.IPad) == 0 {
		return nil
	}
	return &s
}

func screenshotList(list []any) []Screenshot {
	var out []Screenshot
	for _, item := range list {
		var shot Screenshot
		switch t := item.(type) {
		case string:
			u, ok := cleanURL(t)
			if !ok {
				continue
			}
			shot.ImageURL = u
		case object:
			u, ok := cleanURL(str(t["imageURL"]))
			if !ok {
				continue
			}
			shot.ImageURL = u
			w, okW := toInt64(t["width"])
			h, okH := toInt64(t["height"])
			// AltStore stores dimensions as Int16. Keep them only as a valid pair.
			if okW && okH && w > 0 && h > 0 && w <= math.MaxInt16 && h <= math.MaxInt16 {
				shot.Width, shot.Height = int(w), int(h)
			}
		default:
			continue
		}
		out = append(out, shot)
	}
	return out
}

func parsePermissions(v any) *AppPermissions {
	p := &AppPermissions{Entitlements: []string{}, Privacy: map[string]string{}}
	o, ok := v.(object)
	if !ok {
		return p
	}
	seen := map[string]bool{}
	addEnt := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			p.Entitlements = append(p.Entitlements, name)
		}
	}
	switch ents := o["entitlements"].(type) {
	case []any:
		for _, e := range ents {
			switch et := e.(type) {
			case string:
				addEnt(strings.TrimSpace(et))
			case object:
				addEnt(str(et["name"]))
			}
		}
	case object:
		keys := make([]string, 0, len(ents))
		for k := range ents {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			addEnt(strings.TrimSpace(k))
		}
	}
	switch priv := o["privacy"].(type) {
	case object:
		for k, val := range priv {
			if k = strings.TrimSpace(k); k != "" {
				p.Privacy[k] = str(val)
			}
		}
	case []any:
		// Legacy layout: [{"name": "Camera", "usageDescription": "..."}].
		for _, e := range priv {
			eo, ok := e.(object)
			if !ok {
				continue
			}
			name := str(eo["name"])
			if name == "" {
				continue
			}
			if !strings.HasPrefix(name, "NS") {
				name = "NS" + name + "UsageDescription"
			}
			p.Privacy[name] = str(eo["usageDescription"])
		}
	}
	return p
}

func parsePatreon(v any) *Patreon {
	o, ok := v.(object)
	if !ok {
		return nil
	}
	p := &Patreon{
		Currency: str(o["currency"]),
		Benefit:  str(o["benefit"]),
		Hidden:   o["hidden"] == true,
	}
	switch pl := o["pledge"].(type) {
	case json.Number:
		if f, err := pl.Float64(); err == nil {
			p.Pledge = f
		}
	case string:
		if strings.EqualFold(strings.TrimSpace(pl), "custom") {
			p.Pledge = "custom"
		} else if f, err := strconv.ParseFloat(strings.TrimSpace(pl), 64); err == nil {
			p.Pledge = f
		}
	}
	if tiers, ok := o["tiers"].([]any); ok {
		for _, t := range tiers {
			if s := str(t); s != "" {
				p.Tiers = append(p.Tiers, s)
			}
		}
	}
	return p
}

// str converts strings and numbers to a trimmed string. Anything else is "".
func str(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case json.Number:
		return t.String()
	}
	return ""
}

func toInt64(v any) (int64, bool) {
	var s string
	switch t := v.(type) {
	case json.Number:
		s = t.String()
	case string:
		s = strings.TrimSpace(t)
	default:
		return 0, false
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, true
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) && math.Abs(f) < math.MaxInt64 {
		return int64(f), true
	}
	return 0, false
}

func isHex(s string) bool {
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

// tintColor returns a hex color that AltStore's UIColor(hexString:) accepts,
// or "" so the field is left out. A bad tint color fails the whole source.
func tintColor(s string) string {
	s = strings.TrimLeft(strings.TrimSpace(s), "#")
	if !isHex(s) {
		return ""
	}
	switch len(s) {
	case 3, 6:
		return "#" + strings.ToUpper(s)
	case 8:
		// ARGB. Drop alpha, since an 8-digit value can overflow AltStore's Int32 scan.
		return "#" + strings.ToUpper(s[2:])
	}
	return ""
}

// cleanURL returns an absolute http(s) URL with every character that Swift's
// URL(string:) rejects percent-encoded. ok is false when s is not usable.
func cleanURL(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	u, err := url.Parse(s)
	if err != nil {
		u, err = url.Parse(escapeLoose(s))
		if err != nil {
			return "", false
		}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	if u.Host == "" || !isASCII(u.Host) || strings.ContainsAny(u.Host, " \t") {
		return "", false
	}
	u.RawQuery = escapeLoose(u.RawQuery)
	return u.String(), true
}

// escapeLoose percent-encodes bytes outside the URL-safe ASCII set and leaves
// existing escapes and reserved characters alone.
func escapeLoose(s string) string {
	const safe = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~:/?#[]@!$&'()*+,;=%"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '%' && !(i+2 < len(s) && isHex(s[i+1:i+3])) {
			b.WriteString("%25")
			continue
		}
		if strings.IndexByte(safe, c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7E || s[i] < 0x21 {
			return false
		}
	}
	return true
}
