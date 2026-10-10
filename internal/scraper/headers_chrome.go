package scraper

import (
	"fmt"
	"strings"

	fhttp "github.com/bogdanfinn/fhttp"
)

// chromeAccept is the accept header Chrome sends when loading a page.
const chromeAccept = "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"

// chromeHeaderOrder is the order Chrome sends its headers in when loading a page.
var chromeHeaderOrder = []string{
	"host", "sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform", "upgrade-insecure-requests", "user-agent", "accept",
	"sec-fetch-site", "sec-fetch-mode", "sec-fetch-user", "sec-fetch-dest", "accept-encoding", "accept-language",
	"cookie", "priority",
}

// setChromeHeaders adds the headers Chrome sends when loading a page, matching the request's user agent, and puts them in
// Chrome's order. Headers already set (e.g. the user agent and cookies) are kept, except Colly's default "Accept: */*".
func setChromeHeaders(h fhttp.Header) {
	userAgent := h.Get("User-Agent")
	if h.Get("Accept") == "*/*" {
		h.Del("Accept")
	}

	mobile := "?0"
	if strings.Contains(userAgent, "Android") {
		mobile = "?1"
	}
	defaults := [][2]string{
		{"sec-ch-ua-mobile", mobile},
		{"sec-ch-ua-platform", chromePlatform(userAgent)},
		{"upgrade-insecure-requests", "1"},
		{"accept", chromeAccept},
		{"sec-fetch-site", "none"},
		{"sec-fetch-mode", "navigate"},
		{"sec-fetch-user", "?1"},
		{"sec-fetch-dest", "document"},
		{"accept-encoding", "gzip, deflate, br, zstd"},
		{"accept-language", "en-US,en;q=0.9"},
		{"priority", "u=0, i"},
	}
	if major := userAgentMajorVersion(userAgent); major > 0 {
		defaults = append(defaults, [2]string{"sec-ch-ua", chromeBrands(major)})
	}
	for _, kv := range defaults {
		if h.Get(kv[0]) == "" {
			h.Set(kv[0], kv[1])
		}
	}
	h[fhttp.HeaderOrderKey] = chromeHeaderOrder
}

// chromeBrands returns the sec-ch-ua header Google Chrome sends for a major version.
//
// Follows Chromium's brand list: a "GREASE" brand, Chromium, and Google Chrome, with the GREASE brand's characters and
// version, and the order of all three, picked from the major version.
func chromeBrands(major int) string {
	greaseChars := []string{" ", "(", ":", "-", ".", "/", ")", ";", "=", "?", "_"}
	greaseVersions := []string{"8", "99", "24"}
	orders := [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}

	brands := []string{
		fmt.Sprintf(`"Not%sA%sBrand";v="%s"`, greaseChars[major%len(greaseChars)], greaseChars[(major+1)%len(greaseChars)], greaseVersions[major%len(greaseVersions)]),
		fmt.Sprintf(`"Chromium";v="%d"`, major),
		fmt.Sprintf(`"Google Chrome";v="%d"`, major),
	}
	ordered := make([]string, len(brands))
	for i, pos := range orders[major%len(orders)] {
		ordered[pos] = brands[i]
	}
	return strings.Join(ordered, ", ")
}

// chromePlatform returns the sec-ch-ua-platform header for a user agent's operating system.
func chromePlatform(userAgent string) string {
	switch {
	case strings.Contains(userAgent, "Windows"):
		return `"Windows"`
	case strings.Contains(userAgent, "Android"):
		return `"Android"`
	case strings.Contains(userAgent, "CrOS"):
		return `"Chrome OS"`
	case strings.Contains(userAgent, "Macintosh"), strings.Contains(userAgent, "Mac OS X"):
		return `"macOS"`
	case strings.Contains(userAgent, "Linux"):
		return `"Linux"`
	default:
		return `"Windows"`
	}
}
