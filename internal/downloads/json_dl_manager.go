// Package downloads handles file downloading commands and operations.
package downloads

import (
	"context"
	"errors"
	"fmt"
	"time"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/times"
)

// NewJSONDownload creates a download operation with specified options.
func NewJSONDownload(procCtx context.Context, video *models.Video, channelURL *models.ChannelURL, channel *models.Channel, dlStore contracts.DownloadStore, tracker *DownloadTracker, opts *Options) (*JSONDownload, error) {
	if video == nil {
		return nil, errors.New("video cannot be nil")
	}

	logger.Pl.D(1, "JSON download called with video URL: %q", video.URL)

	dl := &JSONDownload{
		Video:      video,
		Channel:    channel,
		ChannelURL: channelURL,
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

// Execute performs the download with retries.
func (d *JSONDownload) Execute() (botBlockChannel bool, err error) {
	if d.Video == nil {
		return false, errors.New("video model is nil")
	}

	key := ongoingKey{TypeJSON, d.Video.URL}
	if _, exists := ongoingDownloads.LoadOrStore(key, struct{}{}); exists {
		return false, fmt.Errorf("metadata for %q: %w", d.Video.URL, ErrDuplicateDownload)
	}
	defer ongoingDownloads.Delete(key)

	// Ensure cleanup on exit.
	defer d.cleanup()

	var (
		lastErr     error
		fsRefreshed bool
	)

	// JSON download attempt loop
	for attempt := 1; attempt <= d.Options.MaxRetries; attempt++ {
		logger.Pl.I("Starting JSON download attempt %d/%d for URL: %s", attempt, d.Options.MaxRetries, d.Video.URL)
		if d.Context.Err() != nil {
			logger.Pl.W("Download canceled: %v", d.Context.Err())
			return false, d.cancelJSONDownload()
		}

		// Download JSON
		err := d.jsonDLAttempt()
		if err == nil { // If err IS nil
			d.Video.UpdatedAt = time.Now()
			logger.Pl.S("Successfully completed JSON download for URL: %s", d.Video.URL)
			return false, nil
		}

		// ERROR PATH:

		// Check bot detection after each attempt.
		if botErr := checkBotDetection(d.Video.URL, d.ChannelURL, d.DLStore.GetDB(), err); botErr != nil {
			return true, botErr // Abort immediately, no retries
		}

		// Check if URL should be avoided (set by previous videos or persistent blocks).
		if avoidErr := checkIfAvoidURL(d.Video.URL, d.ChannelURL, d.DLStore.GetDB()); avoidErr != nil {
			return false, avoidErr
		}

		// Other errors - retry, unless that was the last attempt.
		lastErr = err
		logger.Pl.E("Download attempt %d failed: %v", attempt, err)
		if attempt == d.Options.MaxRetries {
			break
		}
		if !fsRefreshed {
			fsRefreshed = refreshFlareSolverr(d.ChannelURL, d.Video.URL, d.fsGen, err)
		}

		randWait := times.RandomSecsDuration(d.Options.RetryMaxInterval)
		logger.Pl.I("Waiting %.0f seconds before retrying JSON download for %q...", randWait.Seconds(), d.Video.URL)
		select {
		case <-d.Context.Done():
			logger.Pl.W("Download canceled: %v", d.Context.Err())
			return false, d.cancelJSONDownload()
		case <-time.After(randWait):
		}
	}

	// All failed
	return false, fmt.Errorf("all %d JSON download attempts failed for %s: %w", d.Options.MaxRetries, d.Video.URL, lastErr)
}

// jsonDLAttempt performs a single download attempt.
func (d *JSONDownload) jsonDLAttempt() error {
	d.mu.Lock()
	d.cmd = d.buildJSONCommand()
	d.mu.Unlock()

	return d.executeJSONDownload(d.cmd)
}
