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

// GetNewReleases checks a channel's URLs for new video URLs that haven't been recorded as downloaded.
func (b *Browser) GetNewReleases(cs interfaces.ChannelStore, c *models.Channel, ctx context.Context) ([]*models.Video, error) {
	if len(c.URLs) == 0 {
		return nil, fmt.Errorf("channel has no URLs (channel ID: %d)", c.ID)
	}

	// Load already downloaded URLs
	existingURLs, err := cs.LoadGrabbedURLs(c)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	// Convert existing URLs into a map for quick lookup
	existingMap := make(map[string]struct{}, len(existingURLs))
	for _, url := range existingURLs {
		existingMap[url] = struct{}{}
	}

	logging.D(2, "Loaded %d existing downloaded video URLs for channel %q", len(existingMap), c.Name)

	// Prepare data structures
	var (
		newRequests []*models.Video
	)

	// Process each URL separately
	for _, videoURL := range c.URLs {
		parsed, err := url.Parse(videoURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse URL %q: %w", videoURL, err)
		}

		domain := parsed.Hostname()
		protocol := parsed.Scheme
		baseDomainWithProto := protocol + "://" + domain

		logging.D(1, "Processing BaseDomain %q for URL %q", baseDomainWithProto, videoURL)

		// Retrieve authentication details for this specific video URL
		username, password, loginURL, err := cs.GetAuth(c.ID, videoURL)
		if err != nil {
			logging.E(0, "Error getting authentication for channel ID %d, URL %q: %v", c.ID, videoURL, err)
		}

		var (
			authCookies, regCookies, cookies []*http.Cookie
			cookiePath                       string
		)

		// If authorization details exist, perform login and store cookies.
		if (username != "" || password != "") && loginURL != "" {
			cookiePath = getAuthFilePath(c.Name, videoURL)

			authDetails := &models.ChanURLAuthDetails{
				Username:   username,
				Password:   password,
				LoginURL:   loginURL,
				CookiePath: cookiePath,
			}

			// Generate a unique cookie path per URL
			authCookies, err = channelAuth(domain, cookiePath, authDetails)
			if err != nil {
				logging.E(0, "Failed to get auth cookies for %q: %v", videoURL, err)
			}
		}

		// Get cookies globally
		if c.Settings.UseGlobalCookies {
			regCookies, err = b.cookies.GetCookies(videoURL)
			if err != nil {
				logging.E(0, "Failed to get cookies for %q with cookie source %q: %v", videoURL, c.Settings.CookieSource, err)
			}
		}

		// Combine cookies
		cookies = append(authCookies, regCookies...)

		// Fetch new episode URLs for this video URL
		newEpisodeURLs, err := b.newEpisodeURLs(videoURL, existingURLs, nil, cookies, ctx)
		if err != nil {
			return nil, err
		}

		// Filter out already downloaded URLs
		for _, newURL := range newEpisodeURLs {
			if _, exists := existingMap[newURL]; !exists {
				newRequests = append(newRequests, &models.Video{
					ChannelID:  c.ID,
					URL:        newURL,
					VideoDir:   c.VideoDir,
					JSONDir:    c.JSONDir,
					Channel:    c,
					Settings:   c.Settings,
					MetarrArgs: c.MetarrArgs,
					CookiePath: cookiePath,
				})
			}
		}
	}

	logging.D(2, "New download requests for channel %q: %v", c.Name, newRequests)

	// Print summary if new videos were found
	if len(newRequests) > 0 {
		logging.I("Found %d new download requests for channel %q:", len(newRequests), c.Name)
		for i, v := range newRequests {
			logging.P("%s#%d%s - %v", consts.ColorBlue, i+1, consts.ColorReset, v.URL)
			if i >= 25 {
				break
			}
		}
	}
	return newRequests, nil
}

// newEpisodeURLs checks for new episode URLs that are not yet in grabbed-urls.txt
func (b *Browser) newEpisodeURLs(targetURL string, existingURLs, fileURLs []string, cookies []*http.Cookie, ctx context.Context) ([]string, error) {
	uniqueEpisodeURLs := make(map[string]struct{})

	// Set cookies
	if len(cookies) > 0 {
		for _, cookie := range cookies {
			if err := b.collector.SetCookies(targetURL, []*http.Cookie{cookie}); err != nil {
				return nil, err
			}
		}
	}

	// Only scrape website if we're not using a URL file
	var customDom bool
	if !cfg.IsSet(keys.URLFile) {
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
	}

	if customDom {
		if err := b.collector.Visit(targetURL); err != nil {
			return nil, fmt.Errorf("error visiting webpage (%s): %w", targetURL, err)
		}
		b.collector.Wait()
	} else {
		var err error
		if uniqueEpisodeURLs, err = ytDlpURLFetch(targetURL, uniqueEpisodeURLs, ctx); err != nil {
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
func ytDlpURLFetch(chanURL string, uniqueEpisodeURLs map[string]struct{}, ctx context.Context) (map[string]struct{}, error) {
	if uniqueEpisodeURLs == nil {
		uniqueEpisodeURLs = make(map[string]struct{})
	}

	cmd := exec.CommandContext(ctx, cmdvideo.YTDLP, consts.YtDLPFlatPlaylist, consts.YtDLPOutputJSON, chanURL)
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

	// Load cookies from the authenticated session
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

// Extracts the hostname from a URL.
// func urlHostname(rawURL string) string {
// 	parsed, err := url.Parse(rawURL)
// 	if err != nil {
// 		return ""
// 	}
// 	return parsed.Hostname()
// }

// getAuthFilePath generates a unique authentication file path per channel and URL.
func getAuthFilePath(channelName, videoURL string) string {
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
