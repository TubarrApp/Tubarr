package app

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"tubarr/internal/contracts"
	"tubarr/internal/models"
	"tubarr/internal/net"
	"tubarr/internal/utils/logging"
)

var (
	regClient *http.Client
	lanClient *http.Client
)

func init() {
	regClient = &http.Client{Timeout: 10 * time.Second}
	lanClient = &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

// NotifyServices notifies URLs set for the channel by the user.
func NotifyServices(cs contracts.ChannelStore, c *models.Channel, channelsWithNew []string) error {

	// Retrieve notifications for this channel
	notifications, err := cs.GetNotifyURLs(c.ID)
	if err != nil {
		return fmt.Errorf("failed to get notification URLs for channel %q (ID: %d): %w", c.Name, c.ID, err)
	}
	if len(notifications) == 0 {
		logging.D(1, "No notification URLs configured for channel %q (ID: %d)", c.Name, c.ID)
		return nil
	}

	// Create lookup map for new channels
	channelsWithNewMap := make(map[string]bool, len(channelsWithNew))
	for _, u := range channelsWithNew {
		channelsWithNewMap[strings.ToLower(u)] = true
	}

	// Append valid URLs
	var urls []string
	for _, n := range notifications {

		if n.ChannelURL == "" {
			logging.D(3, "Channel URL is empty for notification URL %q", n.ChannelURL)
			urls = append(urls, n.NotifyURL)
			continue
		}

		logging.D(2, "Checking %q exists in notification", n.ChannelURL)
		if channelsWithNewMap[strings.ToLower(n.ChannelURL)] {
			logging.D(3, "Found %q in notification", n.ChannelURL)
			urls = append(urls, n.NotifyURL)
		}
	}

	// Check if any valid URLs
	if len(urls) == 0 {
		logging.D(2, "No notification URLs matched for channel %q (ID: %d)", c.Name, c.ID)
		return nil
	}

	// Send notifications
	if errs := notify(c, urls); len(errs) != 0 {
		return fmt.Errorf("errors sending notifications for channel with ID %d: %w", c.ID, errors.Join(errs...))
	}

	return nil
}

// notify pings notification services as required.
func notify(c *models.Channel, notifyURLs []string) []error {

	// Inner function
	notifyFunc := func(client *http.Client, notifyURL string) error {
		resp, err := client.Post(notifyURL, applicationJSON, nil)
		if err != nil {
			return fmt.Errorf("failed to send notification to URL %q for channel %q (ID: %d): %w",
				notifyURL, c.Name, c.ID, err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				logging.E("Failed to close HTTP response body: %v", err)
			}
		}()

		if resp.StatusCode >= 400 {
			return fmt.Errorf("notification failed with status %d for channel %q (ID: %d)", resp.StatusCode, c.Name, c.ID)
		}
		return nil
	}

	// Notify for each URL
	errs := make([]error, 0, len(notifyURLs))

	for _, notifyURL := range notifyURLs {
		logging.I("Notifying %q", notifyURL)
		parsed, err := url.Parse(notifyURL)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid notification URL %q: %w", notifyURL, err))
			continue
		}

		client := regClient
		if net.IsPrivateNetwork(parsed.Host) {
			client = lanClient
		}

		if err := notifyFunc(client, notifyURL); err != nil {
			errs = append(errs, fmt.Errorf("failed to notify URL %q: %w", notifyURL, err))
			continue
		}
		logging.S("Successfully notified URL %q for channel %q", notifyURL, c.Name)
	}

	if len(errs) == 0 {
		logging.S("Successfully notified all URLs for channel %q: %v", c.Name, notifyURLs)
		return nil
	}

	return errs
}
