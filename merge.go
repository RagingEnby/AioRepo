package main

import (
	"strings"
)

// maxVersions caps the versions kept per app to keep the output small.
const maxVersions = 10

// mergeApps picks one app per bundle ID across all usable sources.
//
// Sources are visited in result order (tier, then URL), and a later copy
// replaces the current pick when:
//   - its source has a lower tier, or
//   - its tier is the same and it has real dates where the pick has only
//     first-seen dates (Scarlet), or
//   - its tier is the same and its newest version is newer, or
//   - its tier is higher, its newest version is newer, and the developer
//     name matches, so a low-trust source can update an app but not take it
//     over.
//
// The pick then gains the versions of every other copy by the same
// developer, so users can roll back to builds that only one mirror still
// lists. Apps matching block are dropped.
func mergeApps(results []*sourceResult, block ruleSet) []*App {
	type group struct {
		pick   *App
		copies []*App
	}
	groups := map[string]*group{}
	var order []string
	for _, r := range results {
		if !r.usable() || r.Status == statusDuplicate {
			continue
		}
		for _, app := range r.Parsed.Apps {
			if len(app.Versions) == 0 || app.isPAL {
				continue
			}
			app.sourceName = r.Parsed.Name
			app.tier = r.Tier
			if block.match(app) {
				continue
			}
			g, ok := groups[app.BundleIdentifier]
			if !ok {
				groups[app.BundleIdentifier] = &group{pick: app, copies: []*App{app}}
				order = append(order, app.BundleIdentifier)
				continue
			}
			g.copies = append(g.copies, app)
			if replaces(app, g.pick) {
				g.pick = app
			}
		}
	}

	out := make([]*App, 0, len(order))
	for _, id := range order {
		g := groups[id]
		app := g.pick
		versions := append([]*Version{}, app.Versions...)
		for _, c := range g.copies {
			if c != app && sameDeveloper(c, app) {
				versions = append(versions, c.Versions...)
			}
		}
		app.Versions = dedupeVersions(versions)
		if len(app.Versions) > maxVersions {
			app.Versions = app.Versions[:maxVersions]
		}
		app.syncLegacy()
		out = append(out, app)
	}
	return out
}

func replaces(c, cur *App) bool {
	switch {
	case c.tier < cur.tier:
		return true
	case c.tier == cur.tier && c.undated != cur.undated:
		// A real date beats a first-seen date at the same tier.
		return !c.undated
	case c.tier == cur.tier:
		return c.lastUpdated().After(cur.lastUpdated())
	default:
		return c.lastUpdated().After(cur.lastUpdated()) && sameDeveloper(c, cur)
	}
}

func sameDeveloper(a, b *App) bool {
	da, db := strings.TrimSpace(a.DeveloperName), strings.TrimSpace(b.DeveloperName)
	return da != "" && !strings.EqualFold(da, "unknown") && strings.EqualFold(da, db)
}

// syncLegacy copies the newest version into the legacy app-level fields.
func (a *App) syncLegacy() {
	if len(a.Versions) == 0 {
		return
	}
	latest := a.Versions[0]
	a.Version = latest.Version
	a.VersionDate = latest.Date
	a.VersionDescription = latest.LocalizedDescription
	a.DownloadURL = latest.DownloadURL
	a.Size = latest.Size
}

// attribute appends the source name to the description, so users can see
// where an app came from before they install it.
func (a *App) attribute() {
	if a.sourceName == "" {
		return
	}
	a.LocalizedDescription = strings.TrimRight(a.LocalizedDescription, " \n") + "\n\nSource: " + a.sourceName
}
