package consts

import (
	"regexp"

	"github.com/TubarrApp/gocommon/sharedtags"
)

// HTMLMetadataRule defines the metadata scraping elements.
type HTMLMetadataRule struct {
	Name     string
	Selector string
	Attr     string
}

// HTMLCrawlRule defines how video URLs are extracted from a channel page.
type HTMLCrawlRule struct {
	Selector   string
	Attr       string         // Attribute to read ("" reads element text).
	JSONPath   string         // gjson path applied to the value, if it holds JSON.
	Include    *regexp.Regexp // URLs must match, if set.
	Exclude    *regexp.Regexp // URLs must not match, if set.
	StripQuery bool
}

// HTMLMetadataQuery holds the site name and metadata rules.
type HTMLMetadataQuery struct {
	Site         string
	Rules        []HTMLMetadataRule
	Crawl        *HTMLCrawlRule // Channel page crawl rule (nil to use built-in or yt-dlp).
	Impersonate  string         // Browser TLS fingerprint to impersonate ("" for none).
	FlareSolverr bool           // Fetch pages through FlareSolverr (takes priority over Impersonate).
}

// HTMLBitchute holds scraping elements for bitchute.com.
var HTMLBitchute = HTMLMetadataQuery{
	Site:  "bitchute.com",
	Crawl: &HTMLCrawlRule{Selector: "a[href]", Attr: "href", Include: regexp.MustCompile(`/video/`)},
	Rules: []HTMLMetadataRule{
		{Name: sharedtags.JTitle, Selector: `meta[itemprop="name"]`, Attr: "content"},
		{Name: sharedtags.JDescription, Selector: `meta[name="description"]`, Attr: "content"},
		{Name: sharedtags.JDescription, Selector: `meta[property="og:description"]`, Attr: "content"},
		{Name: sharedtags.JReleaseDate, Selector: "span[data-v-3c3cf957]", Attr: "data-v-3c3cf957"},
	},
}

// HTMLCensored holds scraping elements for censored.tv.
var HTMLCensored = HTMLMetadataQuery{
	Site:  "censored.tv",
	Crawl: &HTMLCrawlRule{Selector: "a[href]", Attr: "href", Include: regexp.MustCompile(`/episodes/`)},
	Rules: []HTMLMetadataRule{
		{Name: sharedtags.JTitle, Selector: "#episode-container .episode-title"},
		{Name: sharedtags.JDescription, Selector: "#about .raised-content"},
		{Name: sharedtags.JReleaseDate, Selector: "#about time"},
		{Name: sharedtags.JDirectVideoURL, Selector: "source[src*='master.m3u8']", Attr: "src"},
		{Name: sharedtags.JThumbnailURL, Selector: "video-js[poster]", Attr: "poster"},
	},
}

// HTMLOdysee holds scraping elements for odysee.com.
var HTMLOdysee = HTMLMetadataQuery{
	Site:  "odysee.com",
	Crawl: &HTMLCrawlRule{Selector: "a[href]", Attr: "href", Include: regexp.MustCompile(`@`)},
	Rules: []HTMLMetadataRule{
		{Name: sharedtags.JTitle, Selector: "title"},
		{Name: sharedtags.JDescription, Selector: `meta[name="description"]`, Attr: "content"},
		{Name: sharedtags.JDescription, Selector: `meta[property="og:description"]`, Attr: "content"},
		{Name: sharedtags.JReleaseDate, Selector: `meta[property="og:video:release_date"]`, Attr: "content"},
	},
}

// HTMLRumble holds scraping elements for rumble.com.
var HTMLRumble = HTMLMetadataQuery{
	Site:        "rumble.com",
	Impersonate: "chrome", // Rumble blocks non-Chrome TLS fingerprints.
	Crawl: &HTMLCrawlRule{
		Selector:   `rum-videos-grid script[type="application/json"]`,
		JSONPath:   `items.#(object_type=="video")#.url`,
		Include:    regexp.MustCompile(`^https?://([^/]+\.)?rumble\.com/v`),
		Exclude:    regexp.MustCompile(`^https?://([^/]+\.)?rumble\.com/videos`),
		StripQuery: true,
	},
	Rules: []HTMLMetadataRule{
		{Name: sharedtags.JTitle, Selector: "title"},
		{Name: sharedtags.JDescription, Selector: `meta[name="description"]`, Attr: "content"},
		{Name: sharedtags.JDescription, Selector: `meta[property="og:description"]`, Attr: "content"},
		{Name: sharedtags.JReleaseDate, Selector: "time", Attr: "datetime"},
		{Name: sharedtags.JThumbnailURL, Selector: `meta[property="og:image"]`, Attr: "content"},
	},
}
