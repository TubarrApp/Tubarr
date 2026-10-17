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
	"tubarr/internal/limits"
	"tubarr/internal/models"
	"tubarr/internal/times"
)

// NewVideoDownload creates a download operation with specified options.
func NewVideoDownload(procCtx context.Context, video *models.Video, channelURL *models.ChannelURL, channel *models.Channel, dlStore contracts.DownloadStore, tracker *DownloadTracker, opts *Options) (*VideoDownload, error) {
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

// Execute performs the download with retries.
func (d *VideoDownload) Execute() (botBlockChannel bool, err error) {
	if d.Video == nil {
		return false, errors.New("video model is nil")
	}

	key := ongoingKey{TypeVideo, d.Video.URL}
	if _, exists := ongoingDownloads.LoadOrStore(key, struct{}{}); exists {
		return false, fmt.Errorf("video %q: %w", d.Video.URL, ErrDuplicateDownload)
	}
	defer ongoingDownloads.Delete(key)
	d.DLTracker.SetStatus(d.Video, consts.DLStatusQueued, 0, "Waiting for download slot", nil)

	// Acquire per-domain slot first (blocks if domain limit reached, no-op if no limit).
	domainSem, err := limits.DomainDownloads.Acquire(d.Context, d.Video.URL)
	if err != nil {
		return false, d.cancelVideoDownload()
	}
	defer limits.Release(domainSem)

	// Acquire global download slot (blocks if limit reached, no-op if limit=0).
	// Store the semaphore to ensure we release on the same one we acquired.
	dlSem, err := AcquireGlobalDownloadSlot(d.Context, d.Video.URL)
	if err != nil {
		return false, d.cancelVideoDownload()
	}
	defer limits.Release(dlSem)

	// Ensure cleanup on exit.
	defer d.cleanup()

	var (
		lastErr     error
		fsRefreshed bool
	)
	for attempt := 1; attempt <= d.Options.MaxRetries; attempt++ {
		// Check if URL should be avoided (set by previous videos or persistent blocks).
		if err := checkIfAvoidURL(d.Video.URL, d.ChannelURL, d.DLStore.GetDB()); err != nil {
			return false, err
		}

		// Continue to attempt download.
		logger.Pl.I("Starting video download attempt %d/%d for URL: %s",
			attempt, d.Options.MaxRetries, d.Video.URL)

		select {
		case <-d.Context.Done():
			logger.Pl.W("Download canceled: %v", d.Context.Err())
			return false, d.cancelVideoDownload()
		default:
			if err := d.videoDLAttempt(); err != nil {
				if d.Context.Err() != nil {
					return false, d.cancelVideoDownload()
				}

				// Check bot detection FIRST - abort immediately if detected.
				if botErr := checkBotDetection(d.Video.URL, d.ChannelURL, d.DLStore.GetDB(), err); botErr != nil {
					return true, botErr // Only 'TRUE' path for bot detected.
				}

				// Check if URL should be avoided (in case bot was just detected).
				if avoidErr := checkIfAvoidURL(d.Video.URL, d.ChannelURL, d.DLStore.GetDB()); avoidErr != nil {
					return false, avoidErr
				}

				// Other errors - continue retry logic.
				lastErr = err
				logger.Pl.E("Download attempt %d failed: %v", attempt, err)
				d.DLTracker.SetStatus(d.Video, consts.DLStatusFailed, d.Video.DownloadStatus.Percent, "", err)

				if attempt < d.Options.MaxRetries {
					if !fsRefreshed {
						fsRefreshed = refreshFlareSolverr(d.ChannelURL, d.Video.URL, d.fsGen, err)
					}

					randWait := times.RandomSecsDuration(d.Options.RetryMaxInterval)
					logger.Pl.I("Waiting %.0f seconds before retrying video download for %q...", randWait.Seconds(), d.Video.URL)
					d.DLTracker.SetStatus(d.Video, consts.DLStatusQueued, 0,
						fmt.Sprintf("Retrying in %.0fs (attempt %d/%d)", randWait.Seconds(), attempt+1, d.Options.MaxRetries), nil)

					select {
					case <-d.Context.Done():
						logger.Pl.W("Download canceled: %v", d.Context.Err())
						return false, d.cancelVideoDownload()
					case <-time.After(randWait):
						continue
					}
				}
			} else {
				d.Video.UpdatedAt = time.Now()
				d.DLTracker.SetStatus(d.Video, consts.DLStatusCompleted, 100.0, "", nil)
				logger.Pl.S("Successfully completed video download for URL: %s", d.Video.URL)

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

	// Start each attempt from 0%, the first progress line may be a finished stream or resumed part.
	d.DLTracker.SetStatus(d.Video, consts.DLStatusDownloading, 0, "", nil)

	// Execute the video download.
	return d.executeVideoDownload(d.cmd)
}
