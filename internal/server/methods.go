package server

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"
	"tubarr/internal/app"
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
// This function blocks until the stop channel is signaled or context is cancelled.
func (ss *serverStore) startCrawlWatchdog(ctx context.Context, stop <-chan os.Signal) {

	const timeBetweenCheck = (1 * time.Minute)

	// Start ticker
	ticker := time.NewTicker(timeBetweenCheck) // check every minute
	defer ticker.Stop()
	logger.Pl.I("Crawl watchdog started, checking every %s", timeBetweenCheck.String())

	for {
		select {
		case <-stop:
			logger.Pl.I("Crawl watchdog received stop signal, shutting down...")
			return
		case <-ctx.Done():
			logger.Pl.I("Crawl watchdog context cancelled, shutting down...")
			return
		case <-ticker.C:
			// Reload channels from database to get updated LastScan times
			freshChannels, hasRows, err := ss.s.ChannelStore().GetAllChannels(true)
			if err != nil {
				logger.Pl.E("Crawl watchdog: failed to reload channels: %v", err)
				continue
			}
			if !hasRows || len(freshChannels) == 0 {
				logger.Pl.D(2, "Crawl watchdog: no channels found in database")
				continue
			}

			logger.Pl.D(2, "Crawl watchdog: checking %d channel(s) for scheduled crawls", len(freshChannels))
			now := time.Now()
			for _, c := range freshChannels {
				// Skip paused channels
				if c.ChanSettings.Paused {
					logger.Pl.D(2, "Crawl watchdog: channel %q is paused, skipping", c.Name)
					continue
				}

				// Calculate elapsed time since last scan
				elapsed := now.Sub(c.LastScan)
				interval := time.Duration(c.ChanSettings.CrawlFreq) * time.Minute

				// Add jitter
				if !abstractions.IsSet(keys.SkipAllWaits) {
					jitterInt := state.GetOrComputeJitter(c.ID, len(c.URLModels), func() int {
						j := 5
						for _, u := range c.URLModels {
							url := strings.ToLower(u.URL)
							switch {
							case strings.Contains(url, "tube"), strings.Contains(url, "tok"),
								strings.Contains(url, "gram"), strings.Contains(url, "book"), strings.Contains(url, "x."):
								j = max(10, j)
							case strings.Contains(url, "motion"), strings.Contains(url, "vime"),
								strings.Contains(url, "bili"), strings.Contains(url, "ddit"):
								j = max(8, j)
							}
						}
						return j
					})

					if jitterInt > 0 {
						jitter := times.RandomMinsDuration(jitterInt) // Probability, with re-rolls, expect ~half the time added
						interval += jitter
					}

				}

				logger.Pl.D(2, "Crawl watchdog: channel %q - last scan: %s ago, interval: %s",
					c.Name, elapsed.Round(time.Second), interval)

				if elapsed >= interval {
					// Crawl goroutine
					go func(ch *models.Channel) {
						if state.CrawlStateActive(ch.Name) {
							return
						}

						state.LockCrawlState(ch.Name)
						defer state.UnlockCrawlState(ch.Name)

						crawlCtx, cancel := context.WithCancel(ctx)
						defer cancel()

						logger.Pl.I("Crawl watchdog: triggering scheduled crawl for channel %q", ch.Name)
						if err := app.CrawlChannel(crawlCtx, ss.s, ch); err != nil {
							logger.Pl.E("Crawl watchdog: error crawling channel %q: %v", ch.Name, err)
						} else {
							logger.Pl.S("Crawl watchdog: successfully completed crawl for channel %q", ch.Name)
						}
					}(c)
				}
			}
		}
	}
}

