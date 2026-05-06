package scraper

import (
	"net/url"
	"strings"
	"tubarr/internal/domain/logger"

	"golang.org/x/net/publicsuffix"
)

type urlPattern struct {
	name    string
	pattern string
}

type ytDlpOutput struct {
	Entries []struct {
		URL string `json:"url"`
	} `json:"entries"`
}

const (
	bitchute        = "bitchute.com"
	bitchutePattern = "/video/"
	censored        = "censored.tv"
	censoredPattern = "/episodes/"
	odysee          = "odysee.com"
	odyseePattern   = "@"
	rumble          = "rumble.com"
	rumblePattern   = "/v"
	defaultDom      = "default"
	defaultPattern  = "/watch"
)

var patterns = map[string]urlPattern{
	bitchute:   {name: bitchute, pattern: bitchutePattern},
	censored:   {name: censored, pattern: censoredPattern},
	odysee:     {name: odysee, pattern: odyseePattern},
	rumble:     {name: rumble, pattern: rumblePattern},
	defaultDom: {name: defaultDom, pattern: defaultPattern},
}

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
