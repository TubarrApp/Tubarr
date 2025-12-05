// Package scraper handles web scraping operations.
package scraper

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"tubarr/internal/contracts"
	"tubarr/internal/dev"
	"tubarr/internal/domain/command"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"

	"github.com/TubarrApp/gocommon/abstractions"
	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/sharedtags"

	"github.com/gocolly/colly"
	"golang.org/x/net/publicsuffix"
)

// Scraper handles web scraping operations.
type Scraper struct {
	collector     *colly.Collector
	cookieManager *CookieManager
}

// New returns a new Scraper instance.
func New() *Scraper {
	return &Scraper{
		collector:     colly.NewCollector(),
		cookieManager: NewCookieManager(),
	}
}

// GetExistingReleases returns releases already in the database.
func (s *Scraper) GetExistingReleases(cs contracts.ChannelStore, c *models.Channel) (existingURLsMap map[string]struct{}, existingURLs []string, err error) {
	existingURLs, err = cs.GetDownloadedOrIgnoredVideoURLs(c)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}

	existingMap := make(map[string]struct{}, len(existingURLs))
	for _, url := range existingURLs {
		existingMap[url] = struct{}{}
	}

	logger.Pl.D(2, "Loaded %d existing downloaded video URLs for channel %q", len(existingMap), c.Name)
	return existingMap, existingURLs, nil
}

// GetNewReleases checks a channel's URLs for new video URLs that haven't been recorded as downloaded.
func (s *Scraper) GetNewReleases(ctx context.Context, cs contracts.ChannelStore, c *models.Channel) ([]*models.Video, error) {
	if len(c.URLModels) == 0 {
		return nil, fmt.Errorf("channel has no URLs (channel ID: %d)", c.ID)
	}

	existingMap, existingURLs, err := s.GetExistingReleases(cs, c)
	if err != nil {
		return nil, err
	}

	var newRequests []*models.Video

	// Process each ChannelURL
	for _, cu := range c.URLModels {
		if cu.IsManual {
			continue
		}
		logger.Pl.D(1, "Processing channel URL %q", cu.URL)

		// Get access details once per ChannelURL - delegated to CookieManager
		cu.Cookies, cu.CookiePath, err = s.cookieManager.GetChannelCookies(ctx, cs, c, cu)
		if err != nil {
			return nil, err
		}

		// Fetch new episode URLs
		newEpisodeURLs, err := s.newEpisodeURLs(ctx, c.Name, cu.URL, existingURLs, nil, cu.Cookies, cu.CookiePath)
		if err != nil {
			return nil, err
		}

		// Filter and create video requests
		for _, newURL := range newEpisodeURLs {
			if _, exists := existingMap[newURL]; !exists {
				video := &models.Video{
					ChannelID:    c.ID,
					ChannelURLID: cu.ID,
					URL:          newURL,
				}
				cu.Videos = append(cu.Videos, video)
				newRequests = append(newRequests, video)
			}
		}
	}

	// Print summary
	if len(newRequests) > 0 {
		logger.Pl.I("Found %d new video URL requests for channel %q:", len(newRequests), c.Name)
		for i, v := range newRequests {
			if v == nil {
				continue
			}
			logger.Pl.P("%s#%d%s - %q", sharedconsts.ColorBlue, i+1, sharedconsts.ColorReset, v.URL)
			if i >= consts.MaxDisplayedVideos {
				break
			}
		}
	}

	return newRequests, nil
}

