// Package dev allows for programmer level toggles for program tasks (e.g. toggle whether to use a custom scraper for a site)
package dev

// BuiltinScraper toggles a site's built-in metadata scraping rules and channel crawler (off uses yt-dlp).
type BuiltinScraper struct {
	Metadata bool
	Crawl    bool
}

// Built-in scraper toggles per site.
var (
	CensoredTV  = BuiltinScraper{Metadata: true, Crawl: true}
	BitchuteCom = BuiltinScraper{Metadata: false, Crawl: false}
	OdyseeCom   = BuiltinScraper{Metadata: false, Crawl: false}
	RumbleCom   = BuiltinScraper{Metadata: false, Crawl: true}
)
