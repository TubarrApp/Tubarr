// Package downloads handles file downloading commands and operations.
package downloads

import (
	"context"
	"errors"
	"fmt"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

// NewVideoDownload creates a download operation with specified options.
func NewVideoDownload(procCtx context.Context, video *models.Video, channelURL *models.ChannelURL, channel *models.Channel, tracker *DownloadTracker, opts *Options) (*VideoDownload, error) {
	if video == nil {
		return nil, errors.New("video cannot be nil")
	}

	dl := &VideoDownload{
		Video:      video,
		ChannelURL: channelURL,
		Channel:    channel,
		DLTracker:  tracker,
		Context:    procCtx,
	}

	if opts != nil {
		dl.Options = *opts
	} else {
		dl.Options = DefaultOptions
	}

	return dl, nil
}

// Execute performs the download with retries.
func (d *VideoDownload) Execute() (botBlockChannel bool, err error) {
	if d.Video == nil {
		return false, errors.New("video model is nil")
	}

	if _, exists := ongoingDownloads.LoadOrStore(d.Video.URL, struct{}{}); exists {
		logging.I("Skipping duplicate download for: %s", d.Video.URL)
		return false, nil
	}
	defer ongoingDownloads.Delete(d.Video.URL)

	// Ensure cleanup on exit
	defer d.cleanup()

	var lastErr error
	for attempt := 1; attempt <= d.Options.MaxRetries; attempt++ {
		// Check if URL should be avoided (set by previous videos)
		if err := checkIfAvoidURL(d.Video.URL); err != nil {
			return false, err
		}

		// Continue to attempt download
		logging.I("Starting video download attempt %d/%d for URL: %s",
			attempt, d.Options.MaxRetries, d.Video.URL)

		select {
		case <-d.Context.Done():
			logging.W("Download cancelled: %v", d.Context.Err())
			return false, d.cancelVideoDownload()
		default:
			if err := d.videoDLAttempt(); err != nil {
				// Check bot detection FIRST - abort immediately if detected
				if botErr := checkBotDetection(d.Video.URL, err); botErr != nil {
					return true, botErr // Only 'TRUE' path for bot detected
				}

				// Check if URL should be avoided (in case bot was just detected)
				if avoidErr := checkIfAvoidURL(d.Video.URL); avoidErr != nil {
					return false, avoidErr
				}

				// Other errors - continue retry logic
				lastErr = err
				logging.E("Download attempt %d failed: %v", attempt, err)
				d.Video.DownloadStatus.Status = consts.DLStatusFailed
				d.Video.DownloadStatus.Error = err
				d.DLTracker.sendUpdate(d.Video)

				if attempt < d.Options.MaxRetries {
					select {
					case <-d.Context.Done():
						logging.W("Download cancelled: %v", d.Context.Err())
						return false, d.cancelVideoDownload()
					case <-time.After(d.Options.RetryInterval):
						continue
					}
				}
			} else {
				logging.S("Successfully completed video download for URL: %s", d.Video.URL)
				d.Video.UpdatedAt = time.Now()
				d.Video.DownloadStatus.Status = consts.DLStatusCompleted
				d.Video.DownloadStatus.Pct = 100.0
				d.DLTracker.sendUpdate(d.Video)
				return false, nil
			}
		}
	}
	return false, fmt.Errorf("all %d download attempts failed for %s: %w", d.Options.MaxRetries, d.Video.URL, lastErr)
}

// videoDLAttempt performs a single download attempt.
func (d *VideoDownload) videoDLAttempt() error {
	d.mu.Lock()
	d.cmd = d.buildVideoCommand()
	d.mu.Unlock()

	// Set video "Pending" status
	d.Video.DownloadStatus.Status = consts.DLStatusPending
	d.Video.DownloadStatus.Pct = 0.0
	d.DLTracker.sendUpdate(d.Video)

	// Execute the video download
	return d.executeVideoDownload(d.cmd)
}