// ScrapeCustomSite scrapes custom sites for metadata.
func (s *Scraper) ScrapeCustomSite(urlStr, outputDir string, v *models.Video) error {
	// Initialize collector with cookies.
	collector, err := initializeCollector(urlStr, s.cookieManager)
	if err != nil {
		return err
	}

	// Declare metadata variable.
	var metadata map[string]any

	// Determine which rule set to use based on URL.
	switch {
	case strings.Contains(v.URL, "censored.tv"):
		if !dev.CensoredTVUseCustom {
			logger.Pl.I("Using regular scraper for censored.tv ...")
			return nil
		}
		metadata = s.ScrapeWithRules(urlStr, collector, v, consts.HTMLCensored)
	case strings.Contains(v.URL, "bitchute.com"):
		metadata = s.ScrapeWithRules(urlStr, collector, v, consts.HTMLBitchute)
	case strings.Contains(v.URL, "odysee.com"):
		metadata = s.ScrapeWithRules(urlStr, collector, v, consts.HTMLOdysee)
	case strings.Contains(v.URL, "rumble.com"):
		metadata = s.ScrapeWithRules(urlStr, collector, v, consts.HTMLRumble)
	default:
		logger.Pl.D(1, "No custom scraping rules found for URL: %s - will use yt-dlp", v.URL)
		return nil // Not a custom site.
	}

	// Visit the webpage.
	if err := collector.Visit(urlStr); err != nil {
		return fmt.Errorf("failed to visit URL: %w", err)
	}
	collector.Wait()

	// Validate required fields.
	if v.Title == "" {
		logger.Pl.D(3, "Scraped metadata: %+v", metadata)
		return fmt.Errorf("missing required metadata fields (title: %q)", v.Title)
	}

	// Write metadata to file.
	filename := fmt.Sprintf("%s.json", sanitizeFilename(v.Title))
	if err := file.WriteMetadataJSONFile(metadata, filename, outputDir, v); err != nil {
		return fmt.Errorf("failed to write metadata JSON: %w", err)
	}

	logger.Pl.S("Successfully wrote metadata JSON to %s/%s", outputDir, filename)
	return nil
}

// GetChannelCookies is now just a wrapper that delegates to CookieManager
func (s *Scraper) GetChannelCookies(ctx context.Context, cs contracts.ChannelStore, c *models.Channel, cu *models.ChannelURL) (cookies []*http.Cookie, cookieFilePath string, err error) {
	return s.cookieManager.GetChannelCookies(ctx, cs, c, cu)
}

// newEpisodeURLs checks for new episode URLs that are not yet in grabbed-urls.txt
func (s *Scraper) newEpisodeURLs(
	ctx context.Context,
	channelName, channelURL string,
	existingURLs, fileURLs []string,
	cookies []*http.Cookie, cookiePath string) ([]string, error) {
	// Episode map to avoid deduplication
	uniqueEpisodeURLs := make(map[string]struct{})

	// Set cookies
	if len(cookies) > 0 {
		for _, cookie := range cookies {
			if err := s.collector.SetCookies(channelURL, []*http.Cookie{cookie}); err != nil {
				return nil, err
			}
		}
	}

	// Check if domain matches any custom Tubarr domains
	var customDom bool
	pattern := patterns["default"]
	for domain, p := range patterns {
		if strings.Contains(channelURL, domain) {
			pattern = p
			logger.Pl.I("Detected %s link", p.name)
			customDom = true
			break
		}
	}

	s.collector.OnHTML("a[href]", func(e *colly.HTMLElement) {
		link := e.Request.AbsoluteURL(e.Attr("href"))
		if strings.Contains(link, pattern.pattern) {
			uniqueEpisodeURLs[link] = struct{}{}
		}
	})

	if customDom {
		if err := s.collector.Visit(channelURL); err != nil {
			return nil, fmt.Errorf("error visiting webpage %q: %w", channelURL, err)
		}
		s.collector.Wait()
	} else {
		var err error
		if uniqueEpisodeURLs, err = ytDlpURLFetch(ctx, channelName, channelURL, uniqueEpisodeURLs, cookiePath); err != nil {
			return nil, err
		}
	}

	// Collect URLs from all sources (scraped + file)
	episodeURLs := make([]string, 0, len(uniqueEpisodeURLs)+len(fileURLs))
	for url := range uniqueEpisodeURLs {
		episodeURLs = append(episodeURLs, url)
	}
	episodeURLs = append(episodeURLs, fileURLs...)

	if abstractions.IsSet(keys.URLAdd) {
		urls := abstractions.GetStringSlice(keys.URLAdd)
		episodeURLs = append(episodeURLs, urls...)
	}

	// Filter out existing URLs
	newURLs := ignoreDownloadedURLs(episodeURLs, existingURLs)

	if len(newURLs) == 0 {
		logger.Pl.I("No new videos at %s", channelURL)
		return nil, nil
	}
	return newURLs, nil
}

