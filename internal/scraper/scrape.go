// Package scraper handles web scraping operations.
package scraper

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/command"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/file"
	"tubarr/internal/limits"
	"tubarr/internal/models"
	"tubarr/internal/siterules"
	"tubarr/internal/times"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/abstractions"
	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/shareddates"
	"github.com/TubarrApp/gocommon/sharedparsing"
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

// GetNewReleases checks a channel's URLs for new video URLs, returning those found alongside any per-URL errors.
func (s *Scraper) GetNewReleases(ctx context.Context, cs contracts.ChannelStore, c *models.Channel, ignoreCrawl bool) ([]*models.Video, error) {
	if len(c.URLModels) == 0 {
		logger.Pl.D(1, "Channel %q has no URLs configured, skipping automatic crawl", c.Name)
		return []*models.Video{}, nil
	}

	existingURLs, failedURLs, err := cs.GetDownloadedOrIgnoredVideoURLs(c)
	if err != nil {
		return nil, err
	}

	var (
		newRequests []*models.Video
		urlErrs     error
		crawledAny  bool
	)

	// Process each ChannelURL
	for _, cu := range c.URLModels {
		if cu.IsManual || (!ignoreCrawl && cu.Paused()) {
			continue
		}
		logger.Pl.D(1, "Processing channel URL %q", cu.URL)

		// Stagger crawls of the channel's URLs (the channel already waited before the first).
		if crawledAny {
			if err := times.WaitTime(ctx, times.RandomSecsDuration(consts.DefaultBotAvoidanceSeconds), c.Name, cu.URL, ""); err != nil {
				return nil, err
			}
		}
		crawledAny = true

		// Hold a domain crawl slot (no-op if the domain has no crawl limit).
		crawlSem, err := limits.DomainCrawls.Acquire(ctx, cu.URL)
		if err != nil {
			return nil, err
		}

		// Get access details once per ChannelURL - delegated to CookieManager
		cu.Cookies, cu.CookiePath, err = s.cookieManager.GetChannelURLCookies(ctx, cs, c, cu)
		if err != nil {
			limits.Release(crawlSem)
			if ctx.Err() != nil {
				return nil, err
			}
			logger.Pl.E("Skipping channel URL %q, failed to get cookies: %v", cu.URL, err)
			urlErrs = errors.Join(urlErrs, fmt.Errorf("channel URL %q: %w", cu.URL, err))
			continue
		}

		// Failed videos wait in the Available list, so only auto-downloading URLs retry them (canceled ones are never retried).
		seenURLs := existingURLs
		if !c.AutoDownloads(cu) {
			seenURLs = slices.Concat(existingURLs, failedURLs)
		}

		// Fetch new episode URLs
		newEpisodes, err := s.newEpisodeURLs(ctx, c.Name, cu, seenURLs)
		limits.Release(crawlSem)
		if err != nil {
			if ctx.Err() != nil {
				return nil, err
			}
			logger.Pl.E("Skipping channel URL %q, crawl failed: %v", cu.URL, err)
			urlErrs = errors.Join(urlErrs, fmt.Errorf("channel URL %q: %w", cu.URL, err))
			continue
		}

		// Failed videos found again use up one of their crawl retries (Ignore to Date ignores them instead).
		if !ignoreCrawl && c.AutoDownloads(cu) {
			var retried []string
			for _, u := range failedURLs {
				if _, found := newEpisodes[u]; found {
					retried = append(retried, u)
				}
			}
			if err := cs.AddCrawlDLRetries(c.ID, retried); err != nil {
				logger.Pl.E("Failed to count crawl retries for channel URL %q: %v", cu.URL, err)
			}
		}

		// Filter and create video requests, in the order the channel lists them.
		for _, newURL := range listedInOrder(newEpisodes) {
			lv := newEpisodes[newURL]
			if lv == nil {
				continue
			}
			if !validation.IsHTTPURL(newURL) {
				logger.Pl.W("Skipping crawled URL %q for channel %q: not an http(s) URL", newURL, c.Name)
				continue
			}
			if !validation.IsHTTPURL(lv.ThumbnailURL) {
				lv.ThumbnailURL = ""
			}
			video := &models.Video{
				ChannelID:    c.ID,
				ChannelURLID: cu.ID,
				ChannelURL:   cu.URL,
				URL:          newURL,
				Title:        lv.Title,
				ThumbnailURL: lv.ThumbnailURL,
				UploadDate:   lv.UploadDate,
				Duration:     lv.Duration,
			}
			cu.Videos = append(cu.Videos, video)
			newRequests = append(newRequests, video)
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

	return newRequests, urlErrs
}

// ScrapeCustomSite scrapes custom sites for metadata, sending cookies (e.g. a crawl's login) with the page request.
func (s *Scraper) ScrapeCustomSite(ctx context.Context, urlStr, outputDir string, v *models.Video, cookies []*http.Cookie) error {
	// Find a registered rule set (built-in or user-supplied) based on URL domain.
	query, ok := siterules.MatchMetadata(v.URL)
	if !ok {
		logger.Pl.D(1, "No custom scraping rules found for URL: %s - will use yt-dlp", v.URL)
		return nil // Not a custom site.
	}

	// Visit the webpage.
	metadata := make(map[string]any)
	if err := s.visitPage(ctx, urlStr, cookies, query, func(c *colly.Collector) {
		metadata = s.ScrapeWithRules(urlStr, c, v, query)
	}); err != nil {
		return fmt.Errorf("failed to visit URL: %w", err)
	}

	// Validate required fields.
	if v.Title == "" {
		logger.Pl.D(3, "Scraped metadata: %+v", metadata)
		return fmt.Errorf("missing required metadata fields (title: %q)", v.Title)
	}

	// Metarr renames videos to their full title by default, which yt-dlp always fills.
	if fulltitle, _ := metadata[sharedtags.JFulltitle].(string); fulltitle == "" {
		metadata[sharedtags.JFulltitle] = v.Title
	}

	// Write metadata to file, named like yt-dlp's downloads: a hash of the URL keeps apart videos with the same title.
	sum := sha256.Sum256([]byte(v.URL))
	filename := fmt.Sprintf("%s [%s].json", sanitizeFilename(v.Title), hex.EncodeToString(sum[:4]))
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
func TestScrapeSite(ctx context.Context, urlStr string) (matched bool, site string, metadata map[string]any, err error) {
	query, ok := siterules.MatchMetadata(urlStr)
	if !ok {
		return false, "", nil, nil
	}

	s := New()
	v := &models.Video{URL: urlStr}
	if err := s.visitPage(ctx, urlStr, nil, query, func(c *colly.Collector) {
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

// newEpisodeURLs returns new episode URLs not yet downloaded, ignored, or Available, with what the listing shows of each.
func (s *Scraper) newEpisodeURLs(ctx context.Context, channelName string, cu *models.ChannelURL, existingURLs []string) (map[string]*listedVideo, error) {
	channelURL := cu.URL

	// Custom crawl rules and built-in scrapers take priority over yt-dlp.
	_, episodes, crawled, err := s.crawlChannelURLs(ctx, channelURL, cu.Cookies)
	if err != nil {
		return nil, err
	}
	if !crawled {
		if episodes, err = ytDlpURLFetch(ctx, channelName, cu); err != nil {
			return nil, err
		}
	}

	// Add URLs given with --add-url, which have no listing details.
	if episodes == nil {
		episodes = make(map[string]*listedVideo)
	}
	if abstractions.IsSet(keys.URLAdd) {
		for _, u := range abstractions.GetStringSlice(keys.URLAdd) {
			if _, ok := episodes[u]; !ok {
				episodes[u] = &listedVideo{Order: len(episodes)}
			}
		}
	}

	// Filter out existing URLs
	episodeURLs := make([]string, 0, len(episodes))
	for u := range episodes {
		episodeURLs = append(episodeURLs, u)
	}
	newURLs := ignoreDownloadedURLs(episodeURLs, existingURLs)

	if len(newURLs) == 0 {
		logger.Pl.I("No new videos at %s", channelURL)
		return nil, nil
	}
	newEpisodes := make(map[string]*listedVideo, len(newURLs))
	for _, u := range newURLs {
		newEpisodes[u] = episodes[u]
	}
	return newEpisodes, nil
}

// ignoreDownloadedURLs filters out already downloaded URLs, compared normalized.
func ignoreDownloadedURLs(inputURLs, existingURLs []string) []string {
	existing := make(map[string]struct{}, len(existingURLs))
	for _, u := range existingURLs {
		existing[normalizeURL(u)] = struct{}{}
	}
	newURLs := make([]string, 0, len(inputURLs))
	for _, u := range inputURLs {
		if _, seen := existing[normalizeURL(u)]; !seen {
			newURLs = append(newURLs, u)
		}
	}
	return newURLs
}

// ytDlpURLFetch fetches a channel URL's video URLs using yt-dlp.
func ytDlpURLFetch(ctx context.Context, channelName string, cu *models.ChannelURL) (map[string]*listedVideo, error) {
	uniqueEpisodeURLs := make(map[string]*listedVideo)
	channelURL, fs := cu.URL, cu.FlareSolverr
	var crawlArgs, browser string
	if cu.ChanURLSettings != nil {
		crawlArgs, browser = cu.ChanURLSettings.ExtraYTDLPCrawlArgs, cu.ChanURLSettings.CookiesFromBrowser
	}

	// Run yt-dlp, refreshing the FlareSolverr solve and retrying once if Cloudflare blocks a FlareSolverr site.
	var j []byte
	for retried := false; ; retried = true {
		// Build argument
		args := []string{command.YtDLPFlatPlaylist}

		// Add custom crawl arguments.
		if crawlArgs != "" {
			splitArgs, err := sharedparsing.ShellSplit(crawlArgs)
			if err != nil {
				return nil, err
			}
			args = append(args, splitArgs...)
		}

		// Cookie file and browser cookies, which yt-dlp combines (the file's winning where both set one).
		if cu.CookiePath != "" {
			args = append(args, command.CookiePath, cu.CookiePath)
		}
		if browser != "" {
			args = append(args, command.CookiesFromBrowser, browser)
		}

		// Site rule impersonation, and FlareSolverr's user agent (its cookies are in the cookie file).
		args, fsGen := siterules.AddSiteRulesToYTDLP(channelURL, fs, args)

		// Add -J and URL to finalize command
		args = append(args, command.OutputJSON, command.EndOfOptions, channelURL)
		cmd := exec.CommandContext(ctx, command.YTDLP, args...)

		logger.Pl.I("Executing YTDLP playlist fetch command for channel %q URL %q:\n\n%s\n", channelName, channelURL, cmd.String())

		var err error
		if j, err = cmd.Output(); err == nil {
			break
		}

		var stderr string
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			stderr = strings.TrimSpace(string(exitErr.Stderr))
		}
		if fs == nil || retried || !models.IsCloudflareBlock(stderr) {
			return uniqueEpisodeURLs, fmt.Errorf("yt-dlp command failed: %w: %s", err, stderr)
		}

		logger.Pl.W("Cloudflare blocked yt-dlp for %q, refreshing its FlareSolverr solve...", channelURL)
		if err := fs.Refresh(fsGen); err != nil {
			return uniqueEpisodeURLs, fmt.Errorf("yt-dlp was blocked by Cloudflare (%s), and a fresh FlareSolverr solve failed: %w", stderr, err)
		}
	}

	logger.Pl.D(5, "Retrieved command output from YTDLP for channel %q:\n\n%s", channelURL, string(j))

	var result ytDlpEntry
	if err := json.Unmarshal(j, &result); err != nil {
		return uniqueEpisodeURLs, err
	}

	// Retrieve base domain for comparisons.
	chanBaseDomain, _ := getBaseDomain(channelURL)

	// Process entries and add to map.
	for _, entry := range result.videoEntries() {
		// Skip URL-less entries, like empty nested playlists.
		if entry == &result || entry.URL == "" {
			continue
		}
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
		mergeListed(uniqueEpisodeURLs, entry.URL, entry.listed())
		logger.Pl.D(3, "Added entry for channel %q: %q", channelURL, entry)
	}

	return uniqueEpisodeURLs, nil
}

// visitPage visits urlStr with a collector for the site, after setup registers its callbacks on it.
//
// For FlareSolverr sites, requests use the solved cookies and user agent, and a Cloudflare challenge
// triggers one fresh FlareSolverr solve and retry. If FlareSolverr is unavailable, falls back to the
// site's normal requests.
func (s *Scraper) visitPage(ctx context.Context, urlStr string, cookies []*http.Cookie, query models.SiteRules, setup func(c *colly.Collector)) error {
	var (
		sol   *flareSolverrSolution
		fsErr error
	)
	if query.FlareSolverr {
		sol, fsErr = s.cookieManager.flareSolverrSolution(ctx, urlStr, 0)
	}

	for retried := false; ; retried = true {
		if err := ctx.Err(); err != nil {
			return err
		}
		challenged, err := s.visitPageOnce(ctx, urlStr, cookies, query, sol, setup)
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

		logger.Pl.W("Cloudflare challenged the request for %q, refreshing its FlareSolverr solve...", urlStr)
		if sol, err = s.cookieManager.flareSolverrSolution(ctx, urlStr, sol.gen); err != nil {
			return fmt.Errorf("cloudflare challenged the request for %q, and a fresh FlareSolverr solve failed: %w", urlStr, err)
		}
	}
}

// visitPageOnce makes a single visitPage attempt, reporting whether Cloudflare challenged it.
func (s *Scraper) visitPageOnce(ctx context.Context, urlStr string, cookies []*http.Cookie, query models.SiteRules, sol *flareSolverrSolution, setup func(c *colly.Collector)) (challenged bool, err error) {
	c, err := initializeCollector(ctx, urlStr, s.cookieManager, query, sol, cookies)
	if err != nil {
		return false, err
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
// Cookies are set in order, later ones replacing earlier ones with the same name: this crawl session's, then the caller's
// (e.g. the channel URL's), then for FlareSolverr sites (sol set), the solution's, so a refreshed solve isn't undone by
// older Cloudflare cookies. FlareSolverr sites impersonate Chrome with the solution's user agent. Requests are canceled with ctx.
func initializeCollector(ctx context.Context, urlStr string, cm *CookieManager, query models.SiteRules, sol *flareSolverrSolution, cookies []*http.Cookie) (c *colly.Collector, err error) {
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
	if cached := cm.GetCachedCookies(parsedURL.Hostname()); cached != nil {
		jar.SetCookies(parsedURL, cached)
		logger.Pl.D(2, "Set %d cookies for scraper from this crawl session", len(cached))
	} else {
		logger.Pl.D(2, "No cookies available for scraper for hostname %q", parsedURL.Hostname())
	}
	if len(cookies) > 0 {
		jar.SetCookies(parsedURL, cookies)
	}

	// Create a Colly collector with the custom HTTP client
	collector := colly.NewCollector(
		colly.Async(true),
	)
	collector.SetRequestTimeout(60 * time.Second)
	transport := http.DefaultTransport
	switch impersonate := consts.Impersonate(query.Impersonate); {
	case sol != nil:
		rt, userAgent, err := newTLSRoundTripper(consts.ImpersonateChrome, sol.userAgent)
		if err != nil {
			return nil, err
		}
		rt.headers = sol.headers
		transport = rt
		collector.UserAgent = userAgent
		jar.SetCookies(parsedURL, sol.cookies) // Set last, to replace any stale Cloudflare cookies.
	case impersonate != consts.ImpersonateNone:
		rt, userAgent, err := newTLSRoundTripper(impersonate, query.UserAgent)
		if err != nil {
			return nil, err
		}
		transport = rt
		collector.UserAgent = userAgent
	default: // The standard transport, which verifies certificates.
		if query.UserAgent != "" {
			collector.UserAgent = query.UserAgent
		}
	}
	collector.WithTransport(ctxRoundTripper{ctx: ctx, next: transport})
	collector.SetCookieJar(jar)

	return collector, nil
}

// ctxRoundTripper sends requests through next under ctx, as colly v1 has no request context.
type ctxRoundTripper struct {
	ctx  context.Context
	next http.RoundTripper
}

// RoundTrip sends req through next under the round tripper's context.
func (t ctxRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.next.RoundTrip(req.WithContext(t.ctx))
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
					htmlStr, err := h.DOM.Html()
					if err == nil {
						// Clean HTML tags and format
						htmlStr = strings.ReplaceAll(htmlStr, "<br>", "\n")
						htmlStr = strings.ReplaceAll(htmlStr, "<br/>", "\n")
						htmlStr = strings.ReplaceAll(htmlStr, "<br />", "\n")
						htmlStr = strings.ReplaceAll(htmlStr, "&nbsp;", "\n")
						htmlStr = strings.ReplaceAll(htmlStr, " \n", "\n")
						htmlStr = strings.ReplaceAll(htmlStr, "\n ", "\n")
						htmlStr = strings.TrimSpace(htmlStr)
						value = htmlStr
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
			if value != "" && (rule.JSONPath != "" || rule.Regex != nil || len(rule.Fetch) > 0) {
				value = ruleValue(c, h.Request.URL.String(), value, rule)
			}

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

	// yt-dlp's YYYYMMDD upload_date, "2020-12-07", or an RFC3339 timestamp.
	for _, layout := range []string{"20060102", "2006-01-02", time.RFC3339} {
		if t, err := time.Parse(layout, raw); err == nil && !t.IsZero() {
			return t.Format("2006-01-02"), t
		}
	}

	// Word date, e.g. "December 7, 2020".
	parsedDate, err := shareddates.ParseWordDate(raw)
	if err != nil {
		return raw, time.Time{}
	}
	t, err := time.Parse("2006-01-02", parsedDate)
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
		duration       string
		durationString string
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
		sharedtags.JThumbnail:      true,
		sharedtags.JDuration:       true,
		sharedtags.JDurationString: true,
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
	if rules, ok := rulesByField[sharedtags.JThumbnail]; ok {
		setupFieldScraping(collector, sharedtags.JThumbnail, rules, &thumbnailURL)
	}
	if rules, ok := rulesByField[sharedtags.JDuration]; ok {
		setupFieldScraping(collector, sharedtags.JDuration, rules, &duration)
	}
	if rules, ok := rulesByField[sharedtags.JDurationString]; ok {
		setupFieldScraping(collector, sharedtags.JDurationString, rules, &durationString)
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
			metadata[sharedtags.JThumbnail] = thumbnailURL
		}

		// Duration is written in seconds, from the duration field, or else parsed from duration_string (kept as scraped).
		if durationString != "" {
			metadata[sharedtags.JDurationString] = durationString
		}
		parsedDuration, _ := sharedparsing.ParseDuration(duration)
		parsedDurationStr, _ := sharedparsing.ParseDuration(durationString)
		if secs := cmp.Or(parsedDuration, parsedDurationStr); secs > 0 {
			v.Duration = secs
			metadata[sharedtags.JDuration] = secs
		} else if duration != "" || durationString != "" {
			logger.Pl.W("Failed to parse a duration from field %q (%q) or %q (%q)", sharedtags.JDuration, duration, sharedtags.JDurationString, durationString)
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
