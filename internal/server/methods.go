package server

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"
	"tubarr/internal/abstractions"
	"tubarr/internal/app"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/models"
	"tubarr/internal/state"
	"tubarr/internal/utils/logging"
	"tubarr/internal/utils/times"

	"github.com/Masterminds/squirrel"
)

// startCrawlWatchdog constantly checks channels and runs crawls when they're due.
//
// This function blocks until the stop channel is signaled or context is cancelled.
func (ss *serverStore) startCrawlWatchdog(ctx context.Context, stop <-chan os.Signal) {

	const timeBetweenCheck = (1 * time.Minute)

	// Start ticker
	ticker := time.NewTicker(timeBetweenCheck) // check every minute
	defer ticker.Stop()
	logging.I("Crawl watchdog started, checking every %s", timeBetweenCheck.String())

	for {
		select {
		case <-stop:
			logging.I("Crawl watchdog received stop signal, shutting down...")
			return
		case <-ctx.Done():
			logging.I("Crawl watchdog context cancelled, shutting down...")
			return
		case <-ticker.C:
			// Reload channels from database to get updated LastScan times
			freshChannels, hasRows, err := ss.s.ChannelStore().GetAllChannels(true)
			if err != nil {
				logging.E("Crawl watchdog: failed to reload channels: %v", err)
				continue
			}
			if !hasRows || len(freshChannels) == 0 {
				logging.D(2, "Crawl watchdog: no channels found in database")
				continue
			}

			logging.D(2, "Crawl watchdog: checking %d channel(s) for scheduled crawls", len(freshChannels))
			now := time.Now()
			for _, c := range freshChannels {
				// Skip paused channels
				if c.ChanSettings.Paused {
					logging.D(2, "Crawl watchdog: channel %q is paused, skipping", c.Name)
					continue
				}

				// Calculate elapsed time since last scan
				elapsed := now.Sub(c.LastScan)
				interval := time.Duration(c.ChanSettings.CrawlFreq) * time.Minute

				// Add jitter
				if !abstractions.IsSet(keys.SkipAllWaits) {
					jitterTime := 5

					// Check URLs
					for _, cURL := range c.URLModels {
						u := strings.ToLower(cURL.URL)

						switch {
						case strings.Contains(u, "tube"), strings.Contains(u, "tok"),
							strings.Contains(u, "gram"), strings.Contains(u, "book"), strings.Contains(u, "x."):
							jitterTime = max(10, jitterTime)

						case strings.Contains(u, "motion"), strings.Contains(u, "vime"),
							strings.Contains(u, "bili"), strings.Contains(u, "ddit"):
							jitterTime = max(8, jitterTime)
						}
					}

					jitter := times.RandomMinsDuration(jitterTime) // Probability, with re-rolls, expect ~half the time added
					interval += jitter
				}

				logging.D(2, "Crawl watchdog: channel %q - last scan: %s ago, interval: %s",
					c.Name, elapsed.Round(time.Second), interval)

				if elapsed >= interval {
					// -- Crawl launch in goroutine --
					go func(ch *models.Channel) {
						if state.CrawlStateActive(ch.Name) {
							return
						}

						state.LockCrawlState(ch.Name)
						defer state.UnlockCrawlState(ch.Name)

						crawlCtx, cancel := context.WithCancel(ctx)
						defer cancel()

						logging.I("Crawl watchdog: triggering scheduled crawl for channel %q", ch.Name)
						if err := app.CrawlChannel(crawlCtx, ss.s, ch); err != nil {
							logging.E("Crawl watchdog: error crawling channel %q: %v", ch.Name, err)
						} else {
							logging.S("Crawl watchdog: successfully completed crawl for channel %q", ch.Name)
						}
					}(c)
				}
			}
		}
	}
}

// getHomepageCarouselVideos returns the latest 'n' downloaded videos.
func (s *serverStore) getHomepageCarouselVideos(channel *models.Channel, n int) (videos []models.Video, err error) {
	query := squirrel.
		Select(
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
		).
		From(consts.DBVideos).
		Where(squirrel.Eq{consts.QVidChanID: channel.ID}).
		Where(squirrel.Eq{consts.QVidFinished: 1}).            // Only finished downloads
		OrderBy(fmt.Sprintf("%s DESC", consts.QVidUpdatedAt)). // Most recent first
		Limit(uint64(n))

	sqlPlaceholder, args, err := query.PlaceholderFormat(squirrel.Question).ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	rows, err := s.db.Query(sqlPlaceholder, args...)
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
	state.StatusUpdate.Range(func(key, value any) bool {
		videoID, ok := key.(int64)
		if !ok {
			logging.E("Dev Error: Invalid key type in StatusUpdate: %T", key)
			return true // continue iteration
		}

		statusUpdate, ok := value.(models.StatusUpdate)
		if !ok {
			logging.E("Dev Error: Invalid value type in StatusUpdate for video %d: %T", videoID, value)
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

	logging.D(1, "Found %d active downloads in memory for channel %d", len(videos), channel.ID)
	return videos, nil
}

// cancelDownload marks a download as cancelled in the database.
func (s *serverStore) cancelDownload(videoID int64) error {
	query := squirrel.
		Update(consts.DBDownloads).
		Set(consts.QDLStatus, consts.DLStatusCancelled).
		Set(consts.QDLUpdatedAt, "CURRENT_TIMESTAMP").
		Where(squirrel.Eq{consts.QDLVidID: videoID})

	sqlPlaceholder, args, err := query.PlaceholderFormat(squirrel.Question).ToSql()
	if err != nil {
		return fmt.Errorf("failed to build cancel download query: %w", err)
	}

	result, err := s.db.Exec(sqlPlaceholder, args...)
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
