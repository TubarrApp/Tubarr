// Package scraper handles web scraping operations.
package scraper

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os/exec"
	"strings"
	"time"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/command"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly"
	"github.com/spf13/viper"
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
	existingURLs, err = cs.GetAlreadyDownloadedURLs(c)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}

	existingMap := make(map[string]struct{}, len(existingURLs))
	for _, url := range existingURLs {
		existingMap[url] = struct{}{}
	}

	logging.D(2, "Loaded %d existing downloaded video URLs for channel %q", len(existingMap), c.Name)
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
		logging.D(1, "Processing channel URL %q", cu.URL)

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
		logging.I("Found %d new video URL requests for channel %q:", len(newRequests), c.Name)
		for i, v := range newRequests {
			if v == nil {
				continue
			}
			logging.P("%s#%d%s - %q", consts.ColorBlue, i+1, consts.ColorReset, v.URL)
			if i >= consts.MaxDisplayedVideos {
				break
			}
		}
	}

	return newRequests, nil
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
			logging.I("Detected %s link", p.name)
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

	if viper.IsSet(keys.URLAdd) {
		urls := viper.GetStringSlice(keys.URLAdd)
		episodeURLs = append(episodeURLs, urls...)
	}

	// Filter out existing URLs
	newURLs := ignoreDownloadedURLs(episodeURLs, existingURLs)

	if len(newURLs) == 0 {
		logging.I("No new videos at %s", channelURL)
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

	// Sleep
	args = append(args, command.RandomizeRequests...)

	// Create command
	args = append(args, channelURL)
	cmd := exec.CommandContext(ctx, command.YTDLP, args...)

	logging.I("Executing YTDLP playlist fetch command for channel %q URL %q:\n\n%s\n", channelName, channelURL, cmd.String())

	j, err := cmd.Output()
	if err != nil {
		return uniqueEpisodeURLs, fmt.Errorf("yt-dlp command failed: %w", err)
	}

	logging.D(5, "Retrieved command output from YTDLP for channel %q:\n\n%s", channelURL, string(j))

	var result ytDlpOutput
	if err := json.Unmarshal(j, &result); err != nil {
		return uniqueEpisodeURLs, err
	}

	for _, entry := range result.Entries {
		uniqueEpisodeURLs[entry.URL] = struct{}{}
		logging.D(5, "Added entry for channel %q: %q", channelURL, entry)
	}

	return uniqueEpisodeURLs, nil
}

// ScrapeCensoredTVMetadata scrapes Censored.TV links for metadata.
func (s *Scraper) ScrapeCensoredTVMetadata(urlStr, outputDir string, v *models.Video) error {
	// Initialize collector with cookies
	collector, err := initializeCollector(urlStr, s.cookieManager)
	if err != nil {
		return err
	}

	// Metadata to populate
	metadata := make(map[string]any)

	logging.I("Scraping %q for metadata...", urlStr)

	collector.OnHTML("html", func(container *colly.HTMLElement) {
		doc := container.DOM

		// Extract all metadata
		v.Title = extractTitle(consts.HTMLCensoredTitle, doc)
		metadata[consts.MetadataTitle] = v.Title

		v.Description = extractDescription(consts.HTMLCensoredDesc, doc)
		metadata[consts.MetadataDesc] = v.Description

		metadata[consts.MetadataDate] = extractDate(consts.HTMLCensoredDate, doc)

		v.DirectVideoURL = extractVideoURL(consts.HTMLCensoredVideoURL, doc)
		metadata[consts.MetadataVideoURL] = v.DirectVideoURL
	})

	// Visit the webpage
	if err := collector.Visit(urlStr); err != nil {
		return fmt.Errorf("failed to visit URL: %w", err)
	}
	collector.Wait()

	// Validate required fields
	if v.Title == "" || v.DirectVideoURL == "" {
		logging.D(1, "Scraped metadata: %+v", metadata)
		return fmt.Errorf("missing required metadata fields (title: %q, video URL: %q)", v.Title, v.DirectVideoURL)
	}

	// Write metadata to file
	filename := fmt.Sprintf("%s.json", sanitizeFilename(v.Title))
	if err := file.WriteMetadataJSONFile(metadata, filename, outputDir, v); err != nil {
		return fmt.Errorf("failed to write metadata JSON: %w", err)
	}

	logging.S("Successfully wrote metadata JSON to %s/%s", outputDir, filename)
	return nil
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
		logging.W("no authentication cookies available for %q", parsedURL.Host)
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

// extractTitle grabs the title from the webpage.
func extractTitle(findStr string, doc *goquery.Selection) string {
	title := strings.TrimSpace(doc.Find(findStr).Text())
	if title != "" {
		logging.D(2, "Scraped title: %s", title)
	} else {
		logging.D(1, "Title not found")
	}
	return title
}

// extractDescription grabs the description from the webpage.
func extractDescription(findStr string, doc *goquery.Selection) string {
	description, err := doc.Find(findStr).Html()
	if err != nil {
		logging.E("Failed to grab description: %v", err.Error())
		return ""
	}

	// Clean newline tags
	description = strings.ReplaceAll(description, "<br>", "\n")
	description = strings.ReplaceAll(description, "<br/>", "\n")
	description = strings.ReplaceAll(description, "<br />", "\n")
	description = strings.ReplaceAll(description, "&nbsp", "\n")

	// Fix newline quirks
	description = strings.ReplaceAll(description, " \n", "\n")
	description = strings.ReplaceAll(description, "\n ", "\n")

	// Trim space
	description = strings.TrimSpace(description)

	// Unescape special characters
	description = html.UnescapeString(description)

	if description != "" {
		logging.D(2, "Scraped description: %s", description)
	} else {
		logging.D(1, "Description not found")
	}

	return description
}

// extractDate pulls the video release date from metadata.
func extractDate(findStr string, doc *goquery.Selection) string {
	var (
		parsedDate string
		err        error
	)
	date := strings.TrimSpace(doc.Find(findStr).Text())
	if date != "" {
		parsedDate, err = parsing.ParseWordDate(date)
		if err != nil {
			logging.E(err.Error())
		}
		logging.D(2, "Scraped release date: %s", date)
	} else {
		logging.D(1, "Release date not found")
	}
	return strings.TrimSpace(parsedDate)
}

// extractVideoURL extracts the video URL from the webpage.
func extractVideoURL(findStr string, doc *goquery.Selection) string {
	videoURL, ok := doc.Find(findStr).Attr("href")
	if ok {
		logging.D(2, "Scraped video URL: %s", videoURL)
	} else {
		logging.D(1, "Video URL not found")
	}
	return videoURL
}
