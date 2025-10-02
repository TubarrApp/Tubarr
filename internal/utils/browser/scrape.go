// Package browser handles operations relating to web scraping, cookie gathering, etc.
package browser

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"tubarr/internal/cfg"
	"tubarr/internal/cfg/validation"
	"tubarr/internal/domain/cmdvideo"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/errconsts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/interfaces"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"

	"github.com/gocolly/colly"
	"golang.org/x/net/publicsuffix"
)

type Browser struct {
	cookies   *CookieManager
	collector *colly.Collector
}

type urlPattern struct {
	name    string
	pattern string
}

type ytDlpOutput struct {
	Entries []struct {
		URL string `json:"url"`
	} `json:"entries"`
}

const (
	bitchute        = "bitchute.com"
	bitchutePattern = "/video/"
	censored        = "censored.tv"
	censoredPattern = "/episodes/"
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

var customAuthCookies = make(map[string][]*http.Cookie)

func NewBrowser() *Browser {
	return &Browser{
		cookies:   NewCookieManager(),
		collector: colly.NewCollector(),
	}
}

// GetExistingReleases returns releases already in the database.
func (b *Browser) GetExistingReleases(cs interfaces.ChannelStore, c *models.Channel) (existingURLsMap map[string]struct{}, existingURLs []string, err error) {
	// Load already downloaded URLs
	existingURLs, err = cs.LoadGrabbedURLs(c)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}

	// Convert existing URLs into a map for quick lookup
	existingMap := make(map[string]struct{}, len(existingURLs))
	for _, url := range existingURLs {
		existingMap[url] = struct{}{}
	}

	logging.D(2, "Loaded %d existing downloaded video URLs for channel %q", len(existingMap), c.Name)
	return existingMap, existingURLs, nil
}

// GetNewReleases checks a channel's URLs for new video URLs that haven't been recorded as downloaded.
func (b *Browser) GetNewReleases(cs interfaces.ChannelStore, c *models.Channel, ctx context.Context) ([]*models.Video, error) {
	if len(c.URLs) == 0 {
		return nil, fmt.Errorf("channel has no URLs (channel ID: %d)", c.ID)
	}

	// Prepare data structures
	var (
		newRequests []*models.Video
	)

	// Fetch entries already in the database
	existingMap, existingURLs, err := b.GetExistingReleases(cs, c)
	if err != nil {
		return nil, err
	}

	// Process each URL separately
	for _, channelURL := range c.URLs {
		logging.D(1, "Processing channel URL %q", channelURL)

		cookies, chanAccessDetails, err := b.GetChannelAccessDetails(cs, c, channelURL)
		if err != nil {
			return nil, err
		}

		// Fetch new episode URLs for this video URL
		newEpisodeURLs, err := b.newEpisodeURLs(channelURL, existingURLs, nil, cookies, chanAccessDetails.CookiePath, ctx)
		if err != nil {
			return nil, err
		}

		// Filter out already downloaded URLs
		for _, newURL := range newEpisodeURLs {
			if _, exists := existingMap[newURL]; !exists {
				newRequests = append(newRequests, &models.Video{
					ChannelID:  c.ID,
					URL:        newURL,
					ChannelURL: channelURL,
					Channel:    c,
					Settings:   c.Settings,
					MetarrArgs: c.MetarrArgs,
					CookiePath: chanAccessDetails.CookiePath,
				})
			}
		}
	}

	// Print summary if new videos were found
	if len(newRequests) > 0 {
		logging.I("Found %d new video URL requests for channel %q:", len(newRequests), c.Name)
		for i, v := range newRequests {
			if v == nil {
				continue
			}
			logging.P("%s#%d%s - %q", consts.ColorBlue, i+1, consts.ColorReset, v.URL)
			if i >= 24 { // 25 (i starts at 0)
				break
			}
		}
	}
	return newRequests, nil
}

