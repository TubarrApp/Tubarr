package scraper

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/network"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly"
)

// ruleValue runs a scraped value through a rule's json_path, regex, and follow-up requests, returning "" if any stage
// finds nothing.
func ruleValue(c *colly.Collector, pageURL, value string, rule models.MetadataRule) string {
	value = extractValue(value, rule.JSONPath, rule.Regex)
	if value == "" {
		logger.Pl.D(2, "Selector %q for field %q: nothing left after json_path/regex", rule.Selector, rule.Name)
		return ""
	}

	prevURL := pageURL
	for i, step := range rule.Fetch {
		stepURL, err := fetchStepURL(prevURL, step.URL, value)
		if err != nil {
			logger.Pl.W("Field %q fetch step %d: %v", rule.Name, i+1, err)
			return ""
		}

		if err := checkFetchTarget(pageURL, stepURL); err != nil {
			logger.Pl.W("Field %q fetch step %d: %v", rule.Name, i+1, err)
			return ""
		}

		logger.Pl.D(2, "Field %q fetch step %d: requesting %q", rule.Name, i+1, stepURL)
		body, err := fetchBody(c, stepURL, prevURL)
		if err != nil {
			logger.Pl.W("Field %q fetch step %d: request for %q failed: %v", rule.Name, i+1, stepURL, err)
			return ""
		}

		value = string(body)
		if step.Selector != "" {
			if value, err = selectValue(body, step.Selector, step.Attr); err != nil {
				logger.Pl.W("Field %q fetch step %d: failed to parse %q as HTML: %v", rule.Name, i+1, stepURL, err)
				return ""
			}
		}
		if value = extractValue(value, step.JSONPath, step.Regex); value == "" {
			logger.Pl.D(2, "Field %q fetch step %d: nothing found in response from %q", rule.Name, i+1, stepURL)
			return ""
		}
		prevURL = stepURL
	}
	return value
}

// extractValue applies a gjson path (first result) and then a regex (capture group 1, or the whole match) to a value.
func extractValue(value, jsonPath string, re *regexp.Regexp) string {
	if jsonPath != "" {
		values := jsonPathValues(value, jsonPath)
		if len(values) == 0 {
			return ""
		}
		value = values[0]
	}
	if re != nil {
		m := re.FindStringSubmatch(value)
		switch {
		case m == nil:
			return ""
		case len(m) > 1:
			value = m[1]
		default:
			value = m[0]
		}
	}
	return strings.TrimSpace(value)
}

// fetchStepURL fills {value} into a fetch step's URL template and resolves it against the previous URL.
func fetchStepURL(prevURL, template, value string) (string, error) {
	base, err := url.Parse(prevURL)
	if err != nil {
		return "", fmt.Errorf("invalid previous URL %q: %w", prevURL, err)
	}
	filled := strings.ReplaceAll(template, "{value}", value)
	ref, err := url.Parse(filled)
	if err != nil {
		return "", fmt.Errorf("invalid URL %q: %w", filled, err)
	}
	u := base.ResolveReference(ref)
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("URL %q is not an http(s) URL", u)
	}
	return u.String(), nil
}

// checkFetchTarget refuses a fetch step to a private address when the scraped page is on a public host.
//
// Fetch URLs are built from page content, so a public page must not be able to point Tubarr at the local network.
// Pages hosted on the local network may still fetch from it.
func checkFetchTarget(pageURL, targetURL string) error {
	page, err := url.Parse(pageURL)
	if err != nil {
		return fmt.Errorf("invalid page URL %q: %w", pageURL, err)
	}
	target, err := url.Parse(targetURL)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", targetURL, err)
	}
	if !network.IsPrivateNetwork(page.Hostname()) && network.IsPrivateNetwork(target.Hostname()) {
		return fmt.Errorf("refusing request to local network address %q from public page %q", target.Host, page.Host)
	}
	return nil
}

// fetchBody requests a URL with a clone of the page's collector (sharing its transport, user agent, and cookies).
func fetchBody(c *colly.Collector, target, referer string) ([]byte, error) {
	fc := c.Clone()
	fc.AllowURLRevisit = true

	var (
		body   []byte
		reqErr error
	)
	fc.OnRequest(func(r *colly.Request) {
		r.Headers.Set("Referer", referer)
	})
	fc.OnResponse(func(r *colly.Response) {
		body = r.Body
	})
	fc.OnError(func(r *colly.Response, err error) {
		reqErr = fmt.Errorf("HTTP %d: %w", r.StatusCode, err)
	})

	if err := fc.Visit(target); err != nil {
		return nil, err
	}
	fc.Wait()
	return body, reqErr
}

// selectValue returns the first non-empty attribute (or text) of the elements matching selector in an HTML body.
func selectValue(body []byte, selector, attr string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	var value string
	doc.Find(selector).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if attr != "" {
			value, _ = s.Attr(attr)
		} else {
			value = s.Text()
		}
		value = strings.TrimSpace(value)
		return value == ""
	})
	return value, nil
}
