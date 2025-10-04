// Package downloads handles file downloading commands and operations.
package downloads

import (
	"context"
	"errors"
	"fmt"
	"time"

	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

// NewJSONDownload creates a download operation with specified options.
func NewJSONDownload(ctx context.Context, video *models.Video, channelURL *models.ChannelURL, channel *models.Channel, tracker *DownloadTracker, opts *Options) (*JSONDownload, error) {
	if video == nil {
		return nil, errors.New("video cannot be nil")
	}

	logging.D(1, "JSON download called with video URL: %q", video.URL)

	dl := &JSONDownload{
		Video:      video,
		Channel:    channel,
		ChannelURL: channelURL,
		DLTracker:  tracker,
		Context:    ctx,
	}

	if opts != nil {
		dl.Options = *opts
	} else {
		dl.Options = DefaultOptions
	}

	return dl, nil
}

// Execute performs the download with retries.
func (d *JSONDownload) Execute() error {
	if d.Video == nil {
		return errors.New("video model is nil")
	}

	if _, exists := ongoingDownloads.LoadOrStore(d.Video.URL, struct{}{}); exists {
		logging.I("Skipping duplicate download for: %s", d.Video.URL)
		return nil
	}
	defer ongoingDownloads.Delete(d.Video.URL)

	var lastErr error
	for attempt := 1; attempt <= d.Options.MaxRetries; attempt++ {
		logging.I("Starting JSON download attempt %d/%d for URL: %s",
			attempt, d.Options.MaxRetries, d.Video.URL)

		select {
		case <-d.Context.Done():
			return d.cancelJSONDownload()
		default:
			if err := d.jsonDLAttempt(); err != nil {
				lastErr = err
				logging.E(0, "Download attempt %d failed: %v", attempt, err)

				if attempt < d.Options.MaxRetries {
					select {
					case <-d.Context.Done():
						return d.cancelJSONDownload()
					case <-time.After(d.Options.RetryInterval):
						continue
					}
				}
			} else {
				logging.S(0, "Successfully completed JSON download for URL: %s", d.Video.URL)
				d.Video.UpdatedAt = time.Now()
				return nil
			}
		}
	}
	return fmt.Errorf("all %d JSON download attempts failed for %s: %w",
		d.Options.MaxRetries, d.Video.URL, lastErr)
}

// executeAttempt performs a single download attempt.
func (d *JSONDownload) jsonDLAttempt() error {
	cmd := d.buildJSONCommand()
	return d.executeJSONDownload(cmd)
}
