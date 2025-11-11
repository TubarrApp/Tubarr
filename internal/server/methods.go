package server

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"time"
	"tubarr/internal/app"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"

	"github.com/Masterminds/squirrel"
)

// startCrawlWatchdog constantly checks channels and runs crawls when they're due.
//
// This function blocks until the stop channel is signaled or context is cancelled.
func (ss *serverStore) startCrawlWatchdog(ctx context.Context, stop <-chan os.Signal) {
	var crawlLockMap sync.Map
	const timeBetweenCheck = (2 * time.Minute) + (30 * time.Second)

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
			freshChannels, hasRows, err := ss.s.ChannelStore().GetAllChannels()
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

				logging.D(2, "Crawl watchdog: channel %q - last scan: %s ago, interval: %s",
					c.Name, elapsed.Round(time.Second), interval)

				if elapsed >= interval {

					// -- Crawl launch in goroutine --
					go func(ch *models.Channel) {
						lockStateInterface, ok := crawlLockMap.Load(ch.Name)
						if !ok {
							crawlLockMap.Store(ch.Name, false)
							if lockStateInterface, ok = crawlLockMap.Load(ch.Name); !ok {
								logging.E("Failed to reload crawlLockMap state after explicit setting to false")
								return
							}
						}
						lockState, ok := lockStateInterface.(bool)
						if !ok {
							logging.E("Wrong type %T stored for channel %q lock state", lockState, ch.Name)
							return
						}

						if !lockState {
							crawlCtx, cancel := context.WithCancel(ctx)
							defer cancel()
							crawlLockMap.Store(ch.Name, true)
							logging.I("Crawl watchdog: triggering scheduled crawl for channel %q", ch.Name)
							if err := app.CrawlChannel(crawlCtx, ss.s, ss.cs, ch); err != nil {
								logging.E("Crawl watchdog: error crawling channel %q: %v", ch.Name, err)
							} else {
								logging.S("Crawl watchdog: successfully completed crawl for channel %q", ch.Name)
							}
							crawlLockMap.Store(ch.Name, false)
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

// getActiveDownloads retrieves all currently downloading videos for a channel with their progress.
func (s *serverStore) getActiveDownloads(channel *models.Channel) ([]models.Video, error) {
	const (
		vDot = "v."
		dDot = "d."
	)
	query := squirrel.
		Select(
			vDot+consts.QVidID,
			vDot+consts.QVidChanID,
			vDot+consts.QVidChanURLID,
			vDot+consts.QVidThumbnailURL,
			vDot+consts.QVidFinished,
			vDot+consts.QVidURL,
			vDot+consts.QVidTitle,
			vDot+consts.QVidDescription,
			vDot+consts.QVidUploadDate,
			vDot+consts.QVidCreatedAt,
			vDot+consts.QVidUpdatedAt,
			dDot+consts.QDLStatus,
			dDot+consts.QDLPct,
		).
		From(consts.DBVideos + " v").
		InnerJoin(consts.DBDownloads + " d ON " + (vDot + consts.QVidID) + " = " + (dDot + consts.QDLVidID)).
		Where(squirrel.Eq{(vDot + consts.QVidChanID): channel.ID}).
		Where(squirrel.Or{
			squirrel.Eq{(dDot + consts.QDLStatus): consts.DLStatusPending},
			squirrel.Eq{(dDot + consts.QDLStatus): consts.DLStatusDownloading},
		}).
		OrderBy((dDot + consts.QDLCreatedAt) + " DESC")

	sqlPlaceholder, args, err := query.PlaceholderFormat(squirrel.Question).ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build active downloads query: %w", err)
	}

	rows, err := s.db.Query(sqlPlaceholder, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query active downloads: %w", err)
	}
	defer rows.Close()

	var videos []models.Video
	for rows.Next() {
		var video models.Video
		var thumbnailURL sql.NullString
		var description sql.NullString
		var uploadDate sql.NullTime
		var channelURLID sql.NullInt64
		var dlStatus string
		var dlPercentage float64

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
			&dlStatus,
			&dlPercentage,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan active download row: %w", err)
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

		// Set download status
		video.DownloadStatus.Status = consts.DownloadStatus(dlStatus)
		video.DownloadStatus.Pct = dlPercentage

		videos = append(videos, video)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating active download rows: %w", err)
	}
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
