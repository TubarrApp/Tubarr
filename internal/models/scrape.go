package models

import "tubarr/internal/domain/consts"

// SiteScraper holds selectors for scraping each site.
type SiteScraper struct {
	Domain      string
	Selectors   []ScrapeSelectors
	Crawl       *consts.HTMLCrawlRule
	Impersonate consts.Impersonate
}

// ScrapeSelectors holds the field name and HTML selector for grabbing desired metadata.
type ScrapeSelectors struct {
	Field    string
	Selector string
	Attr     string
}
