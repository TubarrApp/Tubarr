package utils

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"strings"
	"tubarr/internal/config"
	keys "tubarr/internal/domain/keys"
	logging "tubarr/internal/utils/logging"

	"github.com/gocolly/colly"
)

// GetNewReleases checks a channel URL for URLs which have not yet been recorded as downloaded
func GetNewReleases() []string {
	uniqueURLs := make(map[string]struct{})
	urlsToCheck := config.GetStringSlice(keys.ChannelCheckNew)

	for _, url := range urlsToCheck {
		if url == "" {
			continue
		}

		cookies, err := getBrowserCookies(url)
		if err != nil {
			logging.PrintE(0, "Could not get cookies for %s: %v", url, err)
			continue
		}

		urls, err := newEpisodeURLs(url, cookies)
		if err != nil {
			logging.PrintE(0, "Could not grab episodes from %s: %v", url, err)
			continue
		}

		// Add unique URLs to map
		for _, newURL := range urls {
			if newURL != "" {
				uniqueURLs[newURL] = struct{}{}
			}
		}
	}
	// Convert map to slice
	var newURLs = make([]string, 0, len(uniqueURLs))
	for url := range uniqueURLs {
		newURLs = append(newURLs, url)
	}

	// Log results
	if len(newURLs) > 0 {
		logging.PrintI("Grabbed %d new episode URLs: %v", len(newURLs), newURLs)
	}

	return newURLs
}

// newEpisodeURLs checks for new episode URLs that are not yet in grabbed-urls.txt
func newEpisodeURLs(targetURL string, cookies []*http.Cookie) ([]string, error) {

	// var (
	// 	authorized bool
	// 	authTag    string
	// )

	c := colly.NewCollector()
	uniqueEpisodeURLs := make(map[string]struct{})

	for _, cookie := range cookies {
		c.SetCookies(targetURL, []*http.Cookie{cookie})
	}
	// Video URL link pattern
	switch {
	case strings.Contains(targetURL, "bitchute.com"):
		logging.PrintI("Detected bitchute.com link")
		c.OnHTML("a[href]", func(e *colly.HTMLElement) {
			link := e.Request.AbsoluteURL(e.Attr("href"))
			if strings.Contains(link, "/video/") {
				uniqueEpisodeURLs[link] = struct{}{}
			}
			// authorized = true
		})

	case strings.Contains(targetURL, "censored.tv"):
		logging.PrintI("Detected censored.tv link")
		c.OnHTML("a[href]", func(e *colly.HTMLElement) {
			link := e.Request.AbsoluteURL(e.Attr("href"))
			if strings.Contains(link, "/episode/") {
				uniqueEpisodeURLs[link] = struct{}{}
			}
		})
		// authorized = true
		// c.OnHTML(".dropdown-toggle-image.spark-nav-profile-photo", func(e *colly.HTMLElement) {
		// 	authTag = "Found sign of authentication: '<img :src=\"user.photo_url\" class=\"dropdown-toggle-image spark-nav-profile-photo\" alt=\"User Photo\" />'"
		// 	authorized = true
		// })
		// c.OnHTML("spark-notifications", func(e *colly.HTMLElement) {
		// 	authTag = "Found sign of authentication '<spark-notifications'"
		// 	authorized = true
		// })

	case strings.Contains(targetURL, "odysee.com"):
		logging.PrintI("Detected Odysee link")
		c.OnHTML("a[href]", func(e *colly.HTMLElement) {
			link := e.Request.AbsoluteURL(e.Attr("href"))
			parts := strings.Split(link, "/")
			if len(parts) > 1 {
				lastPart := parts[len(parts)-1]
				if strings.Contains(link, "@") && strings.Contains(link, lastPart+"/") {
					uniqueEpisodeURLs[link] = struct{}{}
				}
			}
			// authorized = true
		})

	case strings.Contains(targetURL, "rumble.com"):
		logging.PrintI("Detected Rumble link")
		c.OnHTML("a[href]", func(e *colly.HTMLElement) {
			link := e.Request.AbsoluteURL(e.Attr("href"))
			if strings.Contains(link, "/v") {
				uniqueEpisodeURLs[link] = struct{}{}
			}
			// authorized = true
		})

	default:
		logging.PrintI("Using default link detection")
		c.OnHTML("a[href]", func(e *colly.HTMLElement) {
			link := e.Request.AbsoluteURL(e.Attr("href"))
			if strings.Contains(link, "/watch") {
				uniqueEpisodeURLs[link] = struct{}{}
			}
			// authorized = true
		})
	}

	// Visit the target URL
	err := c.Visit(targetURL)
	if err != nil {
		return nil, fmt.Errorf("error visiting webpage (%s): %v", targetURL, err)
	}
	c.Wait()

	// if authorized {
	// 	logging.PrintS(1, "Found sign of successful authentication: %s", authTag)
	// }

	// Convert unique URLs map to slice
	var episodeURLs = make([]string, 0, len(uniqueEpisodeURLs))
	for url := range uniqueEpisodeURLs {
		episodeURLs = append(episodeURLs, url)
	}

	// Load existing URLs from grabbed-urls.txt
	existingURLs, err := loadGrabbedURLsFromFile("grabbed-urls.txt")
	if err != nil {
		return nil, fmt.Errorf("error reading grabbed URLs file: %v", err)
	}

	// Filter out URLs that are already in grabbed-urls.txt
	var newURLs = make([]string, 0, len(episodeURLs))
	for _, url := range episodeURLs {
		normalizedURL := normalizeURL(url)
		exists := false

		for existingURL := range existingURLs {
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
		logging.PrintI("No new videos at %s", targetURL)
		return nil, nil
	}
	return newURLs, nil
}

// loadGrabbedURLsFromFile reads URLs from a file and returns them as a map for quick lookup
func loadGrabbedURLsFromFile(filename string) (map[string]struct{}, error) {

	var (
		filepath string
	)

	videoDir := config.GetString(keys.VideoDir)

	switch strings.HasSuffix(videoDir, "/") {
	case false:
		filepath = videoDir + "/" + filename
	default:
		filepath = videoDir + filename
	}

	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	urlMap := make(map[string]struct{})
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		url := scanner.Text()
		urlMap[url] = struct{}{}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return urlMap, nil
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
