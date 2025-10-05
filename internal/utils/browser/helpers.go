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
func (b *Browser) GetChannelCookies(cs interfaces.ChannelStore, c *models.Channel, cu *models.ChannelURL, ctx context.Context) (cookies []*http.Cookie, generatedCookiePath string, err error) {

	// Retrieve authentication details for this specific video URL
	username, password, loginURL, err := cs.GetAuth(c.ID, cu.URL)
	if err != nil {
		logging.E(0, "Error getting authentication for channel ID %d, URL %q: %v", c.ID, cu.URL, err)
	}

	var (
		authCookies, regCookies []*http.Cookie
		chanAccessDetails       = &models.ChannelAccessDetails{}
	)

	doLogin := username != "" && loginURL != ""

	if doLogin || c.ChanSettings.UseGlobalCookies {

		// Parse hostname for cookie search
		parsed, err := url.Parse(cu.URL)
		if err != nil {
			return nil, "", fmt.Errorf("failed to parse URL %q: %w", cu.URL, err)
		}

		hostname := parsed.Hostname()

		// Create cookie file path
		generatedCookiePath = generateCookieFilePath(c.Name, cu.URL)

		chanAccessDetails = &models.ChannelAccessDetails{
			ChannelURL: cu.URL,
			Username:   username,
			Password:   password,
			LoginURL:   loginURL,
			CookiePath: generatedCookiePath,
		}

		// If authorization details exist, perform login and store cookies.
		if doLogin {
			authCookies, err = channelAuth(hostname, chanAccessDetails, ctx)
			if ctx.Err() != nil {
				return nil, "", ctx.Err()
			}
			if err != nil {
				logging.E(0, "Failed to get auth cookies for %q: %v", cu.URL, err)
			}
		}

		// Get cookies globally
		if c.ChanSettings.UseGlobalCookies {
			regCookies, err = b.cookies.GetCookies(cu.URL)
			if err != nil {
				logging.E(0, "Failed to get cookies for %q with cookie source %q: %v", cu.URL, c.ChanSettings.CookieSource, err)
			}
		}

		// Combine cookies
		cookies = mergeCookies(authCookies, regCookies)

		// Save cookies to file
		err = saveCookiesToFile(cookies, loginURL, generatedCookiePath)
		if err != nil {
			return nil, "", err
		}
	}
	return cookies, generatedCookiePath, nil
}
