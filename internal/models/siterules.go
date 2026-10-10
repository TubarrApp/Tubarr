package models

import "regexp"

// SiteRules holds a site's registered rules: metadata selectors, a channel page crawl rule, and request settings.
type SiteRules struct {
	Site         string
	Metadata     []MetadataRule
	Crawl        *CrawlRule // Channel page crawl rule (nil to use yt-dlp).
	Impersonate  string     // Browser TLS fingerprint to impersonate ("" for none).
	FlareSolverr bool       // Get past Cloudflare with FlareSolverr's cookies and user agent (takes priority over Impersonate).
}

// MetadataRule defines how a metadata field is scraped from a video page.
type MetadataRule struct {
	Name     string
	Selector string
	Attr     string
}

// CrawlRule defines how video URLs are extracted from a channel page.
type CrawlRule struct {
	Selector   string
	Attr       string         // Attribute to read ("" reads element text).
	JSONPath   string         // gjson path applied to the value, if it holds JSON.
	Include    *regexp.Regexp // URLs must match, if set.
	Exclude    *regexp.Regexp // URLs must not match, if set.
	StripQuery bool
}
