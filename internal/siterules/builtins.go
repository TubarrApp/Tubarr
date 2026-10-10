package siterules

import (
	"regexp"
	"tubarr/internal/dev"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedtags"
)

// builtinSites returns the compile-time default site rules, gated by dev toggles.
func builtinSites() map[string]models.SiteRules {
	builtins := []struct {
		rules  models.SiteRules
		toggle dev.BuiltinScraper
	}{
		{Censored, dev.CensoredTV},
		{Bitchute, dev.BitchuteCom},
		{Odysee, dev.OdyseeCom},
		{Rumble, dev.RumbleCom},
	}

	sites := make(map[string]models.SiteRules, len(builtins))
	for _, b := range builtins {
		r := b.rules
		if !b.toggle.Metadata {
			r.Metadata = nil
		}
		if !b.toggle.Crawl {
			r.Crawl = nil
		}
		if len(r.Metadata) > 0 || r.Crawl != nil {
			sites[r.Site] = r
		}
	}
	return sites
}

// Bitchute holds the built-in rules for bitchute.com.
var Bitchute = models.SiteRules{
	Site:  "bitchute.com",
	Crawl: &models.CrawlRule{Selector: "a[href]", Attr: "href", Include: regexp.MustCompile(`/video/`)},
	Metadata: []models.MetadataRule{
		{Name: sharedtags.JTitle, Selector: `meta[itemprop="name"]`, Attr: "content"},
		{Name: sharedtags.JDescription, Selector: `meta[name="description"]`, Attr: "content"},
		{Name: sharedtags.JDescription, Selector: `meta[property="og:description"]`, Attr: "content"},
		{Name: sharedtags.JReleaseDate, Selector: "span[data-v-3c3cf957]", Attr: "data-v-3c3cf957"},
	},
}

// Censored holds the built-in rules for censored.tv.
var Censored = models.SiteRules{
	Site:  "censored.tv",
	Crawl: &models.CrawlRule{Selector: "a[href]", Attr: "href", Include: regexp.MustCompile(`/episodes/`)},
	Metadata: []models.MetadataRule{
		{Name: sharedtags.JTitle, Selector: "#episode-container .episode-title"},
		{Name: sharedtags.JDescription, Selector: "#about .raised-content"},
		{Name: sharedtags.JReleaseDate, Selector: "#about time"},
		{Name: sharedtags.JDirectVideoURL, Selector: "source[src*='master.m3u8']", Attr: "src"},
		{Name: sharedtags.JThumbnailURL, Selector: "video-js[poster]", Attr: "poster"},
	},
}

// Odysee holds the built-in rules for odysee.com.
var Odysee = models.SiteRules{
	Site:  "odysee.com",
	Crawl: &models.CrawlRule{Selector: "a[href]", Attr: "href", Include: regexp.MustCompile(`@`)},
	Metadata: []models.MetadataRule{
		{Name: sharedtags.JTitle, Selector: "title"},
		{Name: sharedtags.JDescription, Selector: `meta[name="description"]`, Attr: "content"},
		{Name: sharedtags.JDescription, Selector: `meta[property="og:description"]`, Attr: "content"},
		{Name: sharedtags.JReleaseDate, Selector: `meta[property="og:video:release_date"]`, Attr: "content"},
	},
}

// Rumble holds the built-in rules for rumble.com.
var Rumble = models.SiteRules{
	Site:        "rumble.com",
	Impersonate: "chrome", // Rumble blocks non-Chrome TLS fingerprints.
	Crawl: &models.CrawlRule{
		Selector:   `rum-videos-grid script[type="application/json"]`,
		JSONPath:   `items.#(object_type=="video")#.url`,
		Include:    regexp.MustCompile(`^https?://([^/]+\.)?rumble\.com/v`),
		Exclude:    regexp.MustCompile(`^https?://([^/]+\.)?rumble\.com/videos`),
		StripQuery: true,
	},
	Metadata: []models.MetadataRule{
		{Name: sharedtags.JTitle, Selector: "title"},
		{Name: sharedtags.JDescription, Selector: `meta[name="description"]`, Attr: "content"},
		{Name: sharedtags.JDescription, Selector: `meta[property="og:description"]`, Attr: "content"},
		{Name: sharedtags.JReleaseDate, Selector: "time", Attr: "datetime"},
		{Name: sharedtags.JThumbnailURL, Selector: `meta[property="og:image"]`, Attr: "content"},
	},
}
