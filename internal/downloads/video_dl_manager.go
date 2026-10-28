// Package downloads handles file downloading commands and operations.
package downloads

import (
	"context"
	"errors"
	"fmt"
	"time"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/times"
)

// NewVideoDownload creates a download operation with specified options.
func NewVideoDownload(procCtx context.Context, video *models.Video, channelURL *models.ChannelURL, channel *models.Channel, dlStore contracts.DownloadStore, tracker Tracker, opts *Options) (*VideoDownload, error) {
	if video == nil {
		return nil, errors.New("video cannot be nil")
	}

	dl := &VideoDownload{
		Video:      video,
		ChannelURL: channelURL,
		Channel:    channel,
		DLStore:    dlStore,
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

// Execute performs the download with retries, once the caller holds its download slots.
func (d *VideoDownload) Execute() error {
	if d.Video == nil {
		return errors.New("video model is nil")
	}

	// Ensure cleanup on exit.
	defer d.cleanup()

	var (
		lastErr     error
		fsRefreshed bool
	)
	for attempt := 1; attempt <= d.Options.MaxAttempts; attempt++ {
		// Check if URL should be avoided (set by previous videos or persistent blocks).
		if err := CheckIfAvoidURL(d.Video, d.ChannelURL, d.DLStore.GetDB()); err != nil {
			return err
		}

		logger.Pl.I("Starting video download attempt %d/%d for URL: %s", attempt, d.Options.MaxAttempts, d.Video.URL)
		if d.Context.Err() != nil {
			logger.Pl.W("Download canceled: %v", d.Context.Err())
			return d.cancelVideoDownload()
		}

		err := d.videoDLAttempt()
		if err == nil {
			d.Video.UpdatedAt = time.Now()
			d.DLTracker.SetStatus(d.Video, consts.DLStatusCompleted, 100.0, "", nil)
			logger.Pl.S("Successfully completed video download for URL: %s", d.Video.URL)
			return nil
		}
		if d.Context.Err() != nil {
			return d.cancelVideoDownload()
		}

		// Check bot detection FIRST - abort immediately if detected.
		if botErr := checkBotDetection(d.Video, d.ChannelURL, d.DLStore.GetDB(), err); botErr != nil {
			return botErr
		}

		// Check if URL should be avoided (in case bot was just detected).
		if avoidErr := CheckIfAvoidURL(d.Video, d.ChannelURL, d.DLStore.GetDB()); avoidErr != nil {
			return avoidErr
		}

		// Other errors - retry, unless that was the last attempt.
		lastErr = err
		logger.Pl.E("Download attempt %d failed: %v", attempt, err)
		d.DLTracker.SetStatus(d.Video, consts.DLStatusFailed, d.Video.DownloadStatus.Percent, "", err)
		if attempt == d.Options.MaxAttempts {
			break
		}
		if !fsRefreshed {
			fsRefreshed = refreshFlareSolverr(d.ChannelURL, d.Video.URL, d.fsGen, err)
		}

		randWait := times.RandomSecsDuration(d.Options.RetryMaxInterval)
		logger.Pl.I("Waiting %.0f seconds before retrying video download for %q...", randWait.Seconds(), d.Video.URL)
		d.DLTracker.SetStatus(d.Video, consts.DLStatusQueued, 0,
			fmt.Sprintf("Retrying in %.0fs (attempt %d/%d)", randWait.Seconds(), attempt+1, d.Options.MaxAttempts), nil)
		select {
		case <-d.Context.Done():
			logger.Pl.W("Download canceled: %v", d.Context.Err())
			return d.cancelVideoDownload()
		case <-time.After(randWait):
		}
	}
	return fmt.Errorf("all %d download attempts failed for %s: %w", d.Options.MaxAttempts, d.Video.URL, lastErr)
}

// videoDLAttempt performs a single download attempt.
func (d *VideoDownload) videoDLAttempt() error {
	d.mu.Lock()
	d.cmd = d.buildVideoCommand()
	d.mu.Unlock()

	// Start each attempt from 0%, the first progress line may be a finished stream or resumed part.
	d.DLTracker.SetStatus(d.Video, consts.DLStatusDownloading, 0, "", nil)

	// Execute the video download.
	return d.executeVideoDownload(d.cmd)
}
