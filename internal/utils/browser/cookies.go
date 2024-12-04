package browser

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

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

// GetCookies retrieves cookies for a given URL, using a specified cookie file if provided.
func (cm *CookieManager) GetCookies(u string) ([]*http.Cookie, error) {
	baseURL, err := extractBaseDomain(u)
	if err != nil {
		return nil, fmt.Errorf("extracting base domain: %w", err)
	}

	// Initialize once
	cm.init.Do(func() {
		cm.stores = kooky.FindAllCookieStores()
	})

	// Check if we already have cookies for this domain
	cm.mu.RLock()
	if cookies, ok := cm.cookies[baseURL]; ok {
		cm.mu.RUnlock()
		return cookies, nil
	}
	cm.mu.RUnlock()

	// Load cookies for domain
	cookies, err := cm.loadCookiesForDomain(baseURL)
	if err != nil {
		return nil, err
	}

	// Store cookies
	cm.mu.Lock()
	cm.cookies[baseURL] = cookies
	cm.mu.Unlock()

	return cookies, nil
}

// loadCookiesForDomain loads the cookies associated with a particularly domain.
func (cm *CookieManager) loadCookiesForDomain(domain string) ([]*http.Cookie, error) {
	var cookies []*http.Cookie
	var attempted []string

	for _, store := range cm.stores {
		browserName := store.Browser()
		attempted = append(attempted, browserName)

		kookieCookies, err := store.ReadCookies(kooky.Valid, kooky.Domain(domain))
		if err != nil {
			logging.D(2, "Failed reading cookies from %s: %v", browserName, err)
			continue
		}

		if len(kookieCookies) > 0 {
			logging.I("Found %d cookies in %s for %s", len(kookieCookies), browserName, domain)
			cookies = append(cookies, convertToHTTPCookies(kookieCookies)...)
		}
	}

	logging.I("Checked browsers: %v", attempted)
	if len(cookies) == 0 {
		logging.I("No cookies found for %s", domain)
	}

	return cookies, nil
}

// convertToHTTPCookies converts kooky cookies to http.Cookie format
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

// extractBaseDomain parses a URL and extracts its base domain
func extractBaseDomain(urlString string) (string, error) {
	parsedURL, err := url.Parse(urlString)
	if err != nil {
		return "", err
	}

	parts := strings.Split(parsedURL.Hostname(), ".")
	if len(parts) > 2 {
		return strings.Join(parts[len(parts)-2:], "."), nil
	}
	return parsedURL.Hostname(), nil
}
