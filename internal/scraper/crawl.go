package scraper

import (
	"cmp"
	"fmt"
	"html"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/siterules"

	"github.com/PuerkitoBio/goquery"
	"github.com/TubarrApp/gocommon/sharedtags"
	"github.com/gocolly/colly"
	"github.com/tidwall/gjson"
)

// CrawledVideo is a video URL found by a crawl, with any preview details read from the channel page.
type CrawledVideo struct {
	URL          string `json:"url"`
	Title        string `json:"title,omitempty"`
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
	UploadDate   string `json:"upload_date,omitempty"` // YYYY-MM-DD.
	Duration     int    `json:"duration,omitempty"`    // Seconds.
}

// TestCrawlSite runs the crawler Tubarr would use for a channel URL and returns the videos found, sorted by URL.
//
// Used by the web UI to preview crawl rules. matched is false if the URL would be crawled with yt-dlp.
func TestCrawlSite(channelURL string) (matched bool, source string, videos []CrawledVideo, err error) {
	source, found, matched, err := New().crawlChannelURLs(channelURL, nil)
	if !matched || err != nil {
		return matched, source, nil, err
	}

	videos = make([]CrawledVideo, 0, len(found))
	for u, lv := range found {
		v := CrawledVideo{URL: u, Title: lv.Title, ThumbnailURL: lv.ThumbnailURL, Duration: lv.Duration}
		if !lv.UploadDate.IsZero() {
			v.UploadDate = lv.UploadDate.Format("2006-01-02")
		}
		videos = append(videos, v)
	}
	slices.SortFunc(videos, func(a, b CrawledVideo) int { return strings.Compare(a.URL, b.URL) })
	return true, source, videos, nil
}

// crawlChannelURLs extracts video URLs from a channel page using a registered (user-defined or built-in) crawl rule.
//
// crawled is false if no crawl rule applies to the URL, and yt-dlp should be used instead.
func (s *Scraper) crawlChannelURLs(channelURL string, cookies []*http.Cookie) (source string, urls map[string]*listedVideo, crawled bool, err error) {
	query, ok := siterules.MatchCrawl(channelURL)
	if !ok {
		return "", nil, false, nil
	}
	logger.Pl.I("Using crawl rule for %q", query.Site)
	urls, err = s.crawlWithRule(channelURL, cookies, query)
	return "crawl rule for " + query.Site, urls, true, err
}

// crawlWithRule extracts video URLs, and any preview fields, from a channel page using a site's crawl rule.
func (s *Scraper) crawlWithRule(channelURL string, cookies []*http.Cookie, query models.SiteRules) (map[string]*listedVideo, error) {
	rule := query.Crawl
	if rule == nil {
		return nil, fmt.Errorf("site %q has no crawl rule", query.Site)
	}
	var urls map[string]*listedVideo

	err := s.visitPage(channelURL, cookies, query, func(c *colly.Collector) {
		urls = make(map[string]*listedVideo) // Reset per attempt, so a challenged attempt can't leave URLs behind.
		c.OnHTML(rule.Selector, func(e *colly.HTMLElement) {
			value := e.Text
			if rule.Attr != "" {
				value = e.Attr(rule.Attr)
			}
			for _, cv := range crawlRuleVideos(e, value, rule) {
				link := e.Request.AbsoluteURL(cv.URL)
				if link == "" {
					continue
				}
				if rule.StripQuery {
					link = removeQueryParams(link)
				}
				if (rule.Include != nil && !rule.Include.MatchString(link)) || (rule.Exclude != nil && rule.Exclude.MatchString(link)) {
					logger.Pl.D(3, "Crawl rule for %q filtered out URL %q", query.Site, link)
					continue
				}
				mergeListed(urls, link, cv.listed)
			}
		})
	})
	if err != nil {
		return nil, err
	}

	logger.Pl.I("Extracted %d video URLs from %q using crawl rule for %q", len(urls), channelURL, query.Site)
	return urls, nil
}

// ruleVideo is a video URL read by a crawl rule, with its preview details.
type ruleVideo struct {
	URL    string
	listed *listedVideo
}

