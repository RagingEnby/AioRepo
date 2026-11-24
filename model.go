package main

// Output types. Field names and value types follow the AltStore classic
// source format (the sideload build, not AltStore PAL). AltStore rejects a
// whole source when one app fails to decode, so every value written here has
// already been checked against what the classic decoder accepts.

type Source struct {
	Name         string   `json:"name"`
	Identifier   string   `json:"identifier"`
	Subtitle     string   `json:"subtitle,omitempty"`
	Description  string   `json:"description,omitempty"`
	IconURL      string   `json:"iconURL,omitempty"`
	Website      string   `json:"website,omitempty"`
	PatreonURL   string   `json:"patreonURL,omitempty"`
	FeaturedApps []string `json:"featuredApps,omitempty"`
	Apps         []*App   `json:"apps"`
	News         []any    `json:"news"`
}

type App struct {
	Name                 string          `json:"name"`
	BundleIdentifier     string          `json:"bundleIdentifier"`
	DeveloperName        string          `json:"developerName"`
	Subtitle             string          `json:"subtitle,omitempty"`
	LocalizedDescription string          `json:"localizedDescription"`
	IconURL              string          `json:"iconURL"`
	TintColor            string          `json:"tintColor,omitempty"`
	Category             string          `json:"category,omitempty"`
	Screenshots          *Screenshots    `json:"screenshots,omitempty"`
	Versions             []*Version      `json:"versions"`
	AppPermissions       *AppPermissions `json:"appPermissions"`
	Patreon              *Patreon        `json:"patreon,omitempty"`
	Beta                 bool            `json:"beta,omitempty"`

	// Legacy fields that mirror the latest version, for older AltStore and
	// SideStore builds that predate the versions array.
	Version            string `json:"version"`
	VersionDate        string `json:"versionDate"`
	VersionDescription string `json:"versionDescription,omitempty"`
	DownloadURL        string `json:"downloadURL"`
	Size               int64  `json:"size"`

	// Not written. Used for filtering and deduplication.
	isPAL      bool
	sourceName string
	tier       int
	undated    bool // dates come from first sighting, not the source
}

type Version struct {
	Version              string `json:"version"`
	BuildVersion         string `json:"buildVersion,omitempty"`
	MarketingVersion     string `json:"marketingVersion,omitempty"`
	Date                 string `json:"date"`
	LocalizedDescription string `json:"localizedDescription,omitempty"`
	DownloadURL          string `json:"downloadURL"`
	Size                 int64  `json:"size"`
	SHA256               string `json:"sha256,omitempty"`
	MinOSVersion         string `json:"minOSVersion,omitempty"`
	MaxOSVersion         string `json:"maxOSVersion,omitempty"`
}

type Screenshot struct {
	ImageURL string `json:"imageURL"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
}

type Screenshots struct {
	IPhone []Screenshot `json:"iphone,omitempty"`
	IPad   []Screenshot `json:"ipad,omitempty"`
}

type AppPermissions struct {
	Entitlements []string          `json:"entitlements"`
	Privacy      map[string]string `json:"privacy"`
}

type Patreon struct {
	Pledge   any      `json:"pledge,omitempty"` // number or "custom"
	Currency string   `json:"currency,omitempty"`
	Benefit  string   `json:"benefit,omitempty"`
	Tiers    []string `json:"tiers,omitempty"`
	Hidden   bool     `json:"hidden,omitempty"`
}
