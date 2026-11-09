package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
	"tubarr/internal/app"
	"tubarr/internal/blocking"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/file"
	"tubarr/internal/limits"
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

	// Apply their current limits to queued videos, catching changes saved without a web edit (such as by config files).
	ss.setQueueLimits(freshChannels...)

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

		// Add jitter
		var jitter time.Duration
		if !abstractions.GetBool(keys.SkipAllWaits) {
			jitter = times.CalculateJitter(c)
		}

		// Crawl the URLs due, each on its own crawl frequency or the channel's.
		due, next := c.DueURLs(now, jitter, true)
		logger.Pl.D(4, "channel %q - %d URL(s) due, next in %s", c.Name, len(due), next.Round(time.Second))
		if len(due) == 0 {
			continue
		}

		// The config file may change what's due, or pause the channel.
		file.UpdateFromConfigFile(ss.s.ChannelStore(), c)

		// Skip paused channels
		if c.ChanSettings.Paused {
			logger.Pl.D(3, "Crawl watchdog: Channel %q is paused, skipping", c.Name)
			continue
		}
		if due, _ = c.DueURLs(now, jitter, true); len(due) == 0 {
			continue
		}

		ch := *c
		ch.URLModels = due

		// Crawl goroutine
		go func(ch *models.Channel) {
			crawlCtx, unlock, ok := state.TryLockCrawl(ctx, ch.ID)
			if !ok {
				return
			}
			defer unlock()

			if err := limits.AcquireSlot(crawlCtx, limits.GlobalCrawls, "crawl", ch.Name); err != nil {
				logger.Pl.D(1, "Crawl watchdog: Crawl for channel %q did not start: %v", ch.Name, err)
				return
			}
			defer limits.GlobalCrawls.Release()

			logger.Pl.D(3, "Crawl watchdog: Triggering scheduled crawl for channel %q", ch.Name)
			if performedCrawls, err := app.CrawlChannel(crawlCtx, ss.s, ss.dq, ch); err != nil {
				logger.Pl.E("Crawl watchdog: Error crawling channel %q: %v", ch.Name, err)
			} else if performedCrawls {
				logger.Pl.S("Crawl watchdog: Completed crawl for channel %q", ch.Name)
			}
		}(&ch)
	}
}

// setQueueLimits applies channels' current concurrency limits to their videos in both queues, waiting ones included.
func (ss *serverStore) setQueueLimits(channels ...*models.Channel) {
	for _, c := range channels {
		ss.dq.SetLimits(c)
		ss.pp.SetLimits(c)
	}
}

// errNoActiveDownload is returned when there is no download to cancel.
var errNoActiveDownload = errors.New("no active download found")

// cancelDownload cancels a video's download, waiting or running.
func (ss *serverStore) cancelDownload(videoID int64) error {
	if !ss.dq.Cancel(videoID) {
		return fmt.Errorf("%w for video ID %d", errNoActiveDownload, videoID)
	}
	return nil
}
