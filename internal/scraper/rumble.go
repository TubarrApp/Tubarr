package scraper

import (
	"encoding/json"
	"net/http"
	"strings"
	"tubarr/internal/domain/consts"
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

// scrapeRumbleChannelURLs extracts video URLs from a Rumble channel page, impersonating Chrome.
func (s *Scraper) scrapeRumbleChannelURLs(channelURL string, cookies []*http.Cookie) (map[string]struct{}, error) {
	urls := make(map[string]struct{})
	lowerChannelURL := strings.ToLower(channelURL)

	err := s.crawlChannelPage(channelURL, cookies, consts.ImpersonateChrome, `rum-videos-grid script[type="application/json"]`, func(e *colly.HTMLElement) {
		var data rumbleGridData
		if err := json.Unmarshal([]byte(e.Text), &data); err != nil {
			logger.Pl.E("Failed to parse Rumble video grid JSON: %v", err)
			return
		}
		for _, item := range data.Items {
			if item.ObjectType != "video" {
				continue
			}
			// by.url is the lowercased channel URL without any trailing path (e.g. /videos),
			// so check that channelURL starts with it rather than requiring exact match.
			if item.By.URL != "" && !strings.HasPrefix(lowerChannelURL, strings.ToLower(item.By.URL)) {
				logger.Pl.D(2, "Skipping video from different channel (by: %q)", item.By.URL)
				continue
			}
			if isValidRumbleVideoURL(item.URL) {
				urls[removeQueryParams(item.URL)] = struct{}{}
			}
		}
	})
	if err != nil {
		return nil, err
	}

	logger.Pl.I("Extracted %d video URLs from Rumble channel page %q", len(urls), channelURL)
	return urls, nil
}
