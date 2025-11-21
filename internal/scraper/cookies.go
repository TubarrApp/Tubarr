package scraper

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/models"

	"github.com/browserutils/kooky"
	// Use all browsers for Kooky:
	_ "github.com/browserutils/kooky/browser/all"
)

// CookieManager handles cookie operations.
type CookieManager struct {
	mu      sync.RWMutex
	stores  []kooky.CookieStore
	cookies map[string][]*http.Cookie
	init    sync.Once
}

// NewCookieManager creates a new cookie manager instance.
func NewCookieManager() *CookieManager {
	return &CookieManager{
		cookies: make(map[string][]*http.Cookie),
	}
}

// GetChannelCookies returns channel access details for a given video.
func (cm *CookieManager) GetChannelCookies(ctx context.Context, cs contracts.ChannelStore, c *models.Channel, cu *models.ChannelURL) (cookies []*http.Cookie, cookieFilePath string, err error) {
	// Fetch auth details if this is a manual entry or not from DB.
	if cu.IsManual || cu.ID == 0 {
		cu.Username, cu.Password, cu.LoginURL, err = cs.GetAuth(c.ID, cu.URL)
		if err != nil {
			logger.Pl.E("Error getting authentication for channel ID %d, URL %q: %v", c.ID, cu.URL, err)
		}
	}

	// Should login?
	doLogin := cu.NeedsAuth()

	// Early return if no cookies needed.
	if !doLogin && !c.ChanSettings.UseGlobalCookies {
		return nil, "", nil
	}

	// Create cookie file path.
	cu.CookiePath = generateCookieFilePath(c.Name, cu.URL)

	// Collect cookies...
	var authCookies, regCookies []*http.Cookie

	// Cookies from direct login.
	if doLogin {
		parsed, err := url.Parse(cu.URL)
		if err != nil {
			return nil, "", fmt.Errorf("failed to parse URL %q: %w", cu.URL, err)
		}
		hostname := parsed.Hostname()

		authCookies, err = channelAuth(ctx, hostname, cu.ToChannelAccessDetails())
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		if err != nil {
			logger.Pl.E("Failed to get auth cookies for %q: %v", cu.URL, err)
		}
	}

	// Cookies from Kooky's 'FindAllCookieStores()' function.
	if c.ChanSettings.UseGlobalCookies {
		regCookies, err = cm.GetCookies(ctx, cu.URL)
		if err != nil {
			logger.Pl.E("Failed to get cookies for %q with cookie source %q: %v", cu.URL, c.ChanSettings.CookiesFromBrowser, err)
		}
	}

	// Combine cookies.
	cookies = mergeCookies(authCookies, regCookies)

	for i := range cookies {
		logger.Pl.D(3, "Got cookie for URL %q: %v", cu.URL, cookies[i])
	}

	// Save cookies to file.
	if len(cookies) > 0 {
		err = saveCookiesToFile(cookies, cu.LoginURL, cu.CookiePath)
		if err != nil {
			return nil, "", err
		}
		return cookies, cu.CookiePath, nil
	}

	return cookies, "", nil
}

// GetCookies retrieves cookies for a given URL.
func (cm *CookieManager) GetCookies(ctx context.Context, u string) ([]*http.Cookie, error) {
	baseDomain, err := getBaseDomain(u)
	if err != nil {
		return nil, fmt.Errorf("error extracting base domain in cookie grab: %w", err)
	}

	// Initialize once.
	cm.init.Do(func() {
		cm.stores = kooky.FindAllCookieStores(ctx)
	})

	// Check if cookies already exist for domain.
	cm.mu.RLock()
	if cookies, ok := cm.cookies[baseDomain]; ok {
		cm.mu.RUnlock()
		return cookies, nil
	}
	cm.mu.RUnlock()

	// Load cookies for domain.
	cookies := cm.loadCookiesForDomain(ctx, baseDomain)

	// Store cookies.
	cm.mu.Lock()
	cm.cookies[baseDomain] = cookies
	cm.mu.Unlock()

	return cookies, nil
}

// GetCachedAuthCookies retrieves cookies from the global auth cache.
func (cm *CookieManager) GetCachedAuthCookies(hostname string) []*http.Cookie {
	if val, ok := globalAuthCookieCache.Load(hostname); ok {
		if cookies, ok := val.([]*http.Cookie); ok {
			return cookies
		}
	}
	return nil
}

// ******************************** Private ********************************

