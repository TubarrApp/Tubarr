package utils

import (
	"Tubarr/internal/config"
	keys "Tubarr/internal/domain/keys"
	logging "Tubarr/internal/utils/logging"
	"bufio"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/gocolly/colly"
)

func GetNewChannelReleases() []string {
	var newURLs []string
	urlsToCheck := config.GetStringSlice(keys.ChannelCheckNew)

	for _, url := range urlsToCheck {
		if url == "" {
			continue
		}

		if cookies, err := GetBrowserCookies(url); err != nil {
			logging.PrintE(0, "Could not get cookies (%v)", err)
		} else if urls, err := newEpisodeURLs(url, cookies); err != nil {
			logging.PrintE(0, "Could not grab new episode (%v)", err)
		} else {
			newURLs = append(newURLs, urls...)
		}
	}

	logging.PrintI("Grabbed new episode URLs: %v", newURLs)
	return newURLs
}

// newEpisodeURLs checks for new episode URLs that are not yet in grabbed-urls.txt
func newEpisodeURLs(targetURL string, cookies []*http.Cookie) ([]string, error) {
	c := colly.NewCollector()

	for _, cookie := range cookies {
		c.SetCookies(targetURL, []*http.Cookie{cookie})
	}

	var episodeURLs []string

	switch {
	case strings.Contains(targetURL, "censored.tv"):
		episodeURLs = censoredTvEpisodes(c, episodeURLs)
	}

	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		link := e.Request.AbsoluteURL(e.Attr("href"))
		if strings.Contains(link, "/episode/") {
			episodeURLs = append(episodeURLs, link)
		}
	})

	// Visit the target URL
	err := c.Visit(targetURL)
	if err != nil {
		return nil, fmt.Errorf("error visiting webpage (%s): %v", targetURL, err)
	}

	// Load existing URLs from grabbed-urls.txt
	existingURLs, err := loadGrabbedURLsFromFile("grabbed-urls.txt")
	if err != nil {
		return nil, fmt.Errorf("error reading grabbed URLs file: %v", err)
	}

	// Filter out URLs that are already in grabbed-urls.txt
	var newURLs []string
	for _, url := range episodeURLs {
		if _, exists := existingURLs[url]; !exists {
			newURLs = append(newURLs, url)
		}
	}

	// Append new URLs to the file and return them
	if len(newURLs) > 0 {
		if err := appendURLsToFile("grabbed-urls.txt", newURLs); err != nil {
			return nil, fmt.Errorf("error appending new URLs to file: %v", err)
		}
	} else {
		logging.PrintI("No new videos at %s", targetURL)
	}
	return newURLs, nil
}

// loadURLsFromFile reads URLs from a file and returns them as a map for quick lookup
func loadGrabbedURLsFromFile(filename string) (map[string]struct{}, error) {
	videoDir := config.GetString(keys.VideoDir)
	var filepath string

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

// appendURLsToFile appends new URLs to the specified file
func appendURLsToFile(filename string, urls []string) error {
	logging.PrintD(2, "Appending URLs to file... %v", urls)
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	// Track URLs that have already been written
	written := make(map[string]bool)

	// Load existing URLs from the file into the map
	existingFile, err := os.Open(filename)
	if err == nil {
		defer existingFile.Close()
		var line string
		for scanner := bufio.NewScanner(existingFile); scanner.Scan(); {
			line = scanner.Text()
			written[line] = true
		}
	}

	// Append only new URLs to the file
	for _, url := range urls {
		if !written[url] {
			if _, err := file.WriteString(url + "\n"); err != nil {
				return err
			}
			written[url] = true
		}
	}

	return nil
}
