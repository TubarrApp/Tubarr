package scraper

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"tubarr/internal/domain/logger"

	"github.com/gocolly/colly"
)

type rumbleGridData struct {
	Items []struct {
		ObjectType string `json:"object_type"`
		URL        string `json:"url"`
		By         struct {
			URL string `json:"url"`
		} `json:"by"`
	} `json:"items"`
}

// scrapeRumbleChannelURLs extracts video URLs from a Rumble channel page.
func scrapeRumbleChannelURLs(channelURL string, cookies []*http.Cookie) (map[string]struct{}, error) {
	urls := make(map[string]struct{})

	c := colly.NewCollector()

	if len(cookies) > 0 {
		if err := c.SetCookies(channelURL, cookies); err != nil {
			return nil, fmt.Errorf("failed to set cookies for Rumble scrape: %w", err)
		}
	}

	c.OnError(func(r *colly.Response, err error) {
		logger.Pl.E("Rumble channel page request failed (HTTP %d): %v", r.StatusCode, err)
	})

	c.OnHTML(`rum-videos-grid script[type="application/json"]`, func(e *colly.HTMLElement) {
		var data rumbleGridData
		if err := json.Unmarshal([]byte(e.Text), &data); err != nil {
			logger.Pl.E("Failed to parse Rumble video grid JSON: %v", err)
			return
		}
		for _, item := range data.Items {
			if item.ObjectType != "video" {
				continue
			}
			// by.url is the channel URL without any trailing path (e.g. /videos),
			// so check that channelURL starts with it rather than requiring exact match.
			if item.By.URL != "" && !strings.HasPrefix(channelURL, item.By.URL) {
				logger.Pl.D(2, "Skipping video from different channel (by: %q)", item.By.URL)
				continue
			}
			if isValidRumbleVideoURL(item.URL) {
				urls[removeQueryParams(item.URL)] = struct{}{}
			}
		}
	})

	if err := c.Visit(channelURL); err != nil {
		return nil, fmt.Errorf("error visiting Rumble channel %q: %w", channelURL, err)
	}

	logger.Pl.I("Extracted %d video URLs from Rumble channel page %q", len(urls), channelURL)
	return urls, nil
}
