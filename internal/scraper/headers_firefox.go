package scraper

import fhttp "github.com/bogdanfinn/fhttp"

// firefoxHeaderOrder is the order Firefox sends its headers in when loading a page.
var firefoxHeaderOrder = []string{
	"host", "user-agent", "accept", "accept-language", "accept-encoding", "cookie", "upgrade-insecure-requests",
	"sec-fetch-dest", "sec-fetch-mode", "sec-fetch-site", "sec-fetch-user", "priority", "te",
}

// setFirefoxHeaders adds the headers Firefox sends when loading a page, and puts them in Firefox's order. Headers
// already set (e.g. the user agent and cookies) are kept, except Colly's default "Accept: */*".
func setFirefoxHeaders(h fhttp.Header) {
	if h.Get("Accept") == "*/*" {
		h.Del("Accept")
	}

	defaults := [][2]string{
		{"accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"},
		{"accept-language", "en-US,en;q=0.9"},
		{"accept-encoding", "gzip, deflate, br, zstd"},
		{"upgrade-insecure-requests", "1"},
		{"sec-fetch-dest", "document"},
		{"sec-fetch-mode", "navigate"},
		{"sec-fetch-site", "none"},
		{"sec-fetch-user", "?1"},
		{"priority", "u=0, i"},
		{"te", "trailers"},
	}
	for _, kv := range defaults {
		if h.Get(kv[0]) == "" {
			h.Set(kv[0], kv[1])
		}
	}
	h[fhttp.HeaderOrderKey] = firefoxHeaderOrder
}