// loadCookiesForDomain loads the cookies associated with a particularly domain.
func (cm *CookieManager) loadCookiesForDomain(ctx context.Context, domain string) []*http.Cookie {
	var cookies []*http.Cookie
	attempted := make([]string, 0)

	domainsToTry := []string{domain, "." + domain}

	for _, d := range domainsToTry {
		kookieCookies, err := kooky.ReadCookies(ctx, kooky.Valid, kooky.Domain(d))
		if err != nil {
			logger.Pl.D(2, "Failed reading cookies for domain %s: %v", d, err)
			continue
		}

		cookiesFound := 0
		for _, c := range kookieCookies {
			name := strings.ToLower(c.Name)
			if strings.HasPrefix(name, "st-") || strings.HasPrefix(name, "cst-") || strings.HasPrefix(name, "temp-") {
				continue
			}
			cookies = append(cookies, convertToHTTPCookies([]*kooky.Cookie{c})...)
			cookiesFound++
		}

		if cookiesFound > 0 {
			logger.Pl.I("Found %d cookies for domain %s", cookiesFound, d)
		}

		// ReadCookies already uses all registered stores internally.
		attempted = append(attempted, "kooky.ReadCookies")
	}

	logger.Pl.I("Checked browsers: %v", attempted)
	if len(cookies) == 0 {
		logger.Pl.I("No cookies found for %s", domain)
	}

	return cookies
}

// convertToHTTPCookies converts kooky cookies to http.Cookie format.
func convertToHTTPCookies(kookyCookies []*kooky.Cookie) []*http.Cookie {
	httpCookies := make([]*http.Cookie, len(kookyCookies))
	for i, c := range kookyCookies {
		httpCookies[i] = &http.Cookie{
			Name:   c.Name,
			Value:  c.Value,
			Path:   c.Path,
			Domain: c.Domain,
			Secure: c.Secure,
		}
	}
	return httpCookies
}

// generateCookieFilePath generates a unique authentication file path per channel and URL.
func generateCookieFilePath(channelName, videoURL string) string {
	// If no specific URL is provided, return the default per-channel auth file.
	if videoURL == "" {
		return filepath.Join(paths.HomeTubarrDir, strings.ReplaceAll(channelName, " ", "-")+".txt")
	}

	// Generate a short hash for the URL to ensure uniqueness.
	urlHash := sha256.Sum256([]byte(videoURL))
	hashString := fmt.Sprintf("%x", urlHash[:8]) // Use first 8 hex characters.

	return filepath.Join(paths.HomeTubarrDir, fmt.Sprintf("%s_%s.txt", strings.ReplaceAll(channelName, " ", "-"), hashString))
}

// saveCookiesToFile saves the cookies to a file in Netscape format.
func saveCookiesToFile(cookies []*http.Cookie, loginURL, cookieFilePath string) error {
	if len(cookies) == 0 {
		logger.Pl.D(2, "No cookies to write, skipping cookie file creation for login URL %q and generated path %q", loginURL, cookieFilePath)
		return nil
	}

	file, err := os.Create(cookieFilePath)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			logger.Pl.E("failed to close file %q due to error: %v", cookieFilePath, err)
		}
	}()

	// Write the header for the Netscape cookies file.
	_, err = file.WriteString("# Netscape HTTP Cookie File\n# https://curl.haxx.se/rfc/cookie_spec.html\n# This is a generated file! Do not edit.\n\n")
	if err != nil {
		return err
	}

	// Log the cookies for debugging.
	logger.Pl.D(1, "Saving %d cookies to file %s...", len(cookies), cookieFilePath)

	for _, cookie := range cookies {
		domain := cookie.Domain
		if domain == "" {
			domain = loginURL
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

		_, err := fmt.Fprintf(file, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
			domain, domainSpecified, cookie.Path, secure, expires, cookie.Name, cookie.Value)
		if err != nil {
			return err
		}
	}
	return nil
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

// tryLoadCachedCookies attempts to load cookies from cache for a given key.
func tryLoadCachedCookies(key string) ([]*http.Cookie, bool) {
	val, ok := globalAuthCookieCache.Load(key)
	if !ok {
		return nil, false
	}

	cookies, ok := val.([]*http.Cookie)
	if !ok {
		// Invalid type in cache, delete and re-auth.
		globalAuthCookieCache.Delete(key)
		return nil, false
	}

	if len(cookies) == 0 {
		logger.Pl.W("Found cached auth for %q but cookie slice is empty", key)
	}

	return cookies, true
}
