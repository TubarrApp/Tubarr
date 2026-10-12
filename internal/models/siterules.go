package models

import (
	"regexp"
	"time"
)

// SiteRules holds a site's registered rules: metadata selectors, a channel page crawl rule, and request settings.
type SiteRules struct {
	Site                string
	Metadata            []MetadataRule
	Crawl               *CrawlRule    // Channel page crawl rule (nil to use yt-dlp).
	Impersonate         string        // Browser TLS fingerprint to impersonate ("" for none).
	UserAgent           string        // User agent to send ("" to match Impersonate, or the HTTP client's default).
	FlareSolverr        bool          // Get past Cloudflare with FlareSolverr's cookies and user agent (takes priority over Impersonate).
	FlareSolverrTimeout time.Duration // Time limit for FlareSolverr to solve the site's challenge (0 for the default).
}

// MetadataRule defines how a metadata field is scraped from a video page.
type MetadataRule struct {
	Name     string
	Selector string
	Attr     string         // Attribute to read ("" reads element text).
	JSONPath string         // gjson path applied to the value, if set.
	Regex    *regexp.Regexp // Applied to the value, keeping capture group 1 (or the whole match), if set.
	Fetch    []FetchStep    // Follow-up requests made with the value, in order.
}

// FetchStep defines a follow-up request built from a scraped value, and how the next value is read from its response.
type FetchStep struct {
	URL      string         // URL template, with {value} replaced by the current value.
	Selector string         // CSS selector applied to an HTML response, if set.
	Attr     string         // Attribute of the selected element to read ("" reads element text).
	JSONPath string         // gjson path applied to the value, if set.
	Regex    *regexp.Regexp // Applied to the value, keeping capture group 1 (or the whole match), if set.
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
