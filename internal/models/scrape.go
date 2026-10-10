package models

import (
	"time"
	"tubarr/internal/domain/consts"
)

// SiteScraper holds selectors for scraping each site.
type SiteScraper struct {
	Domain              string
	Selectors           []ScrapeSelectors
	Crawl               *CrawlRule
	Impersonate         *consts.Impersonate // nil keeps any built-in value.
	FlareSolverr        bool
	FlareSolverrTimeout time.Duration // 0 keeps any built-in value.
}

// ScrapeSelectors holds the field name and HTML selector for grabbing desired metadata.
type ScrapeSelectors struct {
	Field    string
	Selector string
	Attr     string
}