// crawlRuleVideos reads the video URLs and preview fields from an element matched by a crawl rule.
func crawlRuleVideos(e *colly.HTMLElement, value string, rule *models.CrawlRule) []ruleVideo {
	// JSON with an item path: each object holds a video's URL and preview fields.
	if rule.ItemPath != "" {
		items := gjson.Get(value, rule.ItemPath).Array()
		videos := make([]ruleVideo, 0, len(items))
		for _, item := range items {
			lv := &listedVideo{}
			for _, f := range rule.PreviewFields {
				setListedField(lv, f.Name, extractValue(item.Get(f.JSONPath).String(), "", f.Regex), e.Request.AbsoluteURL)
			}
			videos = append(videos, ruleVideo{URL: strings.TrimSpace(item.Get(rule.JSONPath).String()), listed: lv})
		}
		return videos
	}

	// HTML: preview fields are read inside the video's container (or the matched element).
	lv := &listedVideo{}
	if len(rule.PreviewFields) > 0 {
		scope := e.DOM
		if rule.Item != "" {
			if item := e.DOM.Closest(rule.Item); item.Length() > 0 {
				scope = item
			}
		}
		for _, f := range rule.PreviewFields {
			setListedField(lv, f.Name, previewFieldValue(scope, f), e.Request.AbsoluteURL)
		}
	}
	raws := jsonPathValues(value, rule.JSONPath)
	videos := make([]ruleVideo, 0, len(raws))
	for _, raw := range raws {
		videos = append(videos, ruleVideo{URL: strings.TrimSpace(raw), listed: lv})
	}
	return videos
}

// previewFieldValue reads a preview field from the first element matching its selector inside scope (or scope itself).
func previewFieldValue(scope *goquery.Selection, f models.MetadataRule) string {
	sel := scope
	if f.Selector != "" {
		sel = scope.Find(f.Selector).First()
	}
	if sel.Length() == 0 {
		return ""
	}
	value := sel.Text()
	if f.Attr != "" {
		value, _ = sel.Attr(f.Attr)
	}
	return extractValue(html.UnescapeString(value), f.JSONPath, f.Regex)
}

// setListedField sets a preview field on lv from a scraped value, keeping any value already set.
func setListedField(lv *listedVideo, name, value string, absURL func(string) string) {
	if value == "" {
		return
	}
	switch name {
	case sharedtags.JTitle:
		if lv.Title == "" {
			lv.Title = value
		}
	case sharedtags.JThumbnailURL:
		if lv.ThumbnailURL == "" {
			lv.ThumbnailURL = absURL(value)
		}
	case sharedtags.JUploadDate:
		if _, t := parseScrapedDate(value); lv.UploadDate.IsZero() && !t.IsZero() {
			lv.UploadDate = t
		}
	case consts.CrawlFieldDuration:
		if lv.Duration == 0 {
			lv.Duration = parseDuration(value)
		}
	}
}

// mergeListed adds a video to urls, filling details missing from an earlier match of the same URL.
func mergeListed(urls map[string]*listedVideo, link string, lv *listedVideo) {
	existing, ok := urls[link]
	if !ok {
		copied := *lv
		urls[link] = &copied
		return
	}
	existing.Title = cmp.Or(existing.Title, lv.Title)
	existing.ThumbnailURL = cmp.Or(existing.ThumbnailURL, lv.ThumbnailURL)
	existing.Duration = cmp.Or(existing.Duration, lv.Duration)
	if existing.UploadDate.IsZero() {
		existing.UploadDate = lv.UploadDate
	}
}

// parseDuration parses a duration in seconds, as "1:02:03", "62:03", "3723", or ISO 8601 "PT1H2M3S", returning 0 if invalid.
func parseDuration(s string) int {
	s = strings.TrimSpace(s)
	if secs, err := strconv.ParseFloat(s, 64); err == nil {
		return max(int(secs), 0)
	}
	if rest, ok := strings.CutPrefix(strings.ToUpper(s), "PT"); ok {
		d, err := time.ParseDuration(strings.ToLower(rest))
		if err != nil {
			return 0
		}
		return int(d.Seconds())
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0
	}
	total := 0
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 {
			return 0
		}
		total = total*60 + n
	}
	return total
}

// jsonPathValues returns the value itself, or the values at jsonPath if one is set.
func jsonPathValues(value, jsonPath string) []string {
	if jsonPath == "" {
		return []string{value}
	}

	res := gjson.Get(value, jsonPath)
	if !res.IsArray() {
		if res.Exists() {
			return []string{res.String()}
		}
		return nil
	}

	results := res.Array()
	values := make([]string, 0, len(results))
	for _, r := range results {
		values = append(values, r.String())
	}
	return values
}
