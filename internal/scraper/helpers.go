package scraper

import (
	"net/url"
	"strings"
	"tubarr/internal/domain/logger"

	"golang.org/x/net/publicsuffix"
)

// ytDlpEntry is a yt-dlp playlist entry, which may itself be a playlist (e.g. a channel's "Videos" tab).
type ytDlpEntry struct {
	URL     string        `json:"url"`
	Entries []*ytDlpEntry `json:"entries"`
}

// videoEntries returns the entry's leaf entries, descending into nested playlists.
func (e *ytDlpEntry) videoEntries() []*ytDlpEntry {
	if len(e.Entries) == 0 {
		return []*ytDlpEntry{e}
	}
	var out []*ytDlpEntry
	for _, sub := range e.Entries {
		if sub != nil {
			out = append(out, sub.videoEntries()...)
		}
	}
	return out
}

const rumble = "rumble.com"

// getBaseDomain returns the base domain for an inputted URL.
func getBaseDomain(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return publicsuffix.EffectiveTLDPlusOne(u.Hostname())
}

// normalizeURL standardizes URLs for comparison by removing protocol and any trailing slashes.
func normalizeURL(inputURL string) string {
	cleanURL := strings.TrimPrefix(inputURL, "https://")
	cleanURL = strings.TrimPrefix(cleanURL, "http://")
	return strings.TrimSuffix(cleanURL, "/")
}

// isValidRumbleVideoURL returns true only for direct video URLs of the
// form rumble.com/v<slug>.html, excluding channel, user, and listing pages.
func isValidRumbleVideoURL(videoURL string) bool {
	parsed, err := url.Parse(videoURL)
	if err != nil {
		return false
	}
	// Valid video paths look like /v79e41g-some-title.html
	// Reject channel pages (/c/...), user pages (/user/...), listing pages (/videos/...)
	path := parsed.Path
	return strings.HasPrefix(path, "/v") &&
		!strings.HasPrefix(path, "/videos") &&
		!strings.HasPrefix(path, "/user/")
}

// removeQueryParams removes query parameters from a URL for cleaner comparison.
func removeQueryParams(inputURL string) string {
	u, err := url.Parse(inputURL)
	if err != nil {
		logger.Pl.E("Failed to parse URL %q for query parameter removal: %v", inputURL, err)
		return inputURL // Return original if parsing fails
	}
	u.RawQuery = "" // Clear query parameters
	return u.String()
}
