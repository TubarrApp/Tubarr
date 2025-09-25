package browser

import (
	"net/url"

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
