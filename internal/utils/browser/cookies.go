package utils

import (
	logging "Tubarr/internal/utils/logging"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/all"
)

// GetBrowserCookies sets cookies input by the user. Useful for getting URLs
// from websites which require authentication!
func GetBrowserCookies(url string) ([]*http.Cookie, error) {

	baseURL, err := extractBaseDomain(url)
	if err != nil {
		return nil, fmt.Errorf("failed to extract base domain: %v", err)
	}

	allCookies := []*http.Cookie{}
	attemptedBrowsers := make(map[string]bool)

	// Find all cookie stores
	allStores := kooky.FindAllCookieStores()
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
			// Append to the Go http.Cookie structure
			for _, c := range cookies {
				allCookies = append(allCookies, &http.Cookie{
					Name:   c.Name,
					Value:  c.Value,
					Path:   c.Path,
					Domain: c.Domain,
					Secure: c.Secure,
				})
			}
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

// extractBaseDomain helper function to parse a domain as just it's base.
// Useful for the purpose of scraping for cookies.
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
