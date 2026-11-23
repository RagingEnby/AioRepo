package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// validCategories are the categories AltStore shows.
var validCategories = map[string]bool{
	"developer": true, "entertainment": true, "games": true, "lifestyle": true,
	"other": true, "photo-video": true, "social": true, "utilities": true,
}

// categoryOrder breaks ties between equal keyword scores.
var categoryOrder = []string{"games", "social", "photo-video", "entertainment", "developer", "utilities", "lifestyle"}

var categoryKeywords = map[string]*regexp.Regexp{
	"games":         wordsRe("game", "games", "gaming", "emulator", "emulators", "emu", "nintendo", "playstation", "psp", "gba", "gbc", "nes", "snes", "n64", "minecraft", "pokemon", "pokémon", "roblox", "retroarch", "ppsspp", "dolphin", "arcade", "chess", "puzzle", "mame", "rpg"),
	"social":        wordsRe("discord", "instagram", "twitter", "tiktok", "snapchat", "reddit", "facebook", "whatsapp", "telegram", "messenger", "chat", "social", "threads", "bluesky", "mastodon", "x\\+\\+", "tumblr", "wechat", "signal"),
	"photo-video":   wordsRe("video", "videos", "photo", "photos", "camera", "youtube", "vlc", "iptv", "movie", "movies", "netflix", "twitch", "anime", "streaming", "stream", "infuse", "kodi", "plex", "jellyfin"),
	"entertainment": wordsRe("music", "spotify", "soundcloud", "deezer", "manga", "comic", "comics", "book", "books", "novel", "podcast", "podcasts", "radio", "lyrics", "audiobook"),
	"developer":     wordsRe("terminal", "ssh", "git", "developer", "debug", "debugger", "shell", "compiler", "python", "jit", "sideload", "sideloading", "signer", "ide", "xcode", "filza", "trollstore", "jailbreak"),
	"utilities":     wordsRe("vpn", "browser", "file", "files", "manager", "utility", "utilities", "tool", "tools", "keyboard", "cleaner", "scanner", "translator", "calculator", "clipboard", "downloader", "adblock", "proxy", "torrent"),
	"lifestyle":     wordsRe("fitness", "health", "sleep", "food", "recipe", "recipes", "shopping", "travel", "weather", "dating", "workout", "meditation"),
}

func wordsRe(words ...string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(?:^|[^\pL\pN])(?:` + strings.Join(words, "|") + `)(?:$|[^\pL\pN])`)
}

// fixCategory keeps a valid category, and otherwise guesses one from the
// name and description. A category that is set but unknown falls back to
// "other"; a missing one stays empty when no keyword matches.
func fixCategory(a *App) {
	if validCategories[a.Category] {
		return
	}
	if c := inferCategory(a.Name, a.LocalizedDescription); c != "" {
		a.Category = c
	} else if a.Category != "" {
		a.Category = "other"
	}
}

func inferCategory(name, desc string) string {
	if len(desc) > 2000 {
		desc = desc[:2000]
	}
	best, bestScore := "", 0
	for _, c := range categoryOrder {
		re := categoryKeywords[c]
		score := 3*len(re.FindAllStringIndex(name, -1)) + len(re.FindAllStringIndex(desc, -1))
		if score > bestScore {
			best, bestScore = c, score
		}
	}
	return best
}

// NewsItem follows AltStore's news format. notify and tintColor are always
// written because older AltStore builds require them.
type NewsItem struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Caption    string `json:"caption"`
	Date       string `json:"date"`
	TintColor  string `json:"tintColor"`
	Notify     bool   `json:"notify"`
}

const (
	newsDays  = 7
	newsTint  = "#8A5CF5"
	newsNames = 8
)

// buildNews makes one item per day in the last week that added apps.
func buildNews(apps []*App, seen *seenState, now time.Time) []any {
	byDay := map[string][]string{}
	for _, a := range apps {
		if day := seen.Apps[a.BundleIdentifier]; day != "" {
			byDay[day] = append(byDay[day], a.Name)
		}
	}
	today := now.In(localTZ)
	var news []any
	for i := 0; i < newsDays; i++ {
		day := today.AddDate(0, 0, -i).Format(time.DateOnly)
		names := byDay[day]
		if len(names) == 0 {
			continue
		}
		sort.Strings(names)
		caption := strings.Join(names[:min(len(names), newsNames)], ", ")
		if extra := len(names) - newsNames; extra > 0 {
			caption += fmt.Sprintf(", and %d more", extra)
		}
		title := fmt.Sprintf("%d new apps", len(names))
		if len(names) == 1 {
			title = "1 new app"
		}
		d, _ := time.ParseInLocation(time.DateOnly, day, localTZ)
		news = append(news, NewsItem{
			Identifier: "new-apps-" + day,
			Title:      title,
			Caption:    caption,
			Date:       d.Format(time.RFC3339),
			TintColor:  newsTint,
		})
	}
	return news
}

// featuredFile lists bundle IDs to feature, one per line.
const featuredFile = "featured.txt"

// featured returns the IDs from featuredFile that are present in apps.
func featured(apps []*App) ([]string, error) {
	ids, err := readURLFileOptional(featuredFile)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, a := range apps {
		have[a.BundleIdentifier] = true
	}
	var out []string
	for _, id := range ids {
		if have[id] {
			out = append(out, id)
		}
	}
	return out, nil
}
