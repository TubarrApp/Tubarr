package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"tubarr/internal/interfaces"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"

	"golang.org/x/net/publicsuffix"
)

// BaseDomain returns the base domain for an inputted URL.
func BaseDomain(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return publicsuffix.EffectiveTLDPlusOne(u.Hostname())
}

// GetChannelAccessDetails returns channel access details for a given video.
func (b *Browser) GetChannelCookies(cs interfaces.ChannelStore, c *models.Channel, cu *models.ChannelURL, ctx context.Context) (cookies []*http.Cookie, cookieFilePath string, err error) {

	// Fetch auth details if this is a manual entry or not from DB
	if cu.IsManual || cu.ID == 0 {
		cu.Username, cu.Password, cu.LoginURL, err = cs.GetAuth(c.ID, cu.URL)
		if err != nil {
			logging.E(0, "Error getting authentication for channel ID %d, URL %q: %v", c.ID, cu.URL, err)
		}
	}

	// Should login?
	doLogin := cu.Username != "" && cu.LoginURL != "" // 'True' if BOTH username and login URL are non-empty

	// Early return if no cookies needed
	if !doLogin && !c.ChanSettings.UseGlobalCookies {
		return nil, "", nil
	}

	// Create cookie file path
	cu.CookiePath = generateCookieFilePath(c.Name, cu.URL)

	// Collect cookies...
	var authCookies, regCookies []*http.Cookie

	// Cookies from direct login
	if doLogin {
		parsed, err := url.Parse(cu.URL)
		if err != nil {
			return nil, "", fmt.Errorf("failed to parse URL %q: %w", cu.URL, err)
		}
		hostname := parsed.Hostname()

		authCookies, err = channelAuth(hostname, cu.ToChannelAccessDetails(), ctx)
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		if err != nil {
			logging.E(0, "Failed to get auth cookies for %q: %v", cu.URL, err)
		}
	}

	// Cookies from Kooky's 'FindAllCookieStores()' function
	if c.ChanSettings.UseGlobalCookies {
		regCookies, err = b.cookies.GetCookies(cu.URL)
		if err != nil {
			logging.E(0, "Failed to get cookies for %q with cookie source %q: %v", cu.URL, c.ChanSettings.CookieSource, err)
		}
	}

	// Combine cookies
	cookies = mergeCookies(authCookies, regCookies)

	// Save cookies to file
	err = saveCookiesToFile(cookies, cu.LoginURL, cu.CookiePath)
	if err != nil {
		return nil, "", err
	}

	return cookies, cu.CookiePath, nil
}
