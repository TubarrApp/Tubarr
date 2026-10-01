package models

// SiteScraper holds selectors for scraping each site.
type SiteScraper struct {
	Domain    string
	Selectors []ScrapeSelectors
}

// ScrapeSelectors holds the field name and HTML selector for grabbing desired metadata.
type ScrapeSelectors struct {
	Field    string
	Selector string
	Attr     string
}