// getHomepageCarouselVideos returns the latest 'n' downloaded videos.
func (s *serverStore) getHomepageCarouselVideos(channel *models.Channel, n int) (videos []models.Video, err error) {
	query := fmt.Sprintf(
		"SELECT %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s "+
			"FROM %s "+
			"WHERE %s = ? AND %s = 1 "+ // Only finished downloads
			"ORDER BY %s DESC "+
			"LIMIT ?",
		consts.QVidID,
		consts.QVidChanID,
		consts.QVidChanURLID,
		consts.QVidThumbnailURL,
		consts.QVidFinished,
		consts.QVidURL,
		consts.QVidTitle,
		consts.QVidDescription,
		consts.QVidUploadDate,
		consts.QVidCreatedAt,
		consts.QVidUpdatedAt,
		consts.DBVideos,
		consts.QVidChanID,
		consts.QVidFinished,
		consts.QVidUpdatedAt, // Most recent first
	)

	rows, err := s.db.Query(query, channel.ID, n)
	if err != nil {
		return nil, fmt.Errorf("failed to query videos: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var video models.Video
		var thumbnailURL sql.NullString
		var description sql.NullString
		var uploadDate sql.NullTime
		var channelURLID sql.NullInt64

		err := rows.Scan(
			&video.ID,
			&video.ChannelID,
			&channelURLID,
			&thumbnailURL,
			&video.Finished,
			&video.URL,
			&video.Title,
			&description,
			&uploadDate,
			&video.CreatedAt,
			&video.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan video row: %w", err)
		}

		// Handle nullable fields
		if channelURLID.Valid {
			video.ChannelURLID = channelURLID.Int64
		}
		if thumbnailURL.Valid {
			video.ThumbnailURL = thumbnailURL.String
		}
		if description.Valid {
			video.Description = description.String
		}
		if uploadDate.Valid {
			video.UploadDate = uploadDate.Time
		}
		videos = append(videos, video)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return videos, nil
}

// getActiveDownloads retrieves all currently downloading videos for a specific channel from the in-memory StatusUpdate map.
//
// This provides real-time updates without hitting the database on every poll.
func (s *serverStore) getActiveDownloads(channel *models.Channel) ([]models.Video, error) {
	var videos []models.Video

	// Iterate through the in-memory StatusUpdate to find active downloads for this channel
	state.StatusUpdateCache.Range(func(key, value any) bool {
		videoID, ok := key.(int64)
		if !ok {
			logger.Pl.E("Dev Error: Invalid key type in StatusUpdate: %T", key)
			return true // continue iteration
		}

		statusUpdate, ok := value.(models.StatusUpdate)
		if !ok {
			logger.Pl.E("Dev Error: Invalid value type in StatusUpdate for video %d: %T", videoID, value)
			return true // continue iteration
		}

		// Filter by channel ID directly from StatusUpdate
		if statusUpdate.ChannelID != channel.ID {
			return true
		}

		// Only include Pending or Downloading statuses
		if statusUpdate.Status == consts.DLStatusPending || statusUpdate.Status == consts.DLStatusDownloading {
			// Build a Video model from the StatusUpdate
			video := models.Video{
				ID:           statusUpdate.VideoID,
				Title:        statusUpdate.VideoTitle,
				ChannelID:    statusUpdate.ChannelID,
				ChannelURLID: statusUpdate.ChannelURLID,
				URL:          statusUpdate.VideoURL,
				DownloadStatus: models.DLStatus{
					Status:  statusUpdate.Status,
					Percent: statusUpdate.Percent,
					Error:   statusUpdate.Error,
				},
			}
			videos = append(videos, video)
		}

		return true // continue iteration
	})

	logger.Pl.D(1, "Found %d active downloads in memory for channel %d", len(videos), channel.ID)
	return videos, nil
}

// cancelDownload marks a download as cancelled in the database.
func (s *serverStore) cancelDownload(videoID int64) error {
	query := fmt.Sprintf(
		"UPDATE %s SET %s = ?, %s = CURRENT_TIMESTAMP WHERE %s = ?",
		consts.DBDownloads,
		consts.QDLStatus,
		consts.QDLUpdatedAt,
		consts.QDLVidID,
	)

	result, err := s.db.Exec(query, consts.DLStatusCancelled, videoID)
	if err != nil {
		return fmt.Errorf("failed to cancel download: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no active download found for video ID %d", videoID)
	}

	return nil
}
