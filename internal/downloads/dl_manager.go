// Package downloads handles file downloading commands and operations.
package downloads

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

// NewDownload creates a download operation with specified options.
func NewDownload(dlType DownloadType, ctx context.Context, video *models.Video, tracker *DownloadTracker, opts *Options) (*Download, error) {
	if video == nil {
		return nil, errors.New("video cannot be nil")
	}

	dl := &Download{
		Type:      dlType,
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
func (d *Download) Execute() error {
	if d.Video == nil {
		return errors.New("video model is nil")
	}

	var lastErr error
	for attempt := 1; attempt <= d.Options.MaxRetries; attempt++ {
		logging.I("Starting %s download attempt %d/%d for URL: %s",
			d.Type, attempt, d.Options.MaxRetries, d.Video.URL)

		if err := d.executeAttempt(); err != nil {
			lastErr = err
			logging.E(0, "Download attempt %d failed: %v", attempt, err)

			d.Video.DownloadStatus.Status = consts.DLStatusFailed
			d.Video.DownloadStatus.Error = err
			d.DLTracker.sendUpdate(d.Video)

			if attempt < d.Options.MaxRetries {
				time.Sleep(d.Options.RetryInterval)
				continue
			}
		} else {
			logging.S(0, "Successfully completed %s download for URL: %s", d.Type, d.Video.URL)

			d.Video.UpdatedAt = time.Now()
			d.Video.DownloadStatus.Status = consts.DLStatusCompleted
			d.Video.DownloadStatus.Pct = 100.0

			d.DLTracker.sendUpdate(d.Video)

			return nil
		}
	}

	return fmt.Errorf("all %d download attempts failed for %s: %w",
		d.Options.MaxRetries, d.Video.URL, lastErr)
}

// executeAttempt performs a single download attempt.
func (d *Download) executeAttempt() error {
	var cmd *exec.Cmd
	switch d.Type {
	case TypeJSON:
		cmd = d.buildJSONCommand()
	case TypeVideo:
		cmd = d.buildVideoCommand()
	default:
		return fmt.Errorf("unsupported download type: %s", d.Type)
	}

	if d.Type == TypeJSON {
		return d.executeJSONDownload(cmd)
	}

	d.Video.DownloadStatus.Status = consts.DLStatusDownloading
	d.Video.DownloadStatus.Pct = 0.0
	d.DLTracker.sendUpdate(d.Video)

	return d.executeVideoDownload(cmd)
}
