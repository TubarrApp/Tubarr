package scraper

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/models"
	"tubarr/internal/network"
	"tubarr/internal/siterules"

	"github.com/browserutils/kooky"
	// Use all browsers for Kooky:
	_ "github.com/browserutils/kooky/browser/all"
)

// CookieManager handles cookie operations.
type CookieManager struct {
	mu      sync.Mutex
	cookies map[string][]*http.Cookie // Browser store cookies by domain, read once per crawl session.

	// FlareSolverr solutions by hostname for this crawl session.
	fsMu        sync.Mutex
	fsSolutions map[string]*flareSolverrSolution
	fsFailures  map[string]error
	fsGen       int

	// Request headers FlareSolverr's browser sends, copied once per crawl session.
	fsHeaders      [][2]string
	fsHeadersTried bool
}

// NewCookieManager creates a new cookie manager instance.
func NewCookieManager() *CookieManager {
	return &CookieManager{
		cookies:     make(map[string][]*http.Cookie),
		fsSolutions: make(map[string]*flareSolverrSolution),
		fsFailures:  make(map[string]error),
	}
}

// GetChannelURLCookies returns a channel URL's cookies for requests to pageURL's host: its own URL when crawling, a video's when downloading.
func (cm *CookieManager) GetChannelURLCookies(ctx context.Context, cs contracts.ChannelStore, c *models.Channel, cu *models.ChannelURL, pageURL string) (cookies []*http.Cookie, cookieFilePath string, err error) {
	// Fetch auth details if this is a manual entry or not from DB. -- NEVER HAS AUTH? MAY BE UNNECESSARY CODE. --
	if (cu.IsManual || cu.ID == 0) && !cu.NeedsAuth() { // Login details sent with manual videos win.
		cu.Login.Username, cu.Login.Password, cu.Login.LoginURL, err = cs.GetAuth(c.ID, cu.URL)
		if err != nil {
			c.Log().E("Error getting authentication for channel ID %d, URL %q: %v", c.ID, cu.URL, err)
		}
	}

	// Should login, use browser cookies, or use FlareSolverr?
	doLogin := cu.NeedsAuth()
	useGlobal := cu.ChanURLSettings != nil && cu.ChanURLSettings.UseGlobalCookies != nil && *cu.ChanURLSettings.UseGlobalCookies
	useFlareSolverr := siterules.UsesFlareSolverr(pageURL)

	// Early return if no cookies needed.
	if !doLogin && !useGlobal && !useFlareSolverr {
		return nil, "", nil
	}

	// Create cookie file path.
	host := network.Hostname(pageURL)
	cu.Session.CookiePath = generateCookieFilePath(c.Name, cu.URL, host)

	// Collect cookies...
	var authCookies, regCookies []*http.Cookie

	// Cookies from direct login (always fresh, no caching).
	if doLogin {
		authCookies, err = login(ctx, &cu.Login, c.Name)
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		if err != nil {
			c.Log().E("Failed to get auth cookies for %q: %v", cu.URL, err)
		}
	}

	// Cookies from browser cookie stores, for the page's site.
	if useGlobal {
		regCookies, err = cm.GetGlobalCookies(pageURL, c.Name)
		if err != nil {
			c.Log().E("Failed to get cookies for %q: %v", pageURL, err)
		}
	}

	// Combine cookies.
	cookies = mergeCookies(authCookies, regCookies)

	// Cookies from FlareSolverr (override others, e.g. stale Cloudflare cookies from a browser store).
	// If FlareSolverr is unavailable, carries on without it (the failure is already logged).
	if useFlareSolverr {
		if sol, err := cm.flareSolverrSolution(ctx, pageURL, c.Name, 0); err == nil {
			cu.Session.FlareSolverr = models.NewFlareSolverrSolution(sol.ytDLPSolve(), cm.flareSolverrResolver(ctx, cu, pageURL, cookies), cm.flareSolverrNewer(cu, pageURL, cookies))
			cookies = mergeCookies(sol.cookies, cookies)
		} else if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
	}

	for i := range cookies {
		c.Log().D(3, "Got cookie for URL %q: %v", pageURL, cookies[i])
	}

	// Save cookies to file for yt-dlp.
	if len(cookies) > 0 {
		err = saveCookiesToFile(cookies, cu.Session.CookiePath, c.Name)
		if err != nil {
			return nil, "", err
		}
		return cookies, cu.Session.CookiePath, nil
	}

	return cookies, "", nil
}

// GetGlobalCookies returns browser store cookies for a URL's site and its own host, reading each domain once per crawl session.
func (cm *CookieManager) GetGlobalCookies(u, channelName string) ([]*http.Cookie, error) {
	logger.Pl.Channel(channelName).I("Retrieving global cookies for URL: %q", u)
	baseDomain := network.Domain(u)
	if baseDomain == "" {
		return nil, fmt.Errorf("error extracting base domain in cookie grab from URL %q", u)
	}

	// The site's cookies, and the URL's own host's.
	domains := []string{baseDomain}
	if host := network.Hostname(u); host != baseDomain {
		domains = append(domains, host)
	}

	// Each domain is read from the browser stores once, as its cookies are the same for every URL.
	cm.mu.Lock()
	defer cm.mu.Unlock()
	var cookies []*http.Cookie
	for _, d := range domains {
		found, cached := cm.cookies[d]
		if cached {
			logger.Pl.Channel(channelName).D(2, "Using browser cookies for %q read earlier in this crawl session", d)
		} else {
			found = cm.loadCookiesForDomain(d, channelName)
			cm.cookies[d] = found
		}
		cookies = mergeCookies(cookies, found)
	}
	return cookies, nil
}

