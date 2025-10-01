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
func NewVideoDownload(ctx context.Context, video *models.Video, tracker *DownloadTracker, opts *Options) (*VideoDownload, error) {
	if video == nil {
		return nil, errors.New("video cannot be nil")
	}

	dl := &VideoDownload{
		Video:     video,
		DLTracker: tracker,
		Context:   ctx,
	}

	if opts != nil {
		dl.Options = *opts
	} else {
		dl.Options = DefaultOptions
	}

	return dl, nil
}

// Execute performs the download with retries.
func (d *VideoDownload) Execute() error {
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
		logging.I("Starting video download attempt %d/%d for URL: %s",
			attempt, d.Options.MaxRetries, d.Video.URL)

		select {
		case <-d.Context.Done():
			logging.I("Context is done for video with URL %q", d.Video.URL)
			return d.cancelVideoDownload()
		default:
			if err := d.videoDLAttempt(); err != nil {
				lastErr = err
				logging.E(0, "Download attempt %d failed: %v", attempt, err)

				d.Video.DownloadStatus.Status = consts.DLStatusFailed
				d.Video.DownloadStatus.Error = err
				d.DLTracker.sendUpdate(d.Video)

				if attempt < d.Options.MaxRetries {
					select {
					case <-d.Context.Done():
						return d.cancelVideoDownload()
					case <-time.After(d.Options.RetryInterval):
						continue
					}
				}
			} else {
				logging.S(0, "Successfully completed video download for URL: %s", d.Video.URL)

				d.Video.UpdatedAt = time.Now()
				d.Video.DownloadStatus.Status = consts.DLStatusCompleted
				d.Video.DownloadStatus.Pct = 100.0

				d.DLTracker.sendUpdate(d.Video)
				return nil
			}
		}
	}
	return fmt.Errorf("all %d download attempts failed for %s: %w",
		d.Options.MaxRetries, d.Video.URL, lastErr)
}

// executeAttempt performs a single download attempt.
func (d *VideoDownload) videoDLAttempt() error {
	cmd := d.buildVideoCommand()

	// Set video "Pending" status
	d.Video.DownloadStatus.Status = consts.DLStatusPending
	d.Video.DownloadStatus.Pct = 0.0
	d.DLTracker.sendUpdate(d.Video)

	// Execute the video download
	return d.executeVideoDownload(cmd)
}