// newEpisodeURLs checks for new episode URLs that are not yet in grabbed-urls.txt
func (b *Browser) newEpisodeURLs(targetURL string, existingURLs, fileURLs []string, cookies []*http.Cookie, cookiePath string, ctx context.Context) ([]string, error) {
	uniqueEpisodeURLs := make(map[string]struct{})

	// Set cookies
	if len(cookies) > 0 {
		for _, cookie := range cookies {
			if err := b.collector.SetCookies(targetURL, []*http.Cookie{cookie}); err != nil {
				return nil, err
			}
		}
	}

	// Check if domain matches any custom Tubarr domains
	var customDom bool
	pattern := patterns["default"]
	for domain, p := range patterns {
		if strings.Contains(targetURL, domain) {
			pattern = p
			logging.I("Detected %s link", p.name)
			customDom = true
			break
		}
	}

	b.collector.OnHTML("a[href]", func(e *colly.HTMLElement) {
		link := e.Request.AbsoluteURL(e.Attr("href"))
		if strings.Contains(link, pattern.pattern) {
			uniqueEpisodeURLs[link] = struct{}{}
		}
	})

	if customDom {
		if err := b.collector.Visit(targetURL); err != nil {
			return nil, fmt.Errorf("error visiting webpage (%s): %w", targetURL, err)
		}
		b.collector.Wait()
	} else {
		var err error
		if uniqueEpisodeURLs, err = ytDlpURLFetch(targetURL, uniqueEpisodeURLs, cookiePath, ctx); err != nil {
			return nil, err
		}
	}

	// Collect URLs from all sources (scraped + file)
	episodeURLs := make([]string, 0, len(uniqueEpisodeURLs)+len(fileURLs))
	for url := range uniqueEpisodeURLs {
		episodeURLs = append(episodeURLs, url)
	}
	episodeURLs = append(episodeURLs, fileURLs...)

	if cfg.IsSet(keys.URLAdd) {
		urls := cfg.GetStringSlice(keys.URLAdd)
		episodeURLs = append(episodeURLs, urls...)
	}

	// Filter out existing URLs
	newURLs := ignoreDownloadedURLs(episodeURLs, existingURLs)

	if len(newURLs) == 0 {
		logging.I("No new videos at %s", targetURL)
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

// normalizeURL standardizes URLs for comparison by removing protocol and any trailing slashes.
//
// Do NOT add a "ToLower" function as some sites like YouTube have case-sensitive URLs.
func normalizeURL(inputURL string) string {
	cleanURL := strings.TrimPrefix(inputURL, "https://")
	cleanURL = strings.TrimPrefix(cleanURL, "http://")
	return strings.TrimSuffix(cleanURL, "/")
}

// ytDlpURLFetch fetches URLs using yt-dlp.
func ytDlpURLFetch(chanURL string, uniqueEpisodeURLs map[string]struct{}, cookiePath string, ctx context.Context) (map[string]struct{}, error) {
	if uniqueEpisodeURLs == nil {
		uniqueEpisodeURLs = make(map[string]struct{})
	}

	cmd := exec.CommandContext(ctx, cmdvideo.YTDLP, consts.YtDLPFlatPlaylist, consts.YtDLPOutputJSON, chanURL)

	if cookiePath != "" {
		cmd.Args = append(cmd.Args, consts.Cookies, cookiePath)
	}
	logging.D(2, "Executing YTDLP command for channel %q:\n%s", chanURL, cmd.String())

	j, err := cmd.Output()
	if err != nil {
		return uniqueEpisodeURLs, fmt.Errorf(errconsts.YTDLPFailure, err)
	}

	logging.D(5, "Retrieved command output from YTDLP for channel %q:\n\n%s", chanURL, string(j))

	var result ytDlpOutput
	if err := json.Unmarshal(j, &result); err != nil {
		return uniqueEpisodeURLs, err
	}

	for _, entry := range result.Entries {
		uniqueEpisodeURLs[entry.URL] = struct{}{}
		logging.D(5, "Added entry for channel %q: %q", chanURL, entry)
	}

	return uniqueEpisodeURLs, nil
}

// ScrapeCensoredTVMetadata scrapes Censored.TV links for metadata.
func (b *Browser) ScrapeCensoredTVMetadata(urlStr, outputDir string, v *models.Video) error {
	// Create a custom cookie jar to hold cookies
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return fmt.Errorf("failed to create cookie jar: %w", err)
	}

	// Set cookies for the domain
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if cookies, ok := customAuthCookies[parsedURL.Host]; ok {
		jar.SetCookies(parsedURL, cookies)
	} else {
		return fmt.Errorf("no authentication cookies available for %s", parsedURL.Host)
	}

	// Create a Colly collector with the custom HTTP client
	collector := colly.NewCollector(
		colly.Async(true),
	)

	collector.WithTransport(&http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // Adjust if necessary
	})
	collector.SetCookieJar(jar)

	// Metadata to populate
	metadata := make(map[string]interface{})

	logging.I("Scraping %s for metadata...", urlStr)

	collector.OnHTML("html", func(container *colly.HTMLElement) {
		// Use .Find for all deep queries
		doc := container.DOM

		// Title
		title := strings.TrimSpace(doc.Find("#episode-container .episode-title").Text())
		if title != "" {
			logging.D(2, "Scraped title: %s", title)
			metadata["title"] = title
			v.Title = title
		} else {
			logging.D(1, "Title not found")
		}

		// Description
		description, err := doc.Find("#about .raised-content").Html()
		if err != nil {
			logging.E(0, "Failed to grab description: %v", err.Error())
			return
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
			metadata["description"] = description
			v.Description = description
		} else {
			logging.D(1, "Description not found")
		}

		// Release date
		date := strings.TrimSpace(doc.Find("#about time").Text())
		if date != "" {
			parsedDate, err := parsing.ParseWordDate(date)
			if err != nil {
				logging.E(0, err.Error())
			}
			logging.D(2, "Scraped release date: %s", date)
			metadata["release_date"] = parsedDate
		} else {
			logging.D(1, "Release date not found")
		}

		// Video URL
		videoURL, ok := doc.Find("a.dropdown-item[href$='.mp4']").Attr("href")
		if ok {
			logging.D(2, "Scraped video URL: %s", videoURL)
			metadata["direct_video_url"] = videoURL
			v.DirectVideoURL = videoURL
		} else {
			logging.D(1, "Video URL not found")
		}
	})

	// Visit the webpage
	if err := collector.Visit(urlStr); err != nil {
		return fmt.Errorf("failed to visit URL: %w", err)
	}

	collector.Wait()

	// Ensure required fields are populated
	if metadata["title"] == nil || metadata["direct_video_url"] == nil {
		logging.D(1, "Scraped metadata: %+v", metadata)
		return fmt.Errorf("missing required metadata fields (title [got: %s] or video URL [got: %v])", v.Title, metadata["direct_video_url"])
	}

	// Generate filename from title
	filename := fmt.Sprintf("%s.json", sanitizeFilename(metadata["title"].(string)))

	// Write metadata to JSON
	if err := writeMetadataJSON(metadata, outputDir, filename, v); err != nil {
		return fmt.Errorf("failed to write metadata JSON: %w", err)
	}

	logging.S(0, "Successfully wrote metadata JSON to %s/%s", outputDir, filename)
	return nil
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

// writeMetadataJSON writes the custom metadata file.
func writeMetadataJSON(metadata map[string]interface{}, outputDir, filename string, v *models.Video) error {
	filePath := fmt.Sprintf("%s/%s", strings.TrimRight(outputDir, "/"), filename)

	// Ensure the directory exists
	if _, err := validation.ValidateDirectory(outputDir, true); err != nil {
		return err
	}

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			logging.E(0, "failed to close file %v due to error: %v", filePath, err)
		}
	}()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(metadata); err != nil {
		return fmt.Errorf("failed to write JSON: %w", err)
	}

	v.JSONCustomFile = filePath
	return nil
}

// generateCookieFilePath generates a unique authentication file path per channel and URL.
func generateCookieFilePath(channelName, videoURL string) string {
	const tubarrDir = ".tubarr/"
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "/"
	}

	// If no specific URL is provided, return the default per-channel auth file.
	if videoURL == "" {
		return filepath.Join(homeDir, tubarrDir, strings.ReplaceAll(channelName, " ", "-")+".txt")
	}

	// Generate a short hash for the URL to ensure uniqueness
	urlHash := sha256.Sum256([]byte(videoURL))
	hashString := fmt.Sprintf("%x", urlHash[:8]) // Use the first 8 hex characters

	// Construct file path (e.g., ~/.tubarr/CensoredTV_Show_a1b2c3d4.txt)
	return filepath.Join(homeDir, tubarrDir, fmt.Sprintf("%s_%s.txt", strings.ReplaceAll(channelName, " ", "-"), hashString))
}
