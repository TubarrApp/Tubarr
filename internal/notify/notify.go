// Package notify tells the services a user set for a channel (e.g. media servers) that it has new videos.
package notify

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"time"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/network"

	"github.com/TubarrApp/gocommon/sharedparsing"
)

// notifyHTTPClients returns HTTP clients for non-LAN and LAN use.
func newNotifyHTTPClients(noLANtimeout, lanTimeout time.Duration) (nolan *http.Client, lan *http.Client) {
	nolan = &http.Client{Timeout: noLANtimeout}

	lan = &http.Client{
		Timeout: lanTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	return nolan, lan
}

// Services notifies URLs set for the channel by the user.
func Services(cs contracts.ChannelStore, c *models.Channel, channelURLsWithNew []string) error {
	// Retrieve notifications for this channel.
	notifications, err := cs.GetNotifyURLs(c.ID)
	if err != nil {
		return fmt.Errorf("failed to get notification URLs for channel %q (ID: %d): %w", c.Name, c.ID, err)
	}
	if len(notifications) == 0 {
		logger.Pl.D(1, "No notification URLs configured for channel %q (ID: %d)", c.Name, c.ID)
		return nil
	}

	// Create lookup map for new channels.
	channelsWithNewMap := make(map[string]bool, len(channelURLsWithNew))
	for _, u := range channelURLsWithNew {
		channelsWithNewMap[sharedparsing.NormalizeChannelURL(u)] = true
	}

	// Append valid URLs.
	urls := make([]string, 0, len(notifications))
	for _, n := range notifications {

		if n.ChannelURL == "" {
			logger.Pl.D(3, "Channel URL is empty for notification URL %q, adding to notification list", n.ChannelURL)
			urls = append(urls, n.NotifyURL)
			continue
		}

		logger.Pl.D(3, "Checking %q exists in notification", n.ChannelURL)
		if channelsWithNewMap[sharedparsing.NormalizeChannelURL(n.ChannelURL)] {
			logger.Pl.D(3, "Found %q in notification  adding to notification list", n.ChannelURL)
			urls = append(urls, n.NotifyURL)
		}
	}

	// A notify URL set for several channel URLs that all got new videos is still only notified once.
	slices.Sort(urls)
	urls = slices.Compact(urls)

	// Check if any valid URLs.
	if len(urls) == 0 {
		logger.Pl.D(2, "No notification URLs matched for channel %q (ID: %d)", c.Name, c.ID)
		return nil
	}

	// Send notifications.
	if errs := notify(c, urls); len(errs) != 0 {
		return fmt.Errorf("errors sending notifications for channel with ID %d: %w", c.ID, errors.Join(errs...))
	}

	return nil
}

// notify pings notification services as required.
func notify(c *models.Channel, notifyURLs []string) []error {
	// Inner function.
	notifyFunc := func(client *http.Client, notifyURL string) error {
		resp, err := client.Post(notifyURL, "application/json", nil)
		if err != nil {
			// Drop net/http's copy of the full URL, which can hold tokens.
			if ue, ok := errors.AsType[*url.Error](err); ok {
				err = ue.Err
			}
			return fmt.Errorf("failed to send notification for channel %q (ID: %d): %w", c.Name, c.ID, err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				logger.Pl.E("Failed to close HTTP response body: %v", err)
			}
		}()

		if resp.StatusCode >= 400 {
			return fmt.Errorf("notification failed with status %d for channel %q (ID: %d)", resp.StatusCode, c.Name, c.ID)
		}
		return nil
	}

	// Notify for each URL.
	// Only hosts are logged, since notification URLs often hold tokens.
	errs := make([]error, 0, len(notifyURLs))
	notified := make([]string, 0, len(notifyURLs))

	for _, notifyURL := range notifyURLs {
		parsed, err := url.Parse(notifyURL)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid notification URL for channel %q", c.Name))
			continue
		}
		logger.Pl.D(1, "Notifying %q for channel %q", parsed.Host, c.Name)

		regClient, lanClient := newNotifyHTTPClients((30 * time.Second), (10 * time.Second))
		client := regClient
		if network.IsPrivateNetwork(parsed.Host) {
			client = lanClient
		}

		if err := notifyFunc(client, notifyURL); err != nil {
			errs = append(errs, fmt.Errorf("failed to notify %q: %w", parsed.Host, err))
			continue
		}
		logger.Pl.S("Successfully notified %q for channel %q", parsed.Host, c.Name)
		notified = append(notified, parsed.Host)
	}

	if len(errs) == 0 {
		logger.Pl.S("Successfully notified all URLs for channel %q: %v", c.Name, notified)
		return nil
	}

	return errs
}
