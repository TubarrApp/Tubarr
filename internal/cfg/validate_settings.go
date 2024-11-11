package cfg

import (
	"fmt"
	"net/http"
	"net/url"
	"tubarr/internal/models"
	logging "tubarr/internal/utils/logging"
)

// validateConfig validates entries in the config file
func validateConfig(c *models.Config) (err error) {
	if c == nil {
		return fmt.Errorf("model is null")
	}

	// Trim invalid URLs
	if c.RawURLs.URLs, err = validateURLs(c.RawURLs.URLs); err != nil {
		logging.E(0, err.Error())
	}

	// Check sites
	for siteName, site := range c.Sites {
		if site == nil {
			delete(c.Sites, siteName)
			logging.E(0, "Site entered null")
			continue
		}
		for chanName, channel := range c.Channels {
			if channel == nil {
				delete(c.Channels, chanName)
				logging.E(0, "Channel entered null")
				continue
			}
			if cUrl := validateURL(channel.URL); cUrl == "" {
				logging.E(0, "Channel URL '%s' invalid, removing channel...", channel)
				delete(c.Channels, chanName)
			} else {
				channel.URL = cUrl
			}
			channel.SkipTitles = append(channel.SkipTitles, site.Settings.SkipTitles...)
		}
		if len(c.Channels) == 0 {
			delete(c.Sites, siteName)
		}
	}
	cleanupConfig(c)
	return nil
}

// validateURLs validates a list of URLs
func validateURLs(rawUrls []string) (valid []string, err error) {

	var failed []string

	count := len(rawUrls)
	for _, line := range rawUrls {
		if line == "" || line == "\n" {
			continue
		}

		if r := validateURL(line); r != "" {
			valid = append(valid, r)
		} else {
			failed = append(failed, r)
			count--
		}
	}

	if len(valid) == 0 {
		return nil, fmt.Errorf("no valid URLs passed in. URL list: %v", rawUrls)
	}

	if len(failed) > 0 {
		logging.E(0, "Got %d valid URLs. Other URLs seem to be invalid: %v", count, failed)
	} else {
		logging.S(0, "Retrieved %d valid URLs", count)
	}

	return valid, nil
}

// validateURL validates a single URL
func validateURL(rawUrl string) string {
	if rawUrl == "" {
		return ""
	}

	u, err := url.Parse(rawUrl)
	if err != nil {
		logging.E(0, "Error '%v' parsing URL '%s', returning empty", err, rawUrl)
		return ""
	}

	resp, err := http.Get(u.String())
	if err != nil {
		logging.E(0, "Encountered error validating URL '%s': Err: %v", u.String(), err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logging.E(0, "Invalid response status '%s' for URL '%s'", resp.Status, u.String())
		return ""
	}

	return u.String()
}

func cleanupConfig(c *models.Config) {
	if len(c.Sites) == 0 {
		c.Sites = nil
	}
	if len(c.Channels) == 0 {
		c.Channels = nil
	}
	if len(c.RawURLs.URLs) == 0 {
		c.RawURLs.URLs = nil
	}
}
