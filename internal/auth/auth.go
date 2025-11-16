package auth

import (
	"encoding/json"
	"fmt"
	"slices"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/validation"
)

// ParseAuthDetails parses authorization details for channel URLs.
//
// Authentication details should be provided as JSON strings:
//   - Single channel: '{"username":"user","password":"pass","login_url":"https://example.com"}'
//   - Multiple channels: '{"channel_url":"https://ch1.com","username":"user","password":"pass","login_url":"https://example.com"}'
//
// Examples:
//
//	'{"username":"john","password":"p@ss,word!","login_url":"https://login.example.com"}'
//	'{"channel_url":"https://ch1.com","username":"user1","password":"pass1","login_url":"https://login1.com"}'
func ParseAuthDetails(u, p, l string, a, cURLs []string, deleteAll bool) (map[string]*models.ChannelAccessDetails, error) {
	authMap := make(map[string]*models.ChannelAccessDetails, len(cURLs))

	// Deduplicate
	a = validation.DeduplicateSliceEntries(a)

	// Handle delete all operation
	if deleteAll {
		for _, cURL := range cURLs {
			authMap[cURL] = &models.ChannelAccessDetails{
				Username: "",
				Password: "",
				LoginURL: "",
			}
		}
		logger.Pl.I("Deleted authentication details for channel URLs: %v", cURLs)
		return authMap, nil
	}

	// Check if there are any auth details to process
	if len(a) == 0 && (u == "" || l == "") {
		logger.Pl.D(3, "No authorization details to parse...")
		return authMap, nil
	}

	// Parse JSON auth strings
	if len(a) > 0 {
		return parseJSONAuth(a, cURLs)
	}

	// Fallback: individual flags (u, p, l) for all channels
	for _, cURL := range cURLs {
		authMap[cURL] = &models.ChannelAccessDetails{
			Username: u,
			Password: p,
			LoginURL: l,
		}
	}
	return authMap, nil
}

// authDetails represents the JSON structure for authentication details.
type authDetails struct {
	ChannelURL string `json:"channel_url,omitempty"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	LoginURL   string `json:"login_url"`
}

// parseJSONAuth parses JSON-formatted authentication strings.
func parseJSONAuth(authStrings []string, cURLs []string) (map[string]*models.ChannelAccessDetails, error) {
	authMap := make(map[string]*models.ChannelAccessDetails, len(authStrings))

	for i, authStr := range authStrings {
		var auth authDetails

		// Parse JSON
		if err := json.Unmarshal([]byte(authStr), &auth); err != nil {
			return nil, fmt.Errorf("invalid JSON in authentication string %d: %w\nExpected format: '{\"username\":\"user\",\"password\":\"pass\",\"login_url\":\"https://example.com\"}'", i+1, err)
		}

		// Validate required fields
		if auth.Username == "" {
			return nil, fmt.Errorf("authentication string %d: username is required", i+1)
		}
		if auth.LoginURL == "" {
			return nil, fmt.Errorf("authentication string %d: login_url is required", i+1)
		}

		// Determine which channel URL to use
		var channelURL string
		if auth.ChannelURL != "" {
			// Explicit channel URL provided
			channelURL = auth.ChannelURL

			// Validate that this channel URL exists
			if !slices.Contains(cURLs, channelURL) {
				return nil, fmt.Errorf("authentication string %d: channel_url %q does not match any of the provided channel URLs: %v", i+1, channelURL, cURLs)
			}
		} else {
			// No explicit channel URL - use single channel if available
			if len(cURLs) != 1 {
				return nil, fmt.Errorf("authentication string %d: channel_url field is required when there are multiple channel URLs (%d provided)", i+1, len(cURLs))
			}
			channelURL = cURLs[0]
		}

		// Check for duplicate channel URL in auth strings
		if _, exists := authMap[channelURL]; exists {
			return nil, fmt.Errorf("duplicate authentication entry for channel URL: %q", channelURL)
		}

		authMap[channelURL] = &models.ChannelAccessDetails{
			Username: auth.Username,
			Password: auth.Password,
			LoginURL: auth.LoginURL,
		}
	}

	// For single channel case with explicit channel_url, verify it matches
	if len(cURLs) == 1 && len(authMap) == 1 {
		for providedURL := range authMap {
			if providedURL != cURLs[0] {
				return nil, fmt.Errorf("failsafe for user error: authentication specified for channel URL %q but actual channel URL is %q", providedURL, cURLs[0])
			}
		}
	}

	return authMap, nil
}
