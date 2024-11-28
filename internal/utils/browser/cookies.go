package browser

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"tubarr/internal/utils/logging"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/all"
)

var (
	allStores  []kooky.CookieStore
	allCookies []*http.Cookie
)

// initializeCookies initializes all browser cookie stores
func initializeCookies() {
	allStores = kooky.FindAllCookieStores()
	allCookies = []*http.Cookie{}
}

// GetBrowserCookies retrieves cookies for a given URL, using a specified cookie file if provided.
func getBrowserCookies(u string) ([]*http.Cookie, error) {
	baseURL, err := extractBaseDomain(u)
	if err != nil {
		return nil, fmt.Errorf("failed to extract base domain: %w", err)
	}

	// Otherwise, proceed to use browser cookie stores
	if allStores == nil || allCookies == nil || len(allCookies) == 0 {
		initializeCookies()
	}

	attemptedBrowsers := make(map[string]bool, len(allStores))

	for _, store := range allStores {
		browserName := store.Browser()
		logging.D(2, "Attempting to read cookies from %s", browserName)
		attemptedBrowsers[browserName] = true

		cookies, err := store.ReadCookies(kooky.Valid, kooky.Domain(baseURL))
		if err != nil {
			logging.D(2, "Failed to read cookies from %s: %v", browserName, err)
			continue
		}

		if len(cookies) > 0 {
			logging.I("Successfully read %d cookies from %s for domain %s", len(cookies), browserName, baseURL)
			allCookies = append(allCookies, convertToHTTPCookies(cookies)...)
		} else {
			logging.D(2, "No cookies found for %s", browserName)
		}
	}

	// Log summary of attempted browsers
	logging.I("Attempted to read cookies from the following browsers: %v", keysFromMap(attemptedBrowsers))

	if len(allCookies) == 0 {
		logging.I("No cookies found for %q, proceeding without cookies", u)
	} else {
		logging.I("Found a total of %d cookies for %q", len(allCookies), u)
	}

	return allCookies, nil
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

// keysForMap helper function to get keys from a map
func keysFromMap(m map[string]bool) []string {
	mapKeys := make([]string, 0, len(m))
	for k := range m {
		mapKeys = append(mapKeys, k)
	}
	return mapKeys
}