// ignoreDownloadedURLs filters out already downloaded URLs.
func ignoreDownloadedURLs(inputURLs, existingURLs []string) []string {
	var newURLs = make([]string, 0, len(inputURLs))

	for _, url := range inputURLs {
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
	return newURLs
}

// ytDlpURLFetch fetches URLs using yt-dlp.
func ytDlpURLFetch(ctx context.Context, channelName, channelURL string, uniqueEpisodeURLs map[string]struct{}, cookiePath string) (map[string]struct{}, error) {
	if uniqueEpisodeURLs == nil {
		uniqueEpisodeURLs = make(map[string]struct{})
	}

	// Build argument
	args := []string{command.YtDLPFlatPlaylist, command.OutputJSON}

	// Cookies
	if cookiePath != "" {
		args = append(args, command.CookiePath, cookiePath)
	}

	// Create command
	args = append(args, channelURL)
	cmd := exec.CommandContext(ctx, command.YTDLP, args...)

	logger.Pl.I("Executing YTDLP playlist fetch command for channel %q URL %q:\n\n%s\n", channelName, channelURL, cmd.String())

	j, err := cmd.Output()
	if err != nil {
		return uniqueEpisodeURLs, fmt.Errorf("yt-dlp command failed: %w", err)
	}

	logger.Pl.D(5, "Retrieved command output from YTDLP for channel %q:\n\n%s", channelURL, string(j))

	var result ytDlpOutput
	if err := json.Unmarshal(j, &result); err != nil {
		return uniqueEpisodeURLs, err
	}

	for _, entry := range result.Entries {
		uniqueEpisodeURLs[entry.URL] = struct{}{}
		logger.Pl.D(5, "Added entry for channel %q: %q", channelURL, entry)
	}

	return uniqueEpisodeURLs, nil
}

// initializeCollector initializes Colly with any cookies.
func initializeCollector(urlStr string, cm *CookieManager) (c *colly.Collector, err error) {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie jar: %w", err)
	}

	// Set cookies for the domain
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	// Get cookies from auth cache via CookieManager
	if cookies := cm.GetCachedAuthCookies(parsedURL.Host); cookies != nil {
		jar.SetCookies(parsedURL, cookies)
	} else {
		logger.Pl.W("no authentication cookies available for %q", parsedURL.Host)
	}

	// Create a Colly collector with the custom HTTP client
	collector := colly.NewCollector(
		colly.Async(true),
	)
	collector.SetRequestTimeout(60 * time.Second)
	collector.WithTransport(&http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // Adjust if necessary
	})
	collector.SetCookieJar(jar)

	return collector, nil
}

// sanitizeFilename removes illegal characters.
func sanitizeFilename(name string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
}

// setupFieldScraping applies scraping rules for a specific field.
func setupFieldScraping(c *colly.Collector, fieldName string, rules []consts.HTMLMetadataRule, result *string) {
	if result == nil {
		return
	}

	for _, rule := range rules {
		c.OnHTML(rule.Selector, func(h *colly.HTMLElement) {
			if *result != "" {
				return
			}

			var value string
			if rule.Attr != "" {
				value = h.Attr(rule.Attr)
			} else {
				// For description fields without Attr, get HTML content and process it
				if fieldName == sharedtags.JDescription {
					html, err := h.DOM.Html()
					if err == nil {
						// Clean HTML tags and format
						html = strings.ReplaceAll(html, "<br>", "\n")
						html = strings.ReplaceAll(html, "<br/>", "\n")
						html = strings.ReplaceAll(html, "<br />", "\n")
						html = strings.ReplaceAll(html, "&nbsp", "\n")
						html = strings.ReplaceAll(html, " \n", "\n")
						html = strings.ReplaceAll(html, "\n ", "\n")
						html = strings.TrimSpace(html)
						value = html
					} else {
						value = h.Text
					}
				} else {
					value = h.Text
				}
			}

			value = strings.TrimSpace(value)

			if value != "" {
				logger.Pl.S("Grabbed value %q for field %q using selector %q", value, fieldName, rule.Selector)
				*result = value
			}
		})
	}
}

