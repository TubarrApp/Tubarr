package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
	"tubarr/internal/app"
	"tubarr/internal/blocking"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/state"
	"tubarr/internal/times"

	"github.com/TubarrApp/gocommon/abstractions"
)

// startCrawlWatchdog constantly checks channels and runs crawls when they're due.
//
// This function blocks until the stop channel is signaled or context is canceled.
func (ss *serverStore) startCrawlWatchdog(ctx context.Context, stop <-chan os.Signal) {
	const timeBetweenCheck = 10 * time.Second

	// Start ticker
	ticker := time.NewTicker(timeBetweenCheck)
	defer ticker.Stop()
	logger.Pl.I("Crawl watchdog started, checking every %s", timeBetweenCheck.String())
	reported := make(map[int64]string) // Channel load errors already logged, so each is logged once, not every check.

	for {
		select {
		case <-stop:
			logger.Pl.I("Crawl watchdog received stop signal, shutting down...")
			return
		case <-ctx.Done():
			logger.Pl.I("Crawl watchdog context canceled, shutting down...")
			return
		case <-ticker.C:
			ss.crawlDueChannels(ctx, reported)
		}
	}
}

// crawlDueChannels starts a crawl for each channel with URLs due.
func (ss *serverStore) crawlDueChannels(ctx context.Context, reported map[int64]string) {
	// Clean expired blocks first.
	if err := blocking.CleanExpiredBlocks(ss.db); err != nil {
		logger.Pl.W("Failed to clean expired blocks: %v", err)
	}

	// Reload channels from database to get updated LastScan times
	freshChannels, hasRows, err := ss.s.ChannelStore().GetAllChannels(true)
	if err != nil {
		logger.Pl.E("Crawl watchdog: failed to reload channels: %v", err)
		return
	}
	if !hasRows || len(freshChannels) == 0 {
		logger.Pl.D(3, "Crawl watchdog: No channels found in database")
		return
	}

	logger.Pl.D(3, "Crawl watchdog: Checking %d channel(s) for scheduled crawls", len(freshChannels))
	now := time.Now()
	for _, c := range freshChannels {
		if c.LoadErr != nil {
			if msg := c.LoadErr.Error(); reported[c.ID] != msg {
				reported[c.ID] = msg
				logger.Pl.E("Crawl watchdog: skipping crawls until fixed: %v", c.LoadErr)
			}
			continue
		}
		delete(reported, c.ID)

		// Skip paused channels
		if c.ChanSettings.Paused {
			logger.Pl.D(3, "Crawl watchdog: Channel %q is paused, skipping", c.Name)
			continue
		}

		// Add jitter
		var jitter time.Duration
		if !abstractions.IsSet(keys.SkipAllWaits) {
			jitter = times.CalculateJitter(c)
		}

		// Crawl the URLs due, each on its own crawl frequency or the channel's.
		due, next := c.DueURLs(now, jitter, true)
		logger.Pl.D(4, "channel %q - %d URL(s) due, next in %s", c.Name, len(due), next.Round(time.Second))

		if len(due) > 0 {
			ch := *c
			ch.URLModels = due

			// Crawl goroutine
			go func(ch *models.Channel) {
				crawlCtx, unlock, ok := state.TryLockCrawl(ctx, ch.ID)
				if !ok {
					return
				}
				defer unlock()

				release, err := app.AcquireCrawlSlot(state.QueueCtx(crawlCtx), ch.Name)
				if err != nil {
					logger.Pl.D(1, "Crawl watchdog: Crawl for channel %q did not start: %v", ch.Name, err)
					return
				}
				defer release()

				logger.Pl.D(3, "Crawl watchdog: Triggering scheduled crawl for channel %q", ch.Name)
				if performedCrawls, err := app.CrawlChannel(crawlCtx, ss.s, ch); err != nil {
					logger.Pl.E("Crawl watchdog: Error crawling channel %q: %v", ch.Name, err)
				} else if performedCrawls {
					logger.Pl.S("Crawl watchdog: Completed crawl for channel %q", ch.Name)
				}
			}(&ch)
		}
	}
}

// getActiveDownloads retrieves all currently downloading videos for a specific channel from the in-memory StatusUpdate map.
//
// This provides real-time updates without hitting the database on every poll.
func (ss *serverStore) getActiveDownloads(channel *models.Channel) []models.Video {
	var videos []models.Video
	for _, statusUpdate := range state.ChannelStatusUpdates(channel.ID) {
		// Only include Queued or Downloading statuses, and Ignored ones briefly kept to show they were filtered out.
		switch statusUpdate.Status {
		case consts.DLStatusQueued, consts.DLStatusDownloading, consts.DLStatusIgnored:
			// Proceed with loop logic
		default:
			// Go to next iteration
			continue
		}

		// Build a Video model from the StatusUpdate
		var errMsg string
		if statusUpdate.Error != nil {
			errMsg = statusUpdate.Error.Error()
		}
		videos = append(videos, models.Video{
			ID:           statusUpdate.VideoID,
			Title:        statusUpdate.VideoTitle,
			ChannelID:    statusUpdate.ChannelID,
			ChannelURLID: statusUpdate.ChannelURLID,
			ChannelURL:   statusUpdate.ChannelURL,
			URL:          statusUpdate.VideoURL,
			DownloadStatus: models.DLStatus{
				Status:   statusUpdate.Status,
				Percent:  statusUpdate.Percent,
				Stage:    statusUpdate.Stage,
				Error:    statusUpdate.Error,
				ErrorMsg: errMsg,
			},
		})
	}

	logger.Pl.D(1, "Found %d active downloads in memory for channel %d", len(videos), channel.ID)
	return videos
}

// errNoActiveDownload is returned when there is no download to cancel.
var errNoActiveDownload = errors.New("no active download found")

// cancelDownload stops a video's running download and marks it canceled in the database.
//
// The running process is canceled first, as a video waiting to retry isn't Queued or Downloading in the database.
func (ss *serverStore) cancelDownload(ctx context.Context, videoID int64) error {
	videoURL, err := ss.vs.GetVideoURLByID(videoID)
	if err != nil {
		logger.Pl.E("Could not get video URL for ID %d: %v", videoID, err)
	}
	canceled := ss.ds.CancelDownload(videoID, videoURL)

	// Saved even if the client disconnects.
	marked, err := ss.ds.MarkDownloadCanceled(context.WithoutCancel(ctx), videoID)
	if err != nil {
		return err
	}
	if !marked && !canceled {
		return fmt.Errorf("%w for video ID %d", errNoActiveDownload, videoID)
	}
	return nil
}
