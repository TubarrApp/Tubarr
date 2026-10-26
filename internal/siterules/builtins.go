package siterules

import (
	"regexp"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedtags"
)

// builtinScraper toggles a site's built-in metadata scraping rules and channel crawler (off uses yt-dlp).
type builtinScraper struct {
	metadata bool
	crawl    bool
}

// Built-in scraper toggles per site.
var (
	censoredTV  = builtinScraper{metadata: true, crawl: true}
	bitchuteCom = builtinScraper{metadata: false, crawl: false}
	odyseeCom   = builtinScraper{metadata: false, crawl: false}
	rumbleCom   = builtinScraper{metadata: false, crawl: true}
)

// builtinSites returns the compile-time default site rules, gated by the toggles above.
func builtinSites() map[string]models.SiteRules {
	builtins := []struct {
		rules  models.SiteRules
		toggle builtinScraper
	}{
		{censored, censoredTV},
		{bitchute, bitchuteCom},
		{odysee, odyseeCom},
		{rumble, rumbleCom},
	}

	sites := make(map[string]models.SiteRules, len(builtins))
	for _, b := range builtins {
		r := b.rules
		if !b.toggle.metadata {
			r.Metadata = nil
		}
		if !b.toggle.crawl {
			r.Crawl = nil
		}
		if len(r.Metadata) > 0 || r.Crawl != nil {
			sites[r.Site] = r
		}
	}
	return sites
}

// bitchute holds the built-in rules for bitchute.com.
var bitchute = models.SiteRules{
	Site:  "bitchute.com",
	Crawl: &models.CrawlRule{Selector: "a[href]", Attr: "href", Include: regexp.MustCompile(`/video/`)},
	Metadata: []models.MetadataRule{
		{Name: sharedtags.JTitle, Selector: `meta[itemprop="name"]`, Attr: "content"},
		{Name: sharedtags.JDescription, Selector: `meta[name="description"]`, Attr: "content"},
		{Name: sharedtags.JDescription, Selector: `meta[property="og:description"]`, Attr: "content"},
		{Name: sharedtags.JReleaseDate, Selector: "span[data-v-3c3cf957]", Attr: "data-v-3c3cf957"},
	},
}

// censored holds the built-in rules for censored.tv.
var censored = models.SiteRules{
	Site: "censored.tv",
	Crawl: &models.CrawlRule{
		Selector: "a[href]",
		Attr:     "href",
		Include:  regexp.MustCompile(`/episodes/`),
		Item:     ".episode",
		PreviewFields: []models.MetadataRule{
			{Name: sharedtags.JTitle, Selector: "h5"},
			{Name: sharedtags.JThumbnail, Selector: "img[data-src]", Attr: "data-src"},
			{Name: sharedtags.JThumbnail, Selector: "img[src]", Attr: "src"},
			{Name: sharedtags.JDurationString, Selector: ".duration"},
			{Name: sharedtags.JUploadDate, Selector: "time"},
		},
	},
	Metadata: []models.MetadataRule{
		{Name: sharedtags.JTitle, Selector: "#episode-container .episode-title"},
		{Name: sharedtags.JDescription, Selector: "#about .raised-content"},
		{Name: sharedtags.JReleaseDate, Selector: "#about time"},
		{Name: sharedtags.JDirectVideoURL, Selector: "source[src*='master.m3u8']", Attr: "src"},
		{Name: sharedtags.JDurationString, Selector: ".duration"},
		{Name: sharedtags.JThumbnail, Selector: "video-js[poster]", Attr: "poster"},
	},
}

// odysee holds the built-in rules for odysee.com.
var odysee = models.SiteRules{
	Site:  "odysee.com",
	Crawl: &models.CrawlRule{Selector: "a[href]", Attr: "href", Include: regexp.MustCompile(`@`)},
	Metadata: []models.MetadataRule{
		{Name: sharedtags.JTitle, Selector: "title"},
		{Name: sharedtags.JDescription, Selector: `meta[name="description"]`, Attr: "content"},
		{Name: sharedtags.JDescription, Selector: `meta[property="og:description"]`, Attr: "content"},
		{Name: sharedtags.JReleaseDate, Selector: `meta[property="og:video:release_date"]`, Attr: "content"},
	},
}

// rumble holds the built-in rules for rumble.com.
var rumble = models.SiteRules{
	Site:        "rumble.com",
	Impersonate: "chrome", // Rumble blocks non-Chrome TLS fingerprints.
	Crawl: &models.CrawlRule{
		Selector:   `rum-videos-grid script[type="application/json"]`,
		ItemPath:   `items.#(object_type=="video")#`,
		JSONPath:   "url",
		Include:    regexp.MustCompile(`^https?://([^/]+\.)?rumble\.com/v`),
		Exclude:    regexp.MustCompile(`^https?://([^/]+\.)?rumble\.com/videos`),
		StripQuery: true,
		PreviewFields: []models.MetadataRule{
			{Name: sharedtags.JTitle, JSONPath: "title"},
			{Name: sharedtags.JThumbnail, JSONPath: "thumb"},
			{Name: sharedtags.JDuration, JSONPath: "duration"},
			{Name: sharedtags.JUploadDate, JSONPath: "upload_date"},
		},
	},
	Metadata: []models.MetadataRule{
		{Name: sharedtags.JTitle, Selector: "title"},
		{Name: sharedtags.JDescription, Selector: `meta[name="description"]`, Attr: "content"},
		{Name: sharedtags.JDescription, Selector: `meta[property="og:description"]`, Attr: "content"},
		{Name: sharedtags.JReleaseDate, Selector: "time", Attr: "datetime"},
		{Name: sharedtags.JThumbnail, Selector: `meta[property="og:image"]`, Attr: "content"},
		{Name: sharedtags.JDuration, Selector: `script[type="application/ld+json"]`, JSONPath: `#(@type=="VideoObject").duration`},
	},
}
