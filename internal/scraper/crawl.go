package scraper

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"

	"github.com/gocolly/colly"
	"github.com/tidwall/gjson"
)

// TestCrawlSite runs the crawler Tubarr would use for a channel URL and returns the video URLs found.
//
// Used by the web UI to preview crawl rules. matched is false if the URL would be crawled with yt-dlp.
func TestCrawlSite(channelURL string) (matched bool, source string, urls []string, err error) {
	source, found, matched, err := New().crawlChannelURLs(channelURL, nil)
	if !matched || err != nil {
		return matched, source, nil, err
	}

	urls = make([]string, 0, len(found))
	for u := range found {
		urls = append(urls, u)
	}
	slices.Sort(urls)
	return true, source, urls, nil
}

// crawlChannelURLs extracts video URLs from a channel page using a custom crawl rule or built-in scraper.
//
// crawled is false if neither applies to the URL, and yt-dlp should be used instead.
func (s *Scraper) crawlChannelURLs(channelURL string, cookies []*http.Cookie) (source string, urls map[string]struct{}, crawled bool, err error) {
	// User-defined crawl rules take priority over built-in scrapers.
	if query, ok := matchCrawlSite(channelURL); ok {
		logger.Pl.I("Using custom crawl rule for %q", query.Site)
		urls, err = s.crawlWithRule(channelURL, cookies, query)
		return "custom crawl rule for " + query.Site, urls, true, err
	}

	var pattern urlPattern
	for domain, p := range patterns {
		if domain != defaultDom && strings.Contains(channelURL, domain) {
			pattern = p
			break
		}
	}
	if pattern.name == "" {
		return "", nil, false, nil
	}
	logger.Pl.I("Detected %s link", pattern.name)
	source = "built-in " + pattern.name + " crawler"

	if pattern.name == rumble {
		urls, err = s.scrapeRumbleChannelURLs(channelURL, cookies)
		return source, urls, true, err
	}

	var impersonate consts.Impersonate
	if query, ok := matchSite(channelURL, func(consts.HTMLMetadataQuery) bool { return true }); ok {
		impersonate = consts.Impersonate(query.Impersonate)
	}
	urls = make(map[string]struct{})
	err = s.crawlChannelPage(channelURL, cookies, impersonate, "a[href]", func(e *colly.HTMLElement) {
		if link := e.Request.AbsoluteURL(e.Attr("href")); strings.Contains(link, pattern.pattern) {
			urls[link] = struct{}{}
		}
	})
	return source, urls, true, err
}

// crawlChannelPage visits a channel page and passes each element matching the selector to onMatch.
func (s *Scraper) crawlChannelPage(channelURL string, cookies []*http.Cookie, impersonate consts.Impersonate, selector string, onMatch colly.HTMLCallback) error {
	c, err := initializeCollector(channelURL, s.cookieManager, impersonate)
	if err != nil {
		return err
	}
	if len(cookies) > 0 {
		if err := c.SetCookies(channelURL, cookies); err != nil {
			return fmt.Errorf("failed to set cookies for channel page %q: %w", channelURL, err)
		}
	}

	var reqErr error
	c.OnError(func(r *colly.Response, err error) {
		reqErr = fmt.Errorf("channel page %q request failed (HTTP %d): %w", channelURL, r.StatusCode, err)
	})
	c.OnHTML(selector, onMatch)

	if err := c.Visit(channelURL); err != nil {
		return fmt.Errorf("error visiting channel page %q: %w", channelURL, err)
	}
	c.Wait()
	return reqErr
}

// crawlWithRule extracts video URLs from a channel page using a user-defined crawl rule.
func (s *Scraper) crawlWithRule(channelURL string, cookies []*http.Cookie, query consts.HTMLMetadataQuery) (map[string]struct{}, error) {
	rule := query.Crawl
	urls := make(map[string]struct{})

	err := s.crawlChannelPage(channelURL, cookies, consts.Impersonate(query.Impersonate), rule.Selector, func(e *colly.HTMLElement) {
		value := e.Text
		if rule.Attr != "" {
			value = e.Attr(rule.Attr)
		}
		for _, raw := range crawlRuleValues(value, rule.JSONPath) {
			link := e.Request.AbsoluteURL(strings.TrimSpace(raw))
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
			urls[link] = struct{}{}
		}
	})
	if err != nil {
		return nil, err
	}

	logger.Pl.I("Extracted %d video URLs from %q using crawl rule for %q", len(urls), channelURL, query.Site)
	return urls, nil
}

// crawlRuleValues returns the value itself, or the values at jsonPath if one is set.
func crawlRuleValues(value, jsonPath string) []string {
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