// ******************************** Private ********************************

// loadCookiesForDomain loads the cookies associated with a particularly domain.
func (cm *CookieManager) loadCookiesForDomain(domain, channelName string) (cookies []*http.Cookie) {
	// Find cookies from browsers.
	for _, d := range []string{domain, "." + domain} {
		cookiesFound := 0
		for _, c := range kooky.ReadCookies(kooky.Valid, kooky.Domain(d)) {
			name := strings.ToLower(c.Name)
			if strings.HasPrefix(name, "st-") || strings.HasPrefix(name, "cst-") || strings.HasPrefix(name, "temp-") {
				continue
			}
			cookies = append(cookies, convertToHTTPCookies([]*kooky.Cookie{c})...)
			cookiesFound++
		}

		if cookiesFound > 0 {
			logger.Pl.Channel(channelName).I("Found %d cookies for domain %s", cookiesFound, d)
		}
	}

	if len(cookies) == 0 {
		logger.Pl.Channel(channelName).I("No cookies found for domain %s", domain)
	}

	return cookies
}

// convertToHTTPCookies converts kooky cookies to http.Cookie format, keeping all their attributes.
func convertToHTTPCookies(kookyCookies []*kooky.Cookie) []*http.Cookie {
	httpCookies := make([]*http.Cookie, len(kookyCookies))
	for i, c := range kookyCookies {
		cookie := c.Cookie
		httpCookies[i] = &cookie
	}
	return httpCookies
}

// cookieFileNameReplacer turns a channel name into a single file name, so names like "AC/DC" can't point into other folders.
var cookieFileNameReplacer = strings.NewReplacer(" ", "-", "/", "-", "\\", "-")

// generateCookieFilePath returns the cookie file of a channel URL's requests to host, one per channel, URL and host.
func generateCookieFilePath(channelName, channelURL, host string) string {
	name := cookieFileNameReplacer.Replace(channelName)

	// Generate a short hash for the URL and host to ensure uniqueness.
	urlHash := sha256.Sum256([]byte(channelURL + " " + host))
	hashString := fmt.Sprintf("%x", urlHash[:8]) // First 8 bytes (16 hex characters).

	return filepath.Join(paths.HomeTubarrDir, fmt.Sprintf("%s_%s.txt", name, hashString))
}

// saveCookiesToFile saves the cookies to a file in Netscape format.
func saveCookiesToFile(cookies []*http.Cookie, cookieFilePath, channelName string) error {
	if len(cookies) == 0 {
		logger.Pl.Channel(channelName).D(2, "No cookies to write, skipping cookie file creation at %q", cookieFilePath)
		return nil
	}

	// Built in memory then renamed into place, so yt-dlp never reads a half-written file.
	var file strings.Builder

	// Write the header for the Netscape cookies file.
	_, err := file.WriteString("# Netscape HTTP Cookie File\n# https://curl.haxx.se/rfc/cookie_spec.html\n# This is a generated file! Do not edit.\n\n")
	if err != nil {
		return err
	}

	// Log the cookies for debugging.
	logger.Pl.Channel(channelName).D(1, "Saving %d cookies to file %s...", len(cookies), cookieFilePath)

	// Write each cookie in Netscape format.
	for _, cookie := range cookies {
		// Domains are set where cookies are obtained (from the host that sent them), so a missing one can't be guessed here.
		domain := cookie.Domain
		if domain == "" {
			logger.Pl.Channel(channelName).D(2, "Skipping cookie %q: it has no domain", cookie.Name)
			continue
		}

		if !strings.HasPrefix(domain, ".") && strings.Count(domain, ".") > 1 {
			domain = "." + domain
		}

		// Domain-specified flag: TRUE if domain starts with dot.
		domainSpecified := "FALSE"
		if strings.HasPrefix(domain, ".") {
			domainSpecified = "TRUE"
		}

		secure := "FALSE"
		if cookie.Secure {
			secure = "TRUE"
		}

		// Handle expiration time.
		var expires int64
		if !cookie.Expires.IsZero() {
			expires = cookie.Expires.Unix()
		}

		_, err := fmt.Fprintf(&file, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
			domain, domainSpecified, cookie.Path, secure, expires, cookie.Name, cookie.Value)
		if err != nil {
			return err
		}
	}

	tmpPath := cookieFilePath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(file.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, cookieFilePath)
}

// mergeCookies merges cookies so that directly input authorization cookies take precedent (last in file).
func mergeCookies(primary, secondary []*http.Cookie) []*http.Cookie {
	cookieMap := make(map[string]*http.Cookie)

	// secondary first (e.g. Firefox).
	for _, c := range secondary {
		key := c.Domain + "|" + c.Path + "|" + c.Name
		cookieMap[key] = c
	}

	// primary overrides (e.g. manual/auth cookies).
	for _, c := range primary {
		key := c.Domain + "|" + c.Path + "|" + c.Name
		cookieMap[key] = c
	}

	// Merge deduplicated (from map) cookies together.
	merged := make([]*http.Cookie, 0, len(cookieMap))
	for _, c := range cookieMap {
		merged = append(merged, c)
	}
	return merged
}
