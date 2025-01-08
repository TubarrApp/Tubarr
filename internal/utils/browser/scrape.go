// Package browser handles operations relating to web scraping, cookie gathering, etc.
package browser

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
	"os"
	"os/exec"
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

var customAuthCookies = make(map[string][]*http.Cookie)

func NewBrowser() *Browser {
	return &Browser{
		cookies:   NewCookieManager(),
		collector: colly.NewCollector(),
	}
}

// GetNewReleases checks a channel URL for URLs which have not yet been recorded as downloaded.
func (b *Browser) GetNewReleases(cs interfaces.ChannelStore, c *models.Channel, ctx context.Context) ([]*models.Video, error) {
	if c.URL == "" {
		return nil, fmt.Errorf("channel url is blank (channel ID: %d)", c.ID)
	}

	existingURLs, err := cs.LoadGrabbedURLs(c)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	existingMap := make(map[string]struct{}, len(existingURLs))
	for _, url := range existingURLs {
		existingMap[url] = struct{}{}
	}

	if len(existingMap) > 0 {
		logging.I("Found %d existing downloaded video URLs:", len(existingMap))
	}

	var (
		cookies []*http.Cookie
		sb      strings.Builder
	)
	const (
		protoExt = "://"
	)

	if c.BaseDomain == "" {
		logging.D(1, "Parsing URL %q", c.URL)
		parsed, err := url.Parse(c.URL)
		if err != nil {
			return nil, err
		}
		c.BaseDomain = parsed.Hostname()
		parsedScheme := parsed.Scheme

		sb.Reset()
		sb.Grow(len(parsedScheme) + len(protoExt) + len(c.BaseDomain))
		sb.WriteString(parsedScheme)
		sb.WriteString(protoExt)
		sb.WriteString(c.BaseDomain)
		c.BaseDomainWithProto = sb.String()
		logging.D(1, "Saved channel domain %q\nChannel domain with protocol %q", c.BaseDomain, c.BaseDomainWithProto)
	}

	if customAuthCookies[c.BaseDomain] == nil {
		const (
			tubarrDir = ".tubarr/"
			txtExt    = ".txt"
		)

		homeDir, err := os.UserHomeDir()
		if err != nil {
			logging.E(0, "Failed to get user home directory, reverting to '/': %v", err)
			homeDir = "/"
		}

		sb.Reset()
		sb.Grow(len(homeDir) + 1 + len(tubarrDir) + len(c.Name) + len(txtExt))
		sb.WriteString(homeDir)
		if !strings.HasSuffix(c.VideoDir, "/") {
			sb.WriteRune('/')
		}
		sb.WriteString(tubarrDir)

		noSpaceChanName := strings.ReplaceAll(c.Name, " ", "-")
		sb.WriteString(noSpaceChanName)
		sb.WriteString(txtExt)

		if (c.Username != "" || c.Password != "") && c.LoginURL != "" {
			cookies, err = channelAuth(c.BaseDomain, sb.String(), c)
			if err != nil {
				return nil, err
			}
			customAuthCookies[c.BaseDomain] = cookies
			logging.D(2, "Set %d cookies for domain %q: %v", len(cookies), c.BaseDomain, cookies)
		}
	} else {
		cookies = customAuthCookies[c.BaseDomain]
		logging.D(2, "Retrieved %d cookies for domain %q: %v", len(cookies), c.BaseDomain, cookies)
	}

	if cookies == nil {
		cookies, err = b.cookies.GetCookies(c.URL)
		if err != nil {
			return nil, err
		}
	}

	var fileURLs []string
	if cfg.IsSet(keys.URLFile) {
		prs := parsing.NewURLFileParser(cfg.GetString(keys.URLFile))
		if fileURLs, err = prs.ParseURLs(); err != nil {
			return nil, err
		}
	}

	newURLs, err := b.newEpisodeURLs(c.URL, existingURLs, fileURLs, cookies, ctx)
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
					CookiePath: c.CookiePath,
				})
			}
		}
	}

	if len(newRequests) > 0 {
		logging.I("Grabbed %d new download requests:", len(newRequests))
		for i, v := range newRequests {
			i++
			logging.P("%s#%d%s - %v", consts.ColorBlue, i, consts.ColorReset, v.URL)
			if i > 25 {
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

	// Scope scraping to the main container
	collector.OnHTML(".main-episode-player-container", func(container *colly.HTMLElement) {
		// Scrape the title
		title := strings.TrimSpace(container.ChildText("h4"))
		if title != "" {
			logging.D(2, "Scraped title: %s", title)
			metadata["title"] = title
			v.Title = title
		} else {
			logging.D(1, "Title not found in the container")
		}

		// Scrape the description
		description := strings.TrimSpace(container.ChildText("p#check-for-urls"))
		if description != "" {
			logging.D(2, "Scraped description: %s", description)
			metadata["description"] = description
			v.Description = description
		} else {
			logging.D(1, "Description not found in the container")
		}

		// Scrape the release date
		date := strings.TrimSpace(container.ChildText("p.text-muted.text-right.text-date.mb-0"))
		if date != "" {
			parsedDate, err := parsing.ParseWordDate(date)
			if err != nil {
				logging.E(0, err.Error())
			}
			logging.D(2, "Scraped release date: %s", date)
			metadata["release_date"] = parsedDate
		} else {
			logging.D(1, "Release date not found in the container")
		}

		// Scrape the video URL
		videoURL := container.ChildAttr("a[href$='.mp4']", "href")
		if videoURL != "" {
			logging.D(2, "Scraped video URL: %s", videoURL)
			metadata["direct_video_url"] = videoURL
			v.DirectVideoURL = videoURL
		} else {
			logging.D(1, "Video URL not found in the container")
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
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(metadata); err != nil {
		return fmt.Errorf("failed to write JSON: %w", err)
	}

	v.JSONCustomFile = filePath
	return nil
}
