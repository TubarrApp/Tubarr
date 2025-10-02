package browser

import (
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
func (b *Browser) GetChannelAccessDetails(cs interfaces.ChannelStore, c *models.Channel, cURL string) ([]*http.Cookie, *models.ChannelAccessDetails, error) {
	// Retrieve authentication details for this specific video URL

	parsed, err := url.Parse(cURL)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse URL %q: %w", cURL, err)
	}

	hostname := parsed.Hostname()

	// Retrieve authentication details for this specific video URL
	username, password, loginURL, err := cs.GetAuth(c.ID, cURL)
	if err != nil {
		logging.E(0, "Error getting authentication for channel ID %d, URL %q: %v", c.ID, cURL, err)
	}

	var (
		authCookies, regCookies, cookies []*http.Cookie
		generatedCookiePath              string
		chanAccessDetails                *models.ChannelAccessDetails
	)

	doLogin := ((username != "" || password != "") && loginURL != "")

	if doLogin || c.Settings.UseGlobalCookies {
		generatedCookiePath = generateCookieFilePath(c.Name, cURL)

		baseDomain, err := BaseDomain(cURL)
		if err != nil {
			logging.E(0, "Failed to grab base domain for video URL %q: %v", cURL, err)
		}

		chanAccessDetails = &models.ChannelAccessDetails{
			Username:   username,
			Password:   password,
			LoginURL:   loginURL,
			BaseDomain: baseDomain,
			CookiePath: generatedCookiePath,
		}

		// If authorization details exist, perform login and store cookies.
		if doLogin {
			authCookies, err = channelAuth(hostname, chanAccessDetails)
			if err != nil {
				logging.E(0, "Failed to get auth cookies for %q: %v", cURL, err)
			}
		}

		// Get cookies globally
		if c.Settings.UseGlobalCookies {
			regCookies, err = b.cookies.GetCookies(cURL)
			if err != nil {
				logging.E(0, "Failed to get cookies for %q with cookie source %q: %v", cURL, c.Settings.CookieSource, err)
			}
		}

		// Combine cookies
		cookies = mergeCookies(authCookies, regCookies)

		// Save cookies to file
		err = saveCookiesToFile(cookies, chanAccessDetails)
		if err != nil {
			return nil, nil, err
		}
	}

	if chanAccessDetails == nil {
		chanAccessDetails = &models.ChannelAccessDetails{}
	}

	return cookies, chanAccessDetails, nil
}
