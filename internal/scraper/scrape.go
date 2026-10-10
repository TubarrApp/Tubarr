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
	"strconv"
	"strings"
	"time"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/command"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/siterules"

	"github.com/TubarrApp/gocommon/abstractions"
	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/sharedtags"

	"github.com/gocolly/colly"
	"golang.org/x/net/publicsuffix"
)

// Scraper handles web scraping operations.
type Scraper struct {
	cookieManager *CookieManager
}

// New returns a new Scraper instance.
func New() *Scraper {
	return &Scraper{
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
func (s *Scraper) GetNewReleases(ctx context.Context, cs contracts.ChannelStore, c *models.Channel, ignoreCrawl bool) ([]*models.Video, error) {
	if len(c.URLModels) == 0 {
		logger.Pl.D(1, "Channel %q has no URLs configured, skipping automatic crawl", c.Name)
		return []*models.Video{}, nil
	}

	existingMap, existingURLs, err := s.GetExistingReleases(cs, c)
	if err != nil {
		return nil, err
	}

	var newRequests []*models.Video

	// Process each ChannelURL
	for _, cu := range c.URLModels {
		if cu.IsManual || (!ignoreCrawl && (cu.ChanURLSettings != nil && cu.ChanURLSettings.Paused)) {
			continue
		}
		logger.Pl.D(1, "Processing channel URL %q", cu.URL)

		// Get access details once per ChannelURL - delegated to CookieManager
		cu.Cookies, cu.CookiePath, err = s.cookieManager.GetChannelURLCookies(ctx, cs, c, cu)
		if err != nil {
			return nil, err
		}

		// Fetch new episode URLs
		var chanCrawlArgs string
		if cu.ChanURLSettings != nil {
			chanCrawlArgs = cu.ChanURLSettings.ExtraYTDLPCrawlArgs
		}
		newEpisodeURLs, err := s.newEpisodeURLs(ctx, c.Name, cu.URL, existingURLs, nil, cu.Cookies, cu.CookiePath, chanCrawlArgs, cu.FlareSolverr)
		if err != nil {
			return nil, err
		}

		// Filter and create video requests
		for _, newURL := range newEpisodeURLs {
			if _, exists := existingMap[newURL]; !exists {
				video := &models.Video{
					ChannelID:    c.ID,
					ChannelURLID: cu.ID,
					ChannelURL:   cu.URL,
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
	// Find a registered rule set (built-in or user-supplied) based on URL domain.
	query, ok := siterules.MatchMetadata(v.URL)
	if !ok {
		logger.Pl.D(1, "No custom scraping rules found for URL: %s - will use yt-dlp", v.URL)
		return nil // Not a custom site.
	}

	// Visit the webpage.
	var metadata map[string]any
	if err := s.visitPage(urlStr, nil, query, func(c *colly.Collector) {
		metadata = s.ScrapeWithRules(urlStr, c, v, query)
	}); err != nil {
		return fmt.Errorf("failed to visit URL: %w", err)
	}

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

// TestScrapeSite runs the registered custom scraping rules (if any) against a URL and returns
// the discovered metadata.
//
// Used by the web UI to preview whether a custom scraper matches a given URL and retrieves the correct data.
func TestScrapeSite(urlStr string) (matched bool, site string, metadata map[string]any, err error) {
	query, ok := siterules.MatchMetadata(urlStr)
	if !ok {
		return false, "", nil, nil
	}

	s := New()
	v := &models.Video{URL: urlStr}
	if err := s.visitPage(urlStr, nil, query, func(c *colly.Collector) {
		metadata = s.ScrapeWithRules(urlStr, c, v, query)
	}); err != nil {
		return true, query.Site, nil, fmt.Errorf("failed to visit URL: %w", err)
	}

	return true, query.Site, metadata, nil
}

// ScraperURLCookies returns channel access details for a given video.
func (s *Scraper) ScraperURLCookies(ctx context.Context, cs contracts.ChannelStore, c *models.Channel, cu *models.ChannelURL) (cookies []*http.Cookie, cookieFilePath string, err error) {
	return s.cookieManager.GetChannelURLCookies(ctx, cs, c, cu)
}

// newEpisodeURLs checks for new episode URLs that are not yet in grabbed-urls.txt
func (s *Scraper) newEpisodeURLs(
	ctx context.Context,
	channelName, channelURL string,
	existingURLs, fileURLs []string,
	cookies []*http.Cookie, cookiePath string,
	crawlArgs string,
	fs *models.FlareSolverrSolution) ([]string, error) {
	// Custom crawl rules and built-in scrapers take priority over yt-dlp.
	_, uniqueEpisodeURLs, crawled, err := s.crawlChannelURLs(channelURL, cookies)
	if err != nil {
		return nil, err
	}
	if !crawled {
		if uniqueEpisodeURLs, err = ytDlpURLFetch(ctx, channelName, channelURL, nil, cookiePath, crawlArgs, fs); err != nil {
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
func ytDlpURLFetch(ctx context.Context, channelName, channelURL string, uniqueEpisodeURLs map[string]struct{}, cookiePath string, crawlArgs string, fs *models.FlareSolverrSolution) (map[string]struct{}, error) {
	if uniqueEpisodeURLs == nil {
		uniqueEpisodeURLs = make(map[string]struct{})
	}

	// Run yt-dlp, solving through FlareSolverr again and retrying once if Cloudflare blocks a FlareSolverr site.
	var j []byte
	for retried := false; ; retried = true {
		// Build argument
		args := []string{command.YtDLPFlatPlaylist}

		// Add custom crawl arguments: CLI flag takes precedence over per-channel setting.
		if abstractions.IsSet(keys.ExtraYTDLPCrawlArgs) {
			args = append(args, strings.Fields(abstractions.GetString(keys.ExtraYTDLPCrawlArgs))...)
		} else if crawlArgs != "" {
			args = append(args, strings.Fields(crawlArgs)...)
		}

		// Cookies
		if cookiePath != "" {
			args = append(args, command.CookiePath, cookiePath)
		}

		// Site rule impersonation, and FlareSolverr's user agent (its cookies are in the cookie file).
		args, fsGen := siterules.YtDLPArgs(channelURL, fs, args)

		// Add -J and URL to finalize command
		args = append(args, command.OutputJSON, channelURL)
		cmd := exec.CommandContext(ctx, command.YTDLP, args...)

		logger.Pl.I("Executing YTDLP playlist fetch command for channel %q URL %q:\n\n%s\n", channelName, channelURL, cmd.String())

		var err error
		if j, err = cmd.Output(); err == nil {
			break
		}

		var stderr string
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr = strings.TrimSpace(string(exitErr.Stderr))
		}
		if fs == nil || retried || !models.IsCloudflareBlock(stderr) {
			return uniqueEpisodeURLs, fmt.Errorf("yt-dlp command failed: %w: %s", err, stderr)
		}

		logger.Pl.W("Cloudflare blocked yt-dlp for %q, solving through FlareSolverr again...", channelURL)
		if err := fs.Refresh(fsGen); err != nil {
			return uniqueEpisodeURLs, fmt.Errorf("yt-dlp was blocked by Cloudflare (%s), and a fresh FlareSolverr solve failed: %w", stderr, err)
		}
	}

	logger.Pl.D(5, "Retrieved command output from YTDLP for channel %q:\n\n%s", channelURL, string(j))

	var result ytDlpOutput
	if err := json.Unmarshal(j, &result); err != nil {
		return uniqueEpisodeURLs, err
	}

	// Retrieve base domain for comparisons.
	chanBaseDomain, _ := getBaseDomain(channelURL)

	// Process entries and add to map.
	for _, entry := range result.Entries {
		// Filter out the channel URL itself if it appears in the entries list.
		if normalizeURL(entry.URL) == normalizeURL(channelURL) {
			logger.Pl.D(3, "Skipping entry (entry %q is the exact channel URL: %q)", entry.URL, channelURL)
			continue
		}
		// Normalize Rumble URLs.
		if chanBaseDomain == rumble {
			if isValidRumbleVideoURL(entry.URL) {
				entry.URL = removeQueryParams(entry.URL)
			} else {
				logger.Pl.D(2, "Rumble URL %q is not a valid video link, skipping...", entry.URL)
				continue
			}
		}
		// Add to map to ensure uniqueness.
		uniqueEpisodeURLs[entry.URL] = struct{}{}
		logger.Pl.D(3, "Added entry for channel %q: %q", channelURL, entry)
	}

	return uniqueEpisodeURLs, nil
}

// visitPage visits urlStr with a collector for the site, after setup registers its callbacks on it.
//
// For FlareSolverr sites, requests use the solved cookies and user agent, and a Cloudflare challenge
// triggers one fresh FlareSolverr solve and retry. If FlareSolverr is unavailable, falls back to the
// site's normal requests.
func (s *Scraper) visitPage(urlStr string, cookies []*http.Cookie, query models.SiteRules, setup func(c *colly.Collector)) error {
	var (
		sol   *flareSolverrSolution
		fsErr error
	)
	if query.FlareSolverr {
		sol, fsErr = s.cookieManager.flareSolverrSolution(context.Background(), urlStr, 0)
	}

	for retried := false; ; retried = true {
		challenged, err := s.visitPageOnce(urlStr, cookies, query, sol, setup)
		switch {
		case !challenged:
			return err
		case fsErr != nil:
			return fmt.Errorf("cloudflare challenged the request for %q, and FlareSolverr could not be used: %w", urlStr, fsErr)
		case sol == nil:
			return fmt.Errorf("cloudflare challenged the request for %q (try setting flaresolverr = true for %q)", urlStr, query.Site)
		case retried:
			return fmt.Errorf("cloudflare challenged the request for %q again after a fresh FlareSolverr solve", urlStr)
		}

		logger.Pl.W("Cloudflare challenged the request for %q, solving through FlareSolverr again...", urlStr)
		if sol, err = s.cookieManager.flareSolverrSolution(context.Background(), urlStr, sol.gen); err != nil {
			return fmt.Errorf("cloudflare challenged the request for %q, and a fresh FlareSolverr solve failed: %w", urlStr, err)
		}
	}
}

// visitPageOnce makes a single visitPage attempt, reporting whether Cloudflare challenged it.
func (s *Scraper) visitPageOnce(urlStr string, cookies []*http.Cookie, query models.SiteRules, sol *flareSolverrSolution, setup func(c *colly.Collector)) (challenged bool, err error) {
	c, err := initializeCollector(urlStr, s.cookieManager, query, sol)
	if err != nil {
		return false, err
	}
	if len(cookies) > 0 {
		if err := c.SetCookies(urlStr, cookies); err != nil {
			return false, fmt.Errorf("failed to set cookies for %q: %w", urlStr, err)
		}
	}

	var reqErr error
	c.OnResponse(func(r *colly.Response) {
		if isCloudflareChallenge(r.StatusCode, *r.Headers, r.Body) {
			challenged = true
			logCloudflareChallenge(urlStr, r)
		}
	})
	c.OnError(func(r *colly.Response, err error) {
		if r.Headers != nil && isCloudflareChallenge(r.StatusCode, *r.Headers, r.Body) {
			challenged = true
			logCloudflareChallenge(urlStr, r)
		}
		reqErr = fmt.Errorf("request for %q failed (HTTP %d): %w", urlStr, r.StatusCode, err)
	})
	setup(c)

	if err := c.Visit(urlStr); err != nil {
		return challenged, fmt.Errorf("error visiting %q: %w", urlStr, err)
	}
	c.Wait()
	return challenged, reqErr
}

// initializeCollector initializes Colly with any cookies, using a TLS-impersonating transport and user agent if the site
// sets them.
//
// For FlareSolverr sites (sol set), impersonates Chrome with the solution's cookies and user agent.
func initializeCollector(urlStr string, cm *CookieManager, query models.SiteRules, sol *flareSolverrSolution) (c *colly.Collector, err error) {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie jar: %w", err)
	}

	// Set cookies for the domain from this crawl session.
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	// Get cookies from cookie manager for this crawl session.
	if cookies := cm.GetCachedCookies(parsedURL.Hostname()); cookies != nil {
		jar.SetCookies(parsedURL, cookies)
		logger.Pl.D(2, "Set %d cookies for scraper from this crawl session", len(cookies))
	} else {
		logger.Pl.D(2, "No cookies available for scraper for hostname %q", parsedURL.Hostname())
	}

	// Create a Colly collector with the custom HTTP client
	collector := colly.NewCollector(
		colly.Async(true),
	)
	collector.SetRequestTimeout(60 * time.Second)
	switch impersonate := consts.Impersonate(query.Impersonate); {
	case sol != nil:
		rt, userAgent, err := newTLSRoundTripper(consts.ImpersonateChrome, sol.userAgent)
		if err != nil {
			return nil, err
		}
		rt.headers = sol.headers
		collector.WithTransport(rt)
		collector.UserAgent = userAgent
		jar.SetCookies(parsedURL, sol.cookies) // Set last, to replace any stale Cloudflare cookies.
	case impersonate != consts.ImpersonateNone:
		rt, userAgent, err := newTLSRoundTripper(impersonate, query.UserAgent)
		if err != nil {
			return nil, err
		}
		collector.WithTransport(rt)
		collector.UserAgent = userAgent
	default:
		collector.WithTransport(&http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // Adjust if necessary
		})
		if query.UserAgent != "" {
			collector.UserAgent = query.UserAgent
		}
	}
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
func setupFieldScraping(c *colly.Collector, fieldName string, rules []models.MetadataRule, result *string) {
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
						html = strings.ReplaceAll(html, "&nbsp;", "\n")
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
			// Unescape HTML entities like &#39; &quot; &amp; etc. and trim whitespace.
			value = html.UnescapeString(value)
			value = strings.TrimSpace(value)

			if value != "" {
				logger.Pl.S("Grabbed value %q for field %q using selector %q", value, fieldName, rule.Selector)
				*result = value
			}
		})
	}
}

// parseScrapedDate attempts to normalize a scraped date string to "2006-01-02" format.
//
// Returns the normalized string and the parsed time; if parsing fails, returns the
// original (trimmed) string and a zero time.Time.
func parseScrapedDate(raw string) (string, time.Time) {
	raw = strings.TrimSpace(raw)

	var t time.Time
	var err error

	// Plain digit date, e.g. yt-dlp's YYYYMMDD upload_date format.
	if _, parseErr := strconv.ParseInt(raw, 10, 64); parseErr == nil {
		if len(raw) == 8 {
			t, err = time.Parse("20060102", raw)
		} else {
			err = fmt.Errorf("unrecognized plain numeric date length: %q", raw)
		}
	}

	// Hyphenated date, e.g. "2020-12-07".
	if (err != nil || t.IsZero()) && strings.Contains(raw, "-") {
		num := strings.ReplaceAll(raw, "-", "")
		if _, parseErr := strconv.ParseInt(num, 10, 64); parseErr == nil {
			t, err = time.Parse("2006-01-02", raw)
		}
	}

	// RFC3339 timestamp.
	if (err != nil || t.IsZero()) && strings.Contains(raw, "T") {
		t, err = time.Parse(time.RFC3339, raw)
	}

	// Word date, e.g. "December 7, 2020".
	if err != nil || t.IsZero() {
		if parsedDate, parseErr := parsing.ParseWordDate(raw); parseErr == nil {
			t, err = time.Parse("2006-01-02", parsedDate)
		}
	}

	if err != nil || t.IsZero() {
		return raw, time.Time{}
	}
	return t.Format("2006-01-02"), t
}

// parseScrapedYear validates a scraped string as a bare year.
//
// Returns the cleaned year string and whether it looked valid; on failure, returns
// the original (trimmed) string and false.
func parseScrapedYear(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)

	n, err := strconv.Atoi(raw)
	if err != nil || n < 1880 || n > time.Now().Year()+1 {
		return raw, false
	}
	return strconv.Itoa(n), true
}

// ScrapeWithRules scrapes metadata using a site's metadata rules.
func (s *Scraper) ScrapeWithRules(urlStr string, collector *colly.Collector, v *models.Video, query models.SiteRules) map[string]any {
	metadata := make(map[string]any)

	var (
		title          string
		description    string
		directVideoURL string
		thumbnailURL   string
	)

	logger.Pl.I("Scraping %q using rules for %s...", urlStr, query.Site)

	// Group rules by field name
	rulesByField := make(map[string][]models.MetadataRule)
	for _, rule := range query.Metadata {
		rulesByField[rule.Name] = append(rulesByField[rule.Name], rule)
	}

	// Known fields: Do not merge with the below.
	knownFields := map[string]bool{
		sharedtags.JTitle:          true,
		sharedtags.JDescription:    true,
		sharedtags.JDirectVideoURL: true,
		sharedtags.JThumbnailURL:   true,
	}

	// Recognized date fields, each validated as a full date string. Includes an order of preference for v.UploadDate.
	dateFields := map[string]bool{
		sharedtags.JUploadDate:  true,
		sharedtags.JReleaseDate: true,
		sharedtags.JDate:        true,
	}
	uploadDatePrecedence := []string{sharedtags.JUploadDate, sharedtags.JReleaseDate, sharedtags.JDate}

	// Additional recognized fields validated as a bare year instead of a full date.
	yearFields := map[string]bool{
		sharedtags.JYear:        true,
		sharedtags.JReleaseYear: true,
	}

	// Setup scraping for each known field
	if rules, ok := rulesByField[sharedtags.JTitle]; ok {
		setupFieldScraping(collector, sharedtags.JTitle, rules, &title)
	}
	if rules, ok := rulesByField[sharedtags.JDescription]; ok {
		setupFieldScraping(collector, sharedtags.JDescription, rules, &description)
	}
	if rules, ok := rulesByField[sharedtags.JDirectVideoURL]; ok {
		setupFieldScraping(collector, sharedtags.JDirectVideoURL, rules, &directVideoURL)
	}
	if rules, ok := rulesByField[sharedtags.JThumbnailURL]; ok {
		setupFieldScraping(collector, sharedtags.JThumbnailURL, rules, &thumbnailURL)
	}

	// Setup scraping for recognized date fields. Each is written into the metadata JSON
	// under its own key; v.UploadDate is resolved from these afterward, by precedence.
	dateValues := make(map[string]*string, len(dateFields))
	for fieldName := range dateFields {
		rules, ok := rulesByField[fieldName]
		if !ok {
			continue
		}
		value := new(string)
		setupFieldScraping(collector, fieldName, rules, value)
		dateValues[fieldName] = value
	}
	yearValues := make(map[string]*string, len(yearFields))
	for fieldName := range yearFields {
		rules, ok := rulesByField[fieldName]
		if !ok {
			continue
		}
		value := new(string)
		setupFieldScraping(collector, fieldName, rules, value)
		yearValues[fieldName] = value
	}

	// Setup scraping for any remaining user-defined fields outside all the sets above.
	// These get written into the metadata JSON as-is (raw matched text/attr), with no
	// field-specific parsing or validation (no date parsing, no HTML cleanup, etc).
	extraFields := make(map[string]*string, len(rulesByField))
	for fieldName, rules := range rulesByField {
		if knownFields[fieldName] || dateFields[fieldName] || yearFields[fieldName] {
			continue
		}
		value := new(string)
		setupFieldScraping(collector, fieldName, rules, value)
		extraFields[fieldName] = value
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

		if directVideoURL != "" {
			v.DirectVideoURL = directVideoURL
			metadata[sharedtags.JDirectVideoURL] = directVideoURL
		}

		if thumbnailURL != "" {
			v.ThumbnailURL = thumbnailURL
			metadata[sharedtags.JThumbnailURL] = thumbnailURL
		}

		parsedDates := make(map[string]time.Time, len(dateValues))
		for fieldName, value := range dateValues {
			if *value == "" {
				continue
			}
			normalized, t := parseScrapedDate(*value)
			if t.IsZero() {
				logger.Pl.W("Failed to parse date for field %q: %q", fieldName, *value)
			} else {
				parsedDates[fieldName] = t
			}
			metadata[fieldName] = normalized
		}

		// v.UploadDate is resolved from whichever recognized date field parsed
		// successfully, preferring upload_date, then release_date, then date.
		for _, fieldName := range uploadDatePrecedence {
			if t, ok := parsedDates[fieldName]; ok {
				v.UploadDate = t
				logger.Pl.I("Extracted upload date %q from field %q", v.UploadDate.String(), fieldName)
				break
			}
		}

		for fieldName, value := range yearValues {
			if *value == "" {
				continue
			}
			normalized, ok := parseScrapedYear(*value)
			if !ok {
				logger.Pl.W("Scraped value for field %q does not look like a valid year: %q", fieldName, *value)
			}
			metadata[fieldName] = normalized
		}

		for fieldName, value := range extraFields {
			if *value != "" {
				metadata[fieldName] = *value
			}
		}
	})

	return metadata
}