// ScrapeWithRules scrapes metadata using HTMLMetadataQuery rules.
func (s *Scraper) ScrapeWithRules(urlStr string, collector *colly.Collector, v *models.Video, query consts.HTMLMetadataQuery) map[string]any {
	metadata := make(map[string]any)

	var (
		title          string
		description    string
		releaseDate    string
		directVideoURL string
		thumbnailURL   string
	)

	logger.Pl.I("Scraping %q using rules for %s...", urlStr, query.Site)

	// Group rules by field name
	rulesByField := make(map[string][]consts.HTMLMetadataRule)
	for _, rule := range query.Rules {
		rulesByField[rule.Name] = append(rulesByField[rule.Name], rule)
	}

	// Setup scraping for each field
	if rules, ok := rulesByField[sharedtags.JTitle]; ok {
		setupFieldScraping(collector, sharedtags.JTitle, rules, &title)
	}
	if rules, ok := rulesByField[sharedtags.JDescription]; ok {
		setupFieldScraping(collector, sharedtags.JDescription, rules, &description)
	}
	if rules, ok := rulesByField[sharedtags.JReleaseDate]; ok {
		setupFieldScraping(collector, sharedtags.JReleaseDate, rules, &releaseDate)
	}
	if rules, ok := rulesByField[sharedtags.JDirectVideoURL]; ok {
		setupFieldScraping(collector, sharedtags.JDirectVideoURL, rules, &directVideoURL)
	}
	if rules, ok := rulesByField[sharedtags.JThumbnailURL]; ok {
		setupFieldScraping(collector, sharedtags.JThumbnailURL, rules, &thumbnailURL)
	}

	// After scraping completes, populate the Video struct
	collector.OnScraped(func(_ *colly.Response) {
		if title != "" {
			v.Title = title
			metadata[sharedtags.JTitle] = title
		}

		if description != "" {
			v.Description = description
			metadata[sharedtags.JDescription] = description
		}

		if releaseDate != "" {
			// Parse the date
			var t time.Time
			var err error

			// Plain number.
			if _, parseErr := strconv.ParseInt(releaseDate, 10, 64); parseErr == nil { // if err IS nil.
				t, err = time.Parse("2006-01-02", releaseDate)
			}
			// Plain number and hyphens.
			if strings.Contains(releaseDate, "-") {
				num := strings.ReplaceAll(releaseDate, "-", "")
				if _, parseErr := strconv.ParseInt(num, 10, 64); parseErr == nil { // if err IS nil.
					t, err = time.Parse("2006-01-02", releaseDate)
				}
			}
			if err != nil || t.IsZero() {
				if strings.Contains(releaseDate, "T") {
					t, err = time.Parse(time.RFC3339, releaseDate)
				}
			}
			if err != nil || t.IsZero() {
				parsedDate, parseErr := parsing.ParseWordDate(releaseDate)
				if parseErr == nil {
					t, err = time.Parse("2006-01-02", parsedDate)
				}
			}

			releaseDateValue := releaseDate
			if !t.IsZero() && err == nil {
				v.UploadDate = t
				releaseDateValue = t.Format("2006-01-02")
				logger.Pl.I("Extracted upload date %q from metadata", v.UploadDate.String())
			} else {
				logger.Pl.E("Failed to parse upload date %q: %v", releaseDate, err)
			}
			metadata[sharedtags.JReleaseDate] = releaseDateValue
		}

		if directVideoURL != "" {
			v.DirectVideoURL = directVideoURL
			metadata[sharedtags.JDirectVideoURL] = directVideoURL
		}

		if thumbnailURL != "" {
			v.ThumbnailURL = thumbnailURL
			metadata[sharedtags.JThumbnailURL] = thumbnailURL
		}
	})

	return metadata
}
