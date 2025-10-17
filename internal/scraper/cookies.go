package scraper

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"tubarr/internal/utils/logging"

	"github.com/browserutils/kooky"
	// Use all browsers for Kooky:
	_ "github.com/browserutils/kooky/browser/all"
)

// CookieManager holds cookies for a domain.
type CookieManager struct {
	mu      sync.RWMutex
	cookies map[string][]*http.Cookie
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
	kookieCookies, err := kooky.ReadCookies(ctx, kooky.Valid, kooky.Domain(domain))
	if err != nil {
		logging.D(2, "Failed reading cookies: %v", err)
		return nil
	}

	if len(kookieCookies) > 0 {
		logging.I("Found %d cookies for %s", len(kookieCookies), domain)
		return convertToHTTPCookies(kookieCookies)
	}

	logging.I("No cookies found for %s", domain)
	return nil
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
	// Return early if no cookies exist
	if len(cookies) == 0 {
		cookieFilePath = ""
		logging.I("%d cookies to write to file %q, won't use '--cookies' in commands", len(cookies), cookieFilePath)
		return nil
	}

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

		secure := "FALSE"
		if cookie.Secure {
			secure = "TRUE"
		}

		// Handle expiration time correctly
		expires := int64(0)
		if !cookie.Expires.IsZero() {
			expires = cookie.Expires.Unix()
		} else {
			// Log if the expiration time is zero
			logging.W("Cookie %s has no expiration time set", cookie.Name)
		}

		_, err := fmt.Fprintf(file, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
			domain, "FALSE", cookie.Path, secure, expires, cookie.Name, cookie.Value)
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

	// Merge duduplicated (from map) cookies together
	merged := make([]*http.Cookie, 0, len(cookieMap))
	for _, c := range cookieMap {
		merged = append(merged, c)
	}
	return merged
}
