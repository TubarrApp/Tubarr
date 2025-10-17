package scraper

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"tubarr/internal/domain/setup"
	"tubarr/internal/utils/logging"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/all"
)

// CookieManager holds cookies for a domain.
type CookieManager struct {
	mu      sync.RWMutex
	stores  []kooky.CookieStore
	cookies map[string][]*http.Cookie
	init    sync.Once
}

// NewCookieManager initializes a new cookie manager instance.
func NewCookieManager() *CookieManager {
	return &CookieManager{
		cookies: make(map[string][]*http.Cookie),
	}
}

// GetCookies retrieves cookies for a given URL.
func (cm *CookieManager) GetCookies(ctx context.Context, u string) ([]*http.Cookie, error) {
	baseURL, err := baseDomain(u)
	if err != nil {
		return nil, fmt.Errorf("error extracting base domain in cookie grab: %w", err)
	}

	// Initialize stores once
	cm.init.Do(func() {
		cm.stores = kooky.FindAllCookieStores(ctx)
	})

	// Check if we already have cookies for this domain
	cm.mu.RLock()
	if cookies, ok := cm.cookies[baseURL]; ok {
		cm.mu.RUnlock()
		return cookies, nil
	}
	cm.mu.RUnlock()

	// Load cookies for domain
	cookies := cm.loadCookiesForDomain(ctx, baseURL)

	// Store cookies
	cm.mu.Lock()
	cm.cookies[baseURL] = cookies
	cm.mu.Unlock()

	return cookies, nil
}

// loadCookiesForDomain loads the cookies associated with a particularly domain.
func (cm *CookieManager) loadCookiesForDomain(ctx context.Context, domain string) []*http.Cookie {
	var cookies []*http.Cookie
	attempted := make([]string, 0, len(cm.stores))

	// Silence kooky's verbose internal logging
	oldLog := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(oldLog)

	logging.D(1, "Searching for cookies for domain: %q", domain)

	// Domain filter
	domainFilter := kooky.FilterFunc(func(c *kooky.Cookie) bool {
		matchesDomain := c.Domain == domain || c.Domain == "."+domain
		if !matchesDomain {
			return false
		}

		// Filter out temporary session tokens that bloat cookie files
		// These are usually prefixed with ST-, CST-, or similar patterns
		if strings.HasPrefix(strings.ToLower(c.Name), "st-") ||
			strings.HasPrefix(strings.ToLower(c.Name), "cst-") ||
			strings.HasPrefix(strings.ToLower(c.Name), "temp-") {
			return false
		}
		return true
	})

	// Iterate over stores
	for _, store := range cm.stores {
		browserName := store.Browser()
		attempted = append(attempted, browserName)

		kookieCookies := store.TraverseCookies(
			kooky.Valid,
			domainFilter,
		).Collect(ctx)

		if len(kookieCookies) > 0 {
			logging.I("Found %d cookies in %s for %s", len(kookieCookies), browserName, domain)
			cookies = append(cookies, convertToHTTPCookies(kookieCookies)...)
		}
	}

	logging.D(1, "Checked browsers: %v", attempted)

	if len(cookies) == 0 {
		logging.I("No cookies found for %s", domain)
	} else {
		logging.I("Total: %d cookies for %s", len(cookies), domain)
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

// saveCookiesToFile saves the cookies to a file in Netscape format.
func saveCookiesToFile(cookies []*http.Cookie, loginURL, cookieFilePath string) error {
	file, err := os.Create(cookieFilePath)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			logging.E("failed to close file %q due to error: %v", cookieFilePath, err)
		}
	}()

	// Write the header for the Netscape cookies file
	_, err = file.WriteString("# Netscape HTTP Cookie File\n# https://curl.haxx.se/rfc/cookie_spec.html\n# This is a generated file! Do not edit.\n\n")
	if err != nil {
		return err
	}

	// Log the cookies for debugging
	logging.D(1, "Saving %d cookies to file %s...", len(cookies), cookieFilePath)

	for _, cookie := range cookies {
		domain := cookie.Domain
		if domain == "" {
			domain = loginURL
		}

		if !strings.HasPrefix(domain, ".") && strings.Count(domain, ".") > 1 {
			domain = "." + domain
		}

		// Domain-specified flag: TRUE if domain starts with dot
		domainSpecified := "FALSE"
		if strings.HasPrefix(domain, ".") {
			domainSpecified = "TRUE"
		}

		secure := "FALSE"
		if cookie.Secure {
			secure = "TRUE"
		}

		// Handle expiration time
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

	// secondary first (e.g. Firefox)
	for _, c := range secondary {
		key := c.Domain + "|" + c.Path + "|" + c.Name
		cookieMap[key] = c
	}

	// primary overrides (e.g. manual/auth cookies)
	for _, c := range primary {
		key := c.Domain + "|" + c.Path + "|" + c.Name
		cookieMap[key] = c
	}

	// Merge deduplicated (from map) cookies together
	merged := make([]*http.Cookie, 0, len(cookieMap))
	for _, c := range cookieMap {
		merged = append(merged, c)
	}
	return merged
}

// generateCookieFilePath generates a unique authentication file path per channel and URL.
func generateCookieFilePath(channelName, videoURL string) string {
	// If no specific URL is provided, return the default per-channel auth file.
	if videoURL == "" {
		return filepath.Join(setup.HomeTubarrDir, strings.ReplaceAll(channelName, " ", "-")+".txt")
	}

	// Generate a short hash for the URL to ensure uniqueness
	urlHash := sha256.Sum256([]byte(videoURL))
	hashString := fmt.Sprintf("%x", urlHash[:8]) // Use the first 8 hex characters

	// Construct file path (e.g., ~/.tubarr/CensoredTV_Show_a1b2c3d4.txt)
	return filepath.Join(setup.HomeTubarrDir, fmt.Sprintf("%s_%s.txt", strings.ReplaceAll(channelName, " ", "-"), hashString))
}
