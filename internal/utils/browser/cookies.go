package utils

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	config "tubarr/internal/config"
	keys "tubarr/internal/domain/keys"
	logging "tubarr/internal/utils/logging"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/all"
	"github.com/browserutils/kooky/browser/chrome"
	"github.com/browserutils/kooky/browser/firefox"
	"github.com/browserutils/kooky/browser/safari"
)

// GetBrowserCookies sets cookies input by the user. Useful for getting URLs
// from websites which require authentication!

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
func getBrowserCookies(url string) ([]*http.Cookie, error) {
	baseURL, err := extractBaseDomain(url)
	if err != nil {
		return nil, fmt.Errorf("failed to extract base domain: %v", err)
	}

	if config.IsSet(keys.CookiePath) {
		cookieFilePath := config.GetString(keys.CookiePath)

		// If a cookie file path is provided, use it
		if cookieFilePath != "" {
			logging.PrintD(2, "Reading cookies from specified file: %s", cookieFilePath)
			kookyCookies, err := readCookieFile(cookieFilePath)
			if err != nil {
				return nil, fmt.Errorf("failed to read cookies from file: %v", err)
			}
			return convertToHTTPCookies(kookyCookies), nil
		}
	}

	// Otherwise, proceed to use browser cookie stores
	if allStores == nil || allCookies == nil || len(allCookies) == 0 {
		initializeCookies()
	}

	attemptedBrowsers := make(map[string]bool, len(allStores))

	for _, store := range allStores {
		browserName := store.Browser()
		logging.PrintD(2, "Attempting to read cookies from %s", browserName)
		attemptedBrowsers[browserName] = true

		cookies, err := store.ReadCookies(kooky.Valid, kooky.Domain(baseURL))
		if err != nil {
			logging.PrintD(2, "Failed to read cookies from %s: %v", browserName, err)
			continue
		}

		if len(cookies) > 0 {
			logging.PrintI("Successfully read %d cookies from %s for domain %s", len(cookies), browserName, baseURL)
			allCookies = append(allCookies, convertToHTTPCookies(cookies)...)
		} else {
			logging.PrintD(2, "No cookies found for %s", browserName)
		}
	}

	// Log summary of attempted browsers
	logging.PrintI("Attempted to read cookies from the following browsers: %v", keysFromMap(attemptedBrowsers))

	if len(allCookies) == 0 {
		logging.PrintI("No cookies found for '%s', proceeding without cookies", url)
	} else {
		logging.PrintI("Found a total of %d cookies for '%s'", len(allCookies), url)
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
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// readCookieFile reads cookies from the specified cookie file
func readCookieFile(cookieFilePath string) ([]*kooky.Cookie, error) {
	var store kooky.CookieStore
	var err error

	// Attempt to identify and read cookies based on known browser stores
	if strings.Contains(cookieFilePath, "firefox") || strings.Contains(cookieFilePath, "cookies.sqlite") {
		store, err = firefox.CookieStore(cookieFilePath)
	} else if strings.Contains(cookieFilePath, "safari") || strings.Contains(cookieFilePath, "Cookies.binarycookies") {
		store, err = safari.CookieStore(cookieFilePath)
	} else if strings.Contains(cookieFilePath, "chrome") || strings.Contains(cookieFilePath, "Cookies") {
		store, err = chrome.CookieStore(cookieFilePath)
	} else {
		return nil, fmt.Errorf("unsupported cookie file format")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create cookie store: %w", err)
	}

	// Read cookies from the store
	cookies, err := store.ReadCookies()
	if err != nil {
		return nil, fmt.Errorf("failed to read cookies: %w", err)
	}

	return cookies, nil
}
