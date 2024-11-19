package browser

import (
	"fmt"
	"net/http"
	"strings"
	"tubarr/internal/interfaces"
	"tubarr/internal/models"
	logging "tubarr/internal/utils/logging"

	"github.com/gocolly/colly"
)

// GetNewReleases checks a channel URL for URLs which have not yet been recorded as downloaded
func GetNewReleases(cs interfaces.ChannelStore, c *models.Channel) ([]*models.Video, error) {

	uniqueURLs := make(map[string]struct{})

	existingURLs, err := cs.LoadGrabbedURLs(c)
	if err != nil {
		return nil, err
	}

	if len(existingURLs) > 0 {
		fmt.Println()
		logging.I("Found existing downloaded video URLs:")
		count := 1
		for _, u := range existingURLs {
			logging.P("Entry %d: %v", count, u)
			count++
		}
		fmt.Println()
	}

	if c.URL == "" {
		return nil, fmt.Errorf("channel url is blank")
	}

	cookies, err := getBrowserCookies(c.URL)
	if err != nil {
		return nil, err
	}

	newURLs, err := newEpisodeURLs(c.URL, existingURLs, cookies)
	if err != nil {
		return nil, err
	}

	// Add unique URLs to map
	for _, newURL := range newURLs {
		if newURL != "" {
			uniqueURLs[newURL] = struct{}{}
		}
	}

	// Convert map to slice
	newRequests := make([]*models.Video, 0, len(uniqueURLs))

	for url := range uniqueURLs {
		newRequests = append(newRequests, &models.Video{
			ChannelID:  c.ID,
			URL:        url,
			VDir:       c.VDir,
			JDir:       c.JDir,
			Channel:    c,
			Settings:   c.Settings,
			MetarrArgs: c.MetarrArgs,
		})
	}

	// Display results
	if len(newRequests) > 0 {
		logging.I("Grabbed %d new download requests: %v", len(newRequests), uniqueURLs)
	}

	return newRequests, nil
}

// newEpisodeURLs checks for new episode URLs that are not yet in grabbed-urls.txt
func newEpisodeURLs(targetURL string, existingURLs []string, cookies []*http.Cookie) ([]string, error) {

	c := colly.NewCollector()
	uniqueEpisodeURLs := make(map[string]struct{})

	for _, cookie := range cookies {
		c.SetCookies(targetURL, []*http.Cookie{cookie})
	}

	// Video URL link pattern
	switch {
	case strings.Contains(targetURL, "bitchute.com"):
		logging.I("Detected bitchute.com link")
		c.OnHTML("a[href]", func(e *colly.HTMLElement) {
			link := e.Request.AbsoluteURL(e.Attr("href"))
			if strings.Contains(link, "/video/") {
				uniqueEpisodeURLs[link] = struct{}{}
			}
		})

	case strings.Contains(targetURL, "censored.tv"):
		logging.I("Detected censored.tv link")
		c.OnHTML("a[href]", func(e *colly.HTMLElement) {
			link := e.Request.AbsoluteURL(e.Attr("href"))
			if strings.Contains(link, "/episode/") {
				uniqueEpisodeURLs[link] = struct{}{}
			}
		})

	case strings.Contains(targetURL, "odysee.com"):
		logging.I("Detected Odysee link")
		c.OnHTML("a[href]", func(e *colly.HTMLElement) {
			link := e.Request.AbsoluteURL(e.Attr("href"))
			parts := strings.Split(link, "/")
			if len(parts) > 1 {
				lastPart := parts[len(parts)-1]
				if strings.Contains(link, "@") && strings.Contains(link, lastPart+"/") {
					uniqueEpisodeURLs[link] = struct{}{}
				}
			}
		})

	case strings.Contains(targetURL, "rumble.com"):
		logging.I("Detected Rumble link")
		c.OnHTML("a[href]", func(e *colly.HTMLElement) {
			link := e.Request.AbsoluteURL(e.Attr("href"))
			if strings.Contains(link, "/v") {
				uniqueEpisodeURLs[link] = struct{}{}
			}
		})

	default:
		logging.I("Using default link detection")
		c.OnHTML("a[href]", func(e *colly.HTMLElement) {
			link := e.Request.AbsoluteURL(e.Attr("href"))
			if strings.Contains(link, "/watch") {
				uniqueEpisodeURLs[link] = struct{}{}
			}
		})
	}

	// Visit the target URL
	err := c.Visit(targetURL)
	if err != nil {
		return nil, fmt.Errorf("error visiting webpage (%s): %v", targetURL, err)
	}
	c.Wait()

	// Convert unique URLs map to slice
	var episodeURLs = make([]string, 0, len(uniqueEpisodeURLs))
	for url := range uniqueEpisodeURLs {
		episodeURLs = append(episodeURLs, url)
	}

	// Filter out URLs that are already in grabbed-urls.txt
	var newURLs = make([]string, 0, len(episodeURLs))
	for _, url := range episodeURLs {
		normalizedURL := normalizeURL(url)
		exists := false

		for _, existingURL := range existingURLs {
			if normalizeURL(existingURL) == normalizedURL {
				exists = true
				break
			}
		}
		if !exists {
			newURLs = append(newURLs, url)
		}
	}
	if len(newURLs) == 0 {
		logging.I("No new videos at %s", targetURL)
		return nil, nil
	}
	return newURLs, nil
}

// normalizeURL standardizes URLs for comparison by removing protocol and any trailing slashes
func normalizeURL(inputURL string) string {
	// Remove http:// or https://
	cleanURL := strings.TrimPrefix(inputURL, "https://")
	cleanURL = strings.TrimPrefix(cleanURL, "http://")

	// Remove any trailing slash
	cleanURL = strings.TrimSuffix(cleanURL, "/")

	return cleanURL
}
