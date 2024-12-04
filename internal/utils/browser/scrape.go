// Package browser handles operations relating to web scraping, cookie gathering, etc.
package browser

import (
	"fmt"
	"net/http"
	"strings"

	"tubarr/internal/cfg"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/interfaces"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"

	"github.com/gocolly/colly"
)

type Browser struct {
	cookies   *CookieManager
	collector *colly.Collector
}

type urlPattern struct {
	name    string
	pattern string
}

const (
	bitchute        = "bitchute.com"
	bitchutePattern = "/video/"
	censored        = "censored.tv"
	censoredPattern = "/episode/"
	odysee          = "odysee.com"
	odyseePattern   = "@"
	rumble          = "rumble.com"
	rumblePattern   = "/v"
	defaultDom      = "default"
	defaultPattern  = "/watch"
)

var patterns = map[string]urlPattern{
	bitchute:   {name: bitchute, pattern: bitchutePattern},
	censored:   {name: censored, pattern: censoredPattern},
	odysee:     {name: odysee, pattern: odyseePattern},
	rumble:     {name: rumble, pattern: rumblePattern},
	defaultDom: {name: defaultDom, pattern: defaultPattern},
}

func NewBrowser() *Browser {
	return &Browser{
		cookies:   NewCookieManager(),
		collector: colly.NewCollector(),
	}
}

// GetNewReleases checks a channel URL for URLs which have not yet been recorded as downloaded.
func (b *Browser) GetNewReleases(cs interfaces.ChannelStore, c *models.Channel) ([]*models.Video, error) {
	if c.URL == "" {
		return nil, fmt.Errorf("channel url is blank (channel ID: %d)", c.ID)
	}

	existingURLs, err := cs.LoadGrabbedURLs(c)
	if err != nil {
		return nil, err
	}

	existingMap := make(map[string]struct{}, len(existingURLs))
	for _, url := range existingURLs {
		existingMap[url] = struct{}{}
	}

	if len(existingMap) > 0 {
		logging.I("Found %d existing downloaded video URLs:", len(existingMap))
		i := 1
		for url := range existingMap {
			logging.P("%s#%d%s - %v", consts.ColorBlue, i, consts.ColorReset, url)
			if i == 25 {
				break
			}
			i++
		}
	}

	cookies, err := b.cookies.GetCookies(c.URL)
	if err != nil {
		return nil, err
	}

	var fileURLs []string
	if cfg.IsSet(keys.URLFile) {
		prs := parsing.NewURLFileParser(cfg.GetString(keys.URLFile))
		if fileURLs, err = prs.ParseURLs(); err != nil {
			return nil, err
		}
	}

	newURLs, err := b.newEpisodeURLs(c.URL, existingURLs, fileURLs, cookies)
	if err != nil {
		return nil, err
	}

	newRequests := make([]*models.Video, 0, len(newURLs))
	for _, newURL := range newURLs {
		if newURL != "" {
			if _, exists := existingMap[newURL]; !exists {
				newRequests = append(newRequests, &models.Video{
					ChannelID:  c.ID,
					URL:        newURL,
					VideoDir:   c.VideoDir,
					JSONDir:    c.JSONDir,
					Channel:    c,
					Settings:   c.Settings,
					MetarrArgs: c.MetarrArgs,
				})
			}
		}
	}

	if len(newRequests) > 0 {
		logging.I("Grabbed %d new download requests: %v", len(newRequests), newRequests)
	}

	return newRequests, nil
}

// newEpisodeURLs checks for new episode URLs that are not yet in grabbed-urls.txt
func (b *Browser) newEpisodeURLs(targetURL string, existingURLs, fileURLs []string, cookies []*http.Cookie) ([]string, error) {
	uniqueEpisodeURLs := make(map[string]struct{})

	// Set cookies
	for _, cookie := range cookies {
		if err := b.collector.SetCookies(targetURL, []*http.Cookie{cookie}); err != nil {
			return nil, err
		}
	}

	// Only scrape website if we're not using a URL file
	if !cfg.IsSet(keys.URLFile) {
		pattern := patterns["default"]
		for domain, p := range patterns {
			if strings.Contains(targetURL, domain) {
				pattern = p
				logging.I("Detected %s link", p.name)
				break
			}
		}

		b.collector.OnHTML("a[href]", func(e *colly.HTMLElement) {
			link := e.Request.AbsoluteURL(e.Attr("href"))
			if strings.Contains(link, pattern.pattern) {
				uniqueEpisodeURLs[link] = struct{}{}
			}
		})

		if err := b.collector.Visit(targetURL); err != nil {
			return nil, fmt.Errorf("error visiting webpage (%s): %w", targetURL, err)
		}
		b.collector.Wait()
	}

	// Collect URLs from all sources (scraping + files)
	episodeURLs := make([]string, 0, len(uniqueEpisodeURLs)+len(fileURLs))
	for url := range uniqueEpisodeURLs {
		episodeURLs = append(episodeURLs, url)
	}
	episodeURLs = append(episodeURLs, fileURLs...)

	if cfg.IsSet(keys.URLAdd) {
		episodeURLs = append(episodeURLs, cfg.GetStringSlice(keys.URLAdd)...)
	}

	// Filter out existing URLs
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

// normalizeURL standardizes URLs for comparison by removing protocol and any trailing slashes.
//
// Do NOT add a "ToLower" function as some sites like YouTube have case-sensitive URLs.
func normalizeURL(inputURL string) string {
	cleanURL := strings.TrimPrefix(inputURL, "https://")
	cleanURL = strings.TrimPrefix(cleanURL, "http://")
	return strings.TrimSuffix(cleanURL, "/")
}
