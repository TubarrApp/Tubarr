package repo

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
)

// VideoStore holds a pointer to the sql.DB.
type VideoStore struct {
	DB *sql.DB
}

// GetVideoStore returns a video store instance with injected database.
func GetVideoStore(db *sql.DB) *VideoStore {
	return &VideoStore{
		DB: db,
	}
}

// GetDB returns the database.
func (vs *VideoStore) GetDB() *sql.DB {
	return vs.DB
}

// AddVideos adds multiple videos to the database, returning them with filled IDs.
//
// Videos without URLs are skipped, and errors for each are returned.
func (vs *VideoStore) AddVideos(videos []*models.Video, channelID int64) (videoModels []*models.Video, err error) {
	if channelID == 0 {
		return nil, errors.New("videos have no channel ID")
	}

	tx, err := vs.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer rollback(tx)

	validVideos := make([]*models.Video, 0, len(videos))
	errs := make([]error, 0, len(videos))

	// Validate videos
	for i, v := range videos {
		if v.URL == "" {
			errs = append(errs, fmt.Errorf("video #%d must have a URL", i))
			continue
		}
		validVideos = append(validVideos, v)
	}

	// Update finished and ignored status
	updateQuery := fmt.Sprintf(
		"UPDATE %s SET %s = ?, %s = ?, %s = ? WHERE %s = ? AND %s = ?",
		consts.DBVideos,
		consts.QVidFinished,
		consts.QVidIgnored,
		consts.QVidUpdatedAt,
		consts.QVidChanID,
		consts.QVidURL,
	)

	// Insert or update download status using SQLite UPSERT (INSERT ... ON CONFLICT)
	statusQuery := fmt.Sprintf(
		"INSERT INTO %s (%s, %s, %s, %s, %s) "+
			"VALUES (?, ?, ?, ?, ?) "+
			"ON CONFLICT(%s) DO UPDATE SET "+
			"%s = excluded.%s, "+
			"%s = excluded.%s, "+
			"%s = excluded.%s",
		consts.DBDownloads,
		consts.QDLVidID,
		consts.QDLStatus,
		consts.QDLPct,
		consts.QDLCreatedAt,
		consts.QDLUpdatedAt,
		consts.QDLVidID,
		consts.QDLStatus, consts.QDLStatus,
		consts.QDLPct, consts.QDLPct,
		consts.QDLUpdatedAt, consts.QDLUpdatedAt,
	)

	now := time.Now()
	for _, v := range validVideos {
		if videoID, exists := videoExists(tx, v.URL, channelID); exists {
			if _, err := tx.Exec(updateQuery, v.Finished, v.Ignored, now, channelID, v.URL); err != nil {
				errs = append(errs, fmt.Errorf("failed to update video %q: %w", v.URL, err))
				continue
			}
			v.ID = videoID
		} else {
			id, err := insertVideo(tx, v, channelID, v.ChannelURLID, now)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			v.ID = id
		}

		if _, err := tx.Exec(statusQuery, v.ID, v.DownloadStatus.Status, v.DownloadStatus.Percent, now, now); err != nil {
			errs = append(errs, fmt.Errorf("failed to insert/update download status for video %d: %w", v.ID, err))
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		errs = append(errs, fmt.Errorf("failed to commit transaction: %w", err))
		return nil, errors.Join(errs...)
	}

	return validVideos, errors.Join(errs...)
}

// AddVideo adds a new video to the database or updates it if it already exists.
func (vs *VideoStore) AddVideo(v *models.Video, channelID, channelURLID int64) (videoID int64, err error) {
	if v.URL == "" {
		return 0, errors.New("must enter a url for video")
	}

	// Check if video already exists
	if id, exists := vs.VideoExists(v, channelID); exists {
		logger.Pl.D(1, "Video %q already exists in the database with ID %d", v.URL, id)
		v.ID = id // UpdateVideo sets the download status by ID.
		if err := vs.UpdateVideo(v, channelID); err != nil {
			return id, fmt.Errorf("failed to update existing video: %w", err)
		}
		return id, nil
	}

	tx, err := vs.DB.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer rollback(tx)

	// Insert into videos table
	if videoID, err = insertVideo(tx, v, channelID, channelURLID, time.Now()); err != nil {
		return 0, err
	}
	v.ID = videoID

	// Insert into downloads table
	logger.Pl.D(1, "Inserting download status for video %d: status=%q, pct=%.2f", videoID, v.DownloadStatus.Status, v.DownloadStatus.Percent)

	dlSQL := fmt.Sprintf(
		"INSERT INTO %s (%s, %s, %s) VALUES (?, ?, ?)",
		consts.DBDownloads,
		consts.QDLVidID,
		consts.QDLStatus,
		consts.QDLPct,
	)
	dlArgs := []any{videoID, v.DownloadStatus.Status, v.DownloadStatus.Percent}

	logger.Pl.D(1, "Download insert SQL: %s (args: %v)", dlSQL, dlArgs)

	if _, err := tx.Exec(dlSQL, dlArgs...); err != nil {
		return 0, fmt.Errorf("failed to insert download status for video %d: %w", videoID, err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction for video %q: %w", v.URL, err)
	}

	logger.Pl.D(1, "Inserted video %q with ID %d successfully", v.URL, videoID)
	return videoID, nil
}

// UpdateVideoPaths stores a video's file paths only while its row still holds oldVideo and oldJSON, so paths from a
// newer download are never overwritten. It reports whether the row was updated.
func (vs *VideoStore) UpdateVideoPaths(videoID int64, oldVideo, oldJSON, newVideo, newJSON string) (bool, error) {
	query := fmt.Sprintf(
		"UPDATE %s SET %s = ?, %s = ?, %s = ? WHERE %s = ? AND %s = ? AND %s = ?",
		consts.DBVideos,
		consts.QVidVideoPath,
		consts.QVidJSONPath,
		consts.QVidUpdatedAt,
		consts.QVidID,
		consts.QVidVideoPath,
		consts.QVidJSONPath,
	)

	result, err := vs.DB.Exec(query, newVideo, newJSON, time.Now(), videoID, oldVideo, oldJSON)
	if err != nil {
		return false, fmt.Errorf("failed to update paths for video with ID %d: %w", videoID, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to get rows affected: %w", err)
	}
	return rows > 0, nil
}

// UpdateVideo updates the status of the video in the database.
func (vs *VideoStore) UpdateVideo(v *models.Video, channelID int64) error {
	metadataJSON, err := marshalVideoMetadataJSON(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON for video with URL %q: %w", v.URL, err)
	}
	var metadataArg any // NULL without loaded metadata, keeping the stored metadata.
	if metadataJSON != nil {
		metadataArg = metadataJSON
	}

	tx, err := vs.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer rollback(tx)

	// Update videos table. Metarr's error changes only with its status, which only changes when given.
	videoQuery := fmt.Sprintf(
		"UPDATE %s SET %s = COALESCE(NULLIF(?, ''), %s), %s = COALESCE(NULLIF(?, ''), %s), "+
			"%s = ?, %s = ?, %s = ?, %s = ?, %s = ?, %s = ?, %s = COALESCE(NULLIF(?, 0), %s), %s = COALESCE(?, %s), %s = ?, "+
			"%s = COALESCE(NULLIF(?, ''), %s), %s = CASE WHEN ? = '' THEN %s ELSE ? END "+
			"WHERE %s = ? AND %s = ?",
		consts.DBVideos,
		consts.QVidThumbnailURL, consts.QVidThumbnailURL, // Keep any found by the crawl until metadata has one.
		consts.QVidTitle, consts.QVidTitle,
		consts.QVidDescription,
		consts.QVidVideoPath,
		consts.QVidJSONPath,
		consts.QVidFinished,
		consts.QVidIgnored,
		consts.QVidUploadDate,
		consts.QVidFilesize, consts.QVidFilesize, // Keep the stored size when this run didn't read one.
		consts.QVidMetadata, consts.QVidMetadata,
		consts.QVidUpdatedAt,
		consts.QVidMetarrStatus, consts.QVidMetarrStatus,
		consts.QVidMetarrError, consts.QVidMetarrError,
		consts.QVidURL,
		consts.QVidChanID,
	)

	result, err := tx.Exec(
		videoQuery,
		v.ThumbnailURL,
		v.Title,
		v.Description,
		v.VideoFilePath,
		v.JSONFilePath,
		v.Finished,
		v.Ignored,
		v.UploadDate,
		v.Filesize,
		metadataArg,
		time.Now(),
		v.MetarrStatus,
		v.MetarrStatus, v.MetarrError,
		v.URL,
		channelID,
	)
	if err != nil {
		return fmt.Errorf("failed to update video: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("no video found with URL %s", v.URL)
	}

	// Update downloads table
	dlQuery := fmt.Sprintf(
		"UPDATE %s SET %s = ?, %s = ? WHERE %s = ?",
		consts.DBDownloads,
		consts.QDLStatus,
		consts.QDLPct,
		consts.QDLVidID,
	)

	if _, err := tx.Exec(dlQuery, v.DownloadStatus.Status, v.DownloadStatus.Percent, v.ID); err != nil {
		return fmt.Errorf("failed to update download status: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction for video %q: %w", v.URL, err)
	}

	logger.Pl.S("Updated video with URL: %s", v.URL)
	return nil
}

// GetVideoURLByID returns a video's URL by its ID in the database.
func (vs *VideoStore) GetVideoURLByID(videoID int64) (videoURL string, err error) {
	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = ?",
		consts.QVidURL,
		consts.DBVideos,
		consts.QVidID,
	)

	if err = vs.DB.QueryRow(query, videoID).Scan(&videoURL); err != nil {
		logger.Pl.I("Could not scan video URL from database for video ID: %d, URL: %q", videoID, videoURL)
		return "", err
	}

	return videoURL, nil
}

// GetVideoThumbnailURL returns a video's crawled thumbnail URL by its ID in the database.
func (vs *VideoStore) GetVideoThumbnailURL(videoID int64) (thumbURL string, err error) {
	query := fmt.Sprintf(
		"SELECT COALESCE(%s, '') FROM %s WHERE %s = ?",
		consts.QVidThumbnailURL,
		consts.DBVideos,
		consts.QVidID,
	)

	if err = vs.DB.QueryRow(query, videoID).Scan(&thumbURL); err != nil {
		return "", fmt.Errorf("failed to read thumbnail URL for video %d: %w", videoID, err)
	}
	return thumbURL, nil
}

// GetVideoChannelURLID returns the ID of the channel URL a video belongs to.
func (vs *VideoStore) GetVideoChannelURLID(videoID int64) (int64, error) {
	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = ?",
		consts.QVidChanURLID,
		consts.DBVideos,
		consts.QVidID,
	)

	var channelURLID sql.NullInt64
	if err := vs.DB.QueryRow(query, videoID).Scan(&channelURLID); err != nil {
		return 0, fmt.Errorf("failed to read channel URL of video %d: %w", videoID, err)
	}
	return channelURLID.Int64, nil
}

// GetVideosAwaitingMetarr returns finished videos in a channel which Metarr has not yet successfully processed, and with
// includeDone those it has too (for re-runs).
func (vs *VideoStore) GetVideosAwaitingMetarr(channelID int64, includeDone bool) (videos []*models.Video, err error) {
	query := fmt.Sprintf(
		"SELECT %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s FROM %s "+
			"WHERE %s = ? AND %s = 1 AND %s = 0 AND (? OR %s != ?)",
		consts.QVidID,
		consts.QVidChanID,
		consts.QVidChanURLID,
		consts.QVidChanURL,
		consts.QVidURL,
		consts.QVidTitle,
		consts.QVidDescription,
		consts.QVidThumbnailURL,
		consts.QVidUploadDate,
		consts.QVidVideoPath,
		consts.QVidJSONPath,
		consts.QVidMetarrStatus,
		consts.QVidOverrides,
		consts.DBVideos,
		consts.QVidChanID,
		consts.QVidFinished,
		consts.QVidIgnored,
		consts.QVidMetarrStatus,
	)

	rows, err := vs.DB.Query(query, channelID, includeDone, consts.MetarrStatusDone)
	if err != nil {
		return nil, fmt.Errorf("failed to query videos awaiting Metarr: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Pl.E("Failed to close rows: %v", closeErr)
		}
	}()

	for rows.Next() {
		var (
			v                                            models.Video
			channelURLID                                 sql.NullInt64
			channelURL, title, description, thumbnailURL sql.NullString
			videoPath, jsonPath, metarrStatus, overrides sql.NullString
			uploadDate                                   sql.NullTime
		)
		if err := rows.Scan(
			&v.ID,
			&v.ChannelID,
			&channelURLID,
			&channelURL,
			&v.URL,
			&title,
			&description,
			&thumbnailURL,
			&uploadDate,
			&videoPath,
			&jsonPath,
			&metarrStatus,
			&overrides,
		); err != nil {
			return nil, fmt.Errorf("failed to scan video: %w", err)
		}
		if v.Overrides, err = unmarshalVideoOverrides(overrides); err != nil {
			return nil, fmt.Errorf("failed to read overrides for video %q: %w", v.URL, err)
		}

		v.ChannelURLID = channelURLID.Int64
		v.ChannelURL = channelURL.String
		v.Title = title.String
		v.Description = description.String
		v.ThumbnailURL = thumbnailURL.String
		v.UploadDate = uploadDate.Time
		v.VideoFilePath = videoPath.String
		v.JSONFilePath = jsonPath.String
		v.MetarrStatus = consts.MetarrStatus(metarrStatus.String)
		v.Finished = true

		videos = append(videos, &v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed while retrieving videos: %w", err)
	}

	return videos, nil
}

// GetLatestFinishedVideos returns a channel's n most recently updated finished videos, without their metadata.
func (vs *VideoStore) GetLatestFinishedVideos(ctx context.Context, channelID int64, n int) ([]*models.Video, error) {
	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = ? AND %s = 1 ORDER BY %s DESC LIMIT ?",
		videoListColumns,
		consts.DBVideos,
		consts.QVidChanID,
		consts.QVidFinished,
		consts.QVidUpdatedAt,
	)

	rows, err := vs.DB.QueryContext(ctx, query, channelID, n)
	if err != nil {
		return nil, fmt.Errorf("failed to query videos: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Pl.E("Failed to close rows: %v", closeErr)
		}
	}()
	return scanVideoList(rows)
}

// CountFinishedVideos returns how many finished videos a channel has.
func (vs *VideoStore) CountFinishedVideos(ctx context.Context, channelID int64) (n int, err error) {
	query := fmt.Sprintf(
		"SELECT COUNT(*) FROM %s WHERE %s = ? AND %s = 1",
		consts.DBVideos,
		consts.QVidChanID,
		consts.QVidFinished,
	)
	if err := vs.DB.QueryRowContext(ctx, query, channelID).Scan(&n); err != nil {
		return 0, fmt.Errorf("failed to count finished videos: %w", err)
	}
	return n, nil
}

// CountFailedMetarrVideos returns how many downloaded videos Metarr failed on, by channel ID.
func (vs *VideoStore) CountFailedMetarrVideos(ctx context.Context) (counts map[int64]int, err error) {
	query := fmt.Sprintf(
		"SELECT %s, COUNT(*) FROM %s WHERE %s = 1 AND %s = 0 AND %s = ? GROUP BY %s",
		consts.QVidChanID,
		consts.DBVideos,
		consts.QVidFinished,
		consts.QVidIgnored,
		consts.QVidMetarrStatus,
		consts.QVidChanID,
	)
	rows, err := vs.DB.QueryContext(ctx, query, consts.MetarrStatusFailed)
	if err != nil {
		return nil, fmt.Errorf("failed to count videos Metarr failed on: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Pl.E("Failed to close rows: %v", closeErr)
		}
	}()

	counts = make(map[int64]int)
	for rows.Next() {
		var channelID int64
		var n int
		if err := rows.Scan(&channelID, &n); err != nil {
			return nil, fmt.Errorf("failed to scan failed Metarr count: %w", err)
		}
		counts[channelID] = n
	}
	return counts, rows.Err()
}

// AddAvailableVideos stores newly found videos as Available, skipping any already in the channel.
//
// Videos are given in the channel's listing order and inserted last first, so GetAvailableVideos' ID tie-breaker keeps
// that order. Existing rows are left alone, so a video being downloaded (or failed) isn't reset to Available.
func (vs *VideoStore) AddAvailableVideos(videos []*models.Video, channelID int64) (addedURLs []string, err error) {
	tx, err := vs.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer rollback(tx)

	vidQuery := fmt.Sprintf(
		"INSERT INTO %s (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(%s, %s) DO NOTHING",
		consts.DBVideos,
		consts.QVidChanID,
		consts.QVidChanURLID,
		consts.QVidChanURL,
		consts.QVidThumbnailURL,
		consts.QVidURL,
		consts.QVidTitle,
		consts.QVidUploadDate,
		consts.QVidDuration,
		consts.QVidCreatedAt,
		consts.QVidUpdatedAt,
		consts.QVidChanID, consts.QVidURL,
	)
	dlQuery := fmt.Sprintf(
		"INSERT INTO %s (%s, %s) VALUES (?, ?)",
		consts.DBDownloads,
		consts.QDLVidID,
		consts.QDLStatus,
	)

	now := time.Now()
	for _, v := range slices.Backward(videos) {
		if v.URL == "" {
			continue
		}
		v.MarkVideoAsAvailable()

		var uploadDate any
		if !v.UploadDate.IsZero() {
			uploadDate = v.UploadDate
		}
		result, err := tx.Exec(vidQuery, channelID, v.ChannelURLID, v.ChannelURL, v.ThumbnailURL, v.URL, v.Title, uploadDate, v.Duration, now, now)
		if err != nil {
			return nil, fmt.Errorf("failed to insert available video %q: %w", v.URL, err)
		}
		if n, err := result.RowsAffected(); err != nil || n == 0 {
			continue // Already in the channel.
		}
		if v.ID, err = result.LastInsertId(); err != nil {
			return nil, fmt.Errorf("failed to get inserted ID for video %q: %w", v.URL, err)
		}
		if _, err := tx.Exec(dlQuery, v.ID, consts.DLStatusAvailable); err != nil {
			return nil, fmt.Errorf("failed to set available status for video %q: %w", v.URL, err)
		}
		addedURLs = append(addedURLs, v.URL)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}
	return addedURLs, nil
}

// GetAvailableVideos returns all channels' unfinished videos with the given statuses, newest first.
func (vs *VideoStore) GetAvailableVideos(statuses ...consts.DownloadStatus) (videos []*models.AvailableVideo, err error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	query := fmt.Sprintf(
		"SELECT v.%s, v.%s, v.%s, v.%s, v.%s, v.%s, v.%s, v.%s, v.%s, v.%s, d.%s, d.%s, c.%s "+
			"FROM %s v JOIN %s d ON d.%s = v.%s JOIN %s c ON c.%s = v.%s "+
			"WHERE v.%s = 0 AND (v.%s = 0 OR d.%s = ?) AND d.%s IN (%s) "+
			"ORDER BY v.%s DESC, v.%s DESC, v.%s DESC",
		consts.QVidID,
		consts.QVidChanID,
		consts.QVidChanURLID,
		consts.QVidChanURL,
		consts.QVidURL,
		consts.QVidTitle,
		consts.QVidThumbnailURL,
		consts.QVidUploadDate,
		consts.QVidDuration,
		consts.QVidCreatedAt,
		consts.QDLStatus,
		consts.QDLError,
		consts.QChanName,
		consts.DBVideos,
		consts.DBDownloads, consts.QDLVidID, consts.QVidID,
		consts.DBChannels, consts.QChanID, consts.QVidChanID,
		consts.QVidFinished,
		consts.QVidIgnored, consts.QDLStatus, // Ignored videos are listed only when asked for by their status.
		consts.QDLStatus, placeholders(len(statuses)),
		consts.QVidUploadDate,
		consts.QVidCreatedAt,
		consts.QVidID,
	)
	args := make([]any, 0, len(statuses)+1)
	args = append(args, consts.DLStatusIgnored)
	for _, s := range statuses {
		args = append(args, s)
	}

	rows, err := vs.DB.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query available videos: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Pl.E("Failed to close rows: %v", closeErr)
		}
	}()

	for rows.Next() {
		var (
			v                                         models.AvailableVideo
			channelURLID                              sql.NullInt64
			channelURL, title, thumbnailURL, errorMsg sql.NullString
			uploadDate                                sql.NullTime
			duration                                  sql.NullInt64
		)
		if err := rows.Scan(
			&v.ID,
			&v.ChannelID,
			&channelURLID,
			&channelURL,
			&v.URL,
			&title,
			&thumbnailURL,
			&uploadDate,
			&duration,
			&v.CreatedAt,
			&v.DownloadStatus.Status,
			&errorMsg,
			&v.ChannelName,
		); err != nil {
			return nil, fmt.Errorf("failed to scan available video: %w", err)
		}
		v.DownloadStatus.ErrorMsg = errorMsg.String
		v.ChannelURLID = channelURLID.Int64
		v.ChannelURL = channelURL.String
		v.Title = cmp.Or(title.String, v.URL)
		v.ThumbnailURL = thumbnailURL.String
		v.UploadDate = uploadDate.Time
		v.Duration = int(duration.Int64)
		videos = append(videos, &v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed while retrieving available videos: %w", err)
	}
	return videos, nil
}

// ClaimVideos moves a channel's given unfinished videos with a status in from to status, returning those moved.
//
// Each video is claimed by a conditional update, so concurrent requests can't both claim it.
// Claiming as Ignored also marks the video ignored, so crawls skip it. Claiming from only Ignored restores ignored videos.
func (vs *VideoStore) ClaimVideos(channelID int64, videoIDs []int64, from []consts.DownloadStatus, status consts.DownloadStatus) (claimed []*models.Video, err error) {
	if len(videoIDs) == 0 || len(from) == 0 {
		return nil, nil
	}
	restoring := slices.Equal(from, []consts.DownloadStatus{consts.DLStatusIgnored})

	tx, err := vs.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer rollback(tx)

	claimQuery := fmt.Sprintf(
		"UPDATE %s SET %s = ?, %s = 0, %s = CASE WHEN ? THEN '' ELSE %s END, %s = ? WHERE %s = ? AND %s IN (%s) "+
			"AND %s IN (SELECT %s FROM %s WHERE %s = ? AND %s = 0 AND %s = ?)",
		consts.DBDownloads,
		consts.QDLStatus,
		consts.QDLPct,
		consts.QDLError, consts.QDLError, // A new download attempt clears the last one's error.
		consts.QDLUpdatedAt,
		consts.QDLVidID,
		consts.QDLStatus, placeholders(len(from)),
		consts.QDLVidID, consts.QVidID, consts.DBVideos, consts.QVidChanID, consts.QVidFinished, consts.QVidIgnored,
	)
	selectQuery := fmt.Sprintf(
		"SELECT %s, %s, %s, %s FROM %s WHERE %s = ?",
		consts.QVidURL,
		consts.QVidChanURLID,
		consts.QVidChanURL,
		consts.QVidTitle,
		consts.DBVideos,
		consts.QVidID,
	)
	ignoreQuery := fmt.Sprintf(
		"UPDATE %s SET %s = ?, %s = ? WHERE %s = ?",
		consts.DBVideos,
		consts.QVidIgnored,
		consts.QVidUpdatedAt,
		consts.QVidID,
	)

	now := time.Now()
	for _, id := range videoIDs {
		args := []any{status, status == consts.DLStatusQueued, now, id}
		for _, s := range from {
			args = append(args, s)
		}
		result, err := tx.Exec(claimQuery, append(args, channelID, restoring)...)
		if err != nil {
			return nil, fmt.Errorf("failed to claim video %d: %w", id, err)
		}
		if n, err := result.RowsAffected(); err != nil || n == 0 {
			continue // Not available (already claimed, finished, or in another channel).
		}

		v := &models.Video{ID: id, ChannelID: channelID}
		var channelURLID sql.NullInt64
		var channelURL, title sql.NullString
		if err := tx.QueryRow(selectQuery, id).Scan(&v.URL, &channelURLID, &channelURL, &title); err != nil {
			return nil, fmt.Errorf("failed to read claimed video %d: %w", id, err)
		}
		v.ChannelURLID = channelURLID.Int64
		v.ChannelURL = channelURL.String
		v.Title = title.String
		v.DownloadStatus.Status = status

		if status == consts.DLStatusIgnored || restoring {
			v.Ignored = status == consts.DLStatusIgnored
			if _, err := tx.Exec(ignoreQuery, v.Ignored, now, id); err != nil {
				return nil, fmt.Errorf("failed to set ignored for video %d: %w", id, err)
			}
		}
		claimed = append(claimed, v)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}
	return claimed, nil
}

// SetQueuedVideoError stores why a channel's queued video wasn't downloaded, shown once it's back in the Available list.
func (vs *VideoStore) SetQueuedVideoError(channelID int64, videoURL, errMsg string) error {
	query := fmt.Sprintf(
		"UPDATE %s SET %s = ? WHERE %s = ? AND %s IN (SELECT %s FROM %s WHERE %s = ? AND %s = ?)",
		consts.DBDownloads,
		consts.QDLError,
		consts.QDLStatus,
		consts.QDLVidID, consts.QVidID, consts.DBVideos, consts.QVidChanID, consts.QVidURL,
	)
	if _, err := vs.DB.Exec(query, errMsg, consts.DLStatusQueued, channelID, videoURL); err != nil {
		return fmt.Errorf("failed to store error for video %q: %w", videoURL, err)
	}
	return nil
}

// SetMissingVideoErrors stores errMsg as the error of the given videos which have none.
func (vs *VideoStore) SetMissingVideoErrors(videoIDs []int64, errMsg string) error {
	tx, err := vs.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer rollback(tx)

	query := fmt.Sprintf(
		"UPDATE %s SET %s = ? WHERE %s = ? AND COALESCE(%s, '') = ''",
		consts.DBDownloads,
		consts.QDLError,
		consts.QDLVidID,
		consts.QDLError,
	)
	for _, id := range videoIDs {
		if _, err := tx.Exec(query, errMsg, id); err != nil {
			return fmt.Errorf("failed to store error for video %d: %w", id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// ReconnectOrphanedVideos links videos whose channel URL was deleted to the one matching their recorded address.
func (vs *VideoStore) ReconnectOrphanedVideos() (reconnected int64, err error) {
	type channelURLKey struct {
		channelID int64
		addr      string
	}
	type target struct {
		id  int64
		url string
	}
	matchKey := func(u string) string {
		u = strings.ToLower(strings.TrimSpace(u))
		if _, rest, ok := strings.Cut(u, "://"); ok {
			u = rest
		}
		u = strings.TrimPrefix(strings.TrimPrefix(u, "www."), "m.")
		return strings.TrimRight(u, "/")
	}

	tx, err := vs.DB.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer rollback(tx)

	targets := map[channelURLKey]*target{} // nil where two of a channel's URLs match the same address.
	rows, err := tx.Query(fmt.Sprintf("SELECT %s, %s, %s FROM %s WHERE %s = 0",
		consts.QChanURLID, consts.QChanURLChannelID, consts.QChanURLURL, consts.DBChannelURLs, consts.QChanURLIsManual))
	if err != nil {
		return 0, fmt.Errorf("failed to read channel URLs: %w", err)
	}
	for rows.Next() {
		var t target
		var channelID int64
		if err := rows.Scan(&t.id, &channelID, &t.url); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("failed to read channel URL: %w", err)
		}
		key := channelURLKey{channelID, matchKey(t.url)}
		if _, dup := targets[key]; dup {
			targets[key] = nil
			continue
		}
		targets[key] = &t
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return 0, fmt.Errorf("failed to read channel URLs: %w", err)
	}

	type orphan struct {
		id, channelID int64
		addr          string
	}
	var orphans []orphan
	rows, err = tx.Query(fmt.Sprintf("SELECT %s, %s, %s FROM %s WHERE %s IS NULL AND COALESCE(%s, '') != ''",
		consts.QVidID, consts.QVidChanID, consts.QVidChanURL, consts.DBVideos, consts.QVidChanURLID, consts.QVidChanURL))
	if err != nil {
		return 0, fmt.Errorf("failed to read videos without a channel URL: %w", err)
	}
	for rows.Next() {
		var o orphan
		if err := rows.Scan(&o.id, &o.channelID, &o.addr); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("failed to read video without a channel URL: %w", err)
		}
		orphans = append(orphans, o)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return 0, fmt.Errorf("failed to read videos without a channel URL: %w", err)
	}

	update := fmt.Sprintf("UPDATE %s SET %s = ?, %s = ? WHERE %s = ?", consts.DBVideos, consts.QVidChanURLID, consts.QVidChanURL, consts.QVidID)
	for _, o := range orphans {
		t := targets[channelURLKey{o.channelID, matchKey(o.addr)}]
		if t == nil {
			continue
		}
		if _, err := tx.Exec(update, t.id, t.url, o.id); err != nil {
			return 0, fmt.Errorf("failed to reconnect video %d to channel URL %q: %w", o.id, t.url, err)
		}
		reconnected++
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}
	return reconnected, nil
}

// RecoverInterrupted repairs videos a stopped Tubarr left mid-download or mid-Metarr, for startup.
//
// Downloaded videos with a stored file become finished with Metarr pending, the rest Failed so they return to Available.
func (vs *VideoStore) RecoverInterrupted() (pending, failed int64, err error) {
	tx, err := vs.DB.Begin()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer rollback(tx)

	if pending, err = vs.recoverInterruptedPending(tx); err != nil {
		return 0, 0, err
	}

	if failed, err = vs.recoverInterruptedFailed(tx); err != nil {
		return 0, 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("failed to commit transaction: %w", err)
	}
	return pending, failed, nil
}

// VideoExists returns the video's ID and true if the channel already has a video with its URL.
func (vs *VideoStore) VideoExists(v *models.Video, channelID int64) (int64, bool) {
	return videoExists(vs.DB, v.URL, channelID)
}

// GetVideoOverrides returns a video's Download As overrides, nil if it has none.
func (vs *VideoStore) GetVideoOverrides(videoID int64) (*models.VideoOverrides, error) {
	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = ?",
		consts.QVidOverrides,
		consts.DBVideos,
		consts.QVidID,
	)

	var overrides sql.NullString
	if err := vs.DB.QueryRow(query, videoID).Scan(&overrides); err != nil {
		return nil, fmt.Errorf("failed to read overrides for video %d: %w", videoID, err)
	}
	return unmarshalVideoOverrides(overrides)
}

// ThumbnailURLs returns every crawled thumbnail URL videos use.
func (vs *VideoStore) ThumbnailURLs() ([]string, error) {
	query := fmt.Sprintf(
		"SELECT DISTINCT %s FROM %s WHERE %s IS NOT NULL AND %s != ''",
		consts.QVidThumbnailURL, consts.DBVideos, consts.QVidThumbnailURL, consts.QVidThumbnailURL,
	)
	rows, err := vs.DB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to read thumbnail URLs: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Pl.E("Failed to close rows for thumbnail URLs: %v", err)
		}
	}()

	var thumbURLs []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, fmt.Errorf("failed to read thumbnail URL: %w", err)
		}
		thumbURLs = append(thumbURLs, u)
	}
	return thumbURLs, rows.Err()
}

// ThumbnailOverrides returns the thumbnail choices of videos whose Download As overrides set or strip one, by video ID.
func (vs *VideoStore) ThumbnailOverrides() (map[int64]models.VideoOverrides, error) {
	query := fmt.Sprintf(
		"SELECT %s, COALESCE(json_extract(%s, '$.thumbnail'), ''), COALESCE(json_extract(%s, '$.strip_thumbnail'), 0) FROM %s "+
			"WHERE json_extract(%s, '$.thumbnail') IS NOT NULL OR json_extract(%s, '$.strip_thumbnail')",
		consts.QVidID, consts.QVidOverrides, consts.QVidOverrides, consts.DBVideos, consts.QVidOverrides, consts.QVidOverrides,
	)
	rows, err := vs.DB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to read thumbnail overrides: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Pl.E("Failed to close rows for thumbnail overrides: %v", err)
		}
	}()

	overrides := map[int64]models.VideoOverrides{}
	for rows.Next() {
		var id int64
		var o models.VideoOverrides
		if err := rows.Scan(&id, &o.Thumbnail, &o.StripThumbnail); err != nil {
			return nil, fmt.Errorf("failed to read thumbnail override: %w", err)
		}
		overrides[id] = o
	}
	return overrides, rows.Err()
}

// SetVideoOverrides stores the given videos' Download As overrides, clearing them if o is nil.
func (vs *VideoStore) SetVideoOverrides(videoIDs []int64, o *models.VideoOverrides) error {
	var overrides any // NULL when cleared.
	if o != nil {
		b, err := json.Marshal(o)
		if err != nil {
			return fmt.Errorf("failed to marshal overrides: %w", err)
		}
		overrides = string(b)
	}

	tx, err := vs.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer rollback(tx)

	query := fmt.Sprintf(
		"UPDATE %s SET %s = ? WHERE %s = ?",
		consts.DBVideos,
		consts.QVidOverrides,
		consts.QVidID,
	)
	for _, id := range videoIDs {
		if _, err := tx.Exec(query, overrides, id); err != nil {
			return fmt.Errorf("failed to store overrides for video %d: %w", id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// ******************************** Private ***************************************************************************************

// recoverInterruptedPending marks unfinished videos with a finished download and a stored file as finished, Metarr pending.
func (vs *VideoStore) recoverInterruptedPending(tx *sql.Tx) (pending int64, err error) {
	pendingQuery := fmt.Sprintf(
		"UPDATE %s SET %s = 1, %s = ?, %s = ? WHERE %s = 0 AND %s = 0 AND COALESCE(%s, '') != '' "+
			"AND %s IN (SELECT %s FROM %s WHERE %s = ?)",
		consts.DBVideos,
		consts.QVidFinished, consts.QVidMetarrStatus, consts.QVidUpdatedAt,
		consts.QVidFinished, consts.QVidIgnored, consts.QVidVideoPath,
		consts.QVidID, consts.QDLVidID, consts.DBDownloads, consts.QDLStatus,
	)
	result, err := tx.Exec(pendingQuery, consts.MetarrStatusPending, time.Now(), consts.DLStatusCompleted)
	if err != nil {
		return 0, fmt.Errorf("failed to mark interrupted Metarr runs pending: %w", err)
	}
	if pending, err = result.RowsAffected(); err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return pending, nil
}

// recoverInterruptedFailed marks unfinished videos still Queued, Downloading or with a finished download as Failed.
//
// Run after recoverInterruptedPending, so the finished downloads left are the ones with no stored file.
func (vs *VideoStore) recoverInterruptedFailed(tx *sql.Tx) (failed int64, err error) {
	failedQuery := fmt.Sprintf(
		"UPDATE %s SET %s = ?, %s = ?, %s = CURRENT_TIMESTAMP WHERE %s IN (?, ?, ?) "+
			"AND %s IN (SELECT %s FROM %s WHERE %s = 0 AND %s = 0)",
		consts.DBDownloads,
		consts.QDLStatus, consts.QDLError, consts.QDLUpdatedAt,
		consts.QDLStatus,
		consts.QDLVidID, consts.QVidID, consts.DBVideos, consts.QVidFinished, consts.QVidIgnored,
	)
	result, err := tx.Exec(failedQuery, consts.DLStatusFailed, "Tubarr stopped before this video finished",
		consts.DLStatusQueued, consts.DLStatusDownloading, consts.DLStatusCompleted)
	if err != nil {
		return 0, fmt.Errorf("failed to mark interrupted downloads failed: %w", err)
	}
	if failed, err = result.RowsAffected(); err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return failed, nil
}

// unmarshalVideoOverrides decodes a stored overrides column, nil if it is unset.
func unmarshalVideoOverrides(raw sql.NullString) (*models.VideoOverrides, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var o models.VideoOverrides
	if err := json.Unmarshal([]byte(raw.String), &o); err != nil {
		return nil, err
	}
	return &o, nil
}

// videoExists returns the ID and true if the channel has a video with videoURL, looked up through q.
func videoExists(q querier, videoURL string, channelID int64) (int64, bool) {
	var id int64
	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = ? AND %s = ?",
		consts.QVidID,
		consts.DBVideos,
		consts.QVidURL,
		consts.QVidChanID,
	)

	err := q.QueryRow(query, videoURL, channelID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false
	} else if err != nil {
		logger.Pl.E("Error checking if video exists: %v", err)
		return 0, false
	}

	logger.Pl.D(1, "Video with URL %q already exists in the database with ID %d", videoURL, id)
	return id, true
}

// insertVideo adds v as a new row, returning its ID.
func insertVideo(q querier, v *models.Video, channelID, channelURLID int64, now time.Time) (int64, error) {
	metadataJSON, err := marshalVideoMetadataJSON(v)
	if err != nil {
		return 0, err
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		consts.DBVideos,
		consts.QVidChanID,
		consts.QVidChanURLID,
		consts.QVidChanURL,
		consts.QVidThumbnailURL,
		consts.QVidURL,
		consts.QVidTitle,
		consts.QVidDescription,
		consts.QVidFinished,
		consts.QVidIgnored,
		consts.QVidUploadDate,
		consts.QVidMetadata,
		consts.QVidCreatedAt,
		consts.QVidUpdatedAt,
	)

	result, err := q.Exec(
		query,
		channelID,
		channelURLID,
		v.ChannelURL,
		v.ThumbnailURL,
		v.URL,
		v.Title,
		v.Description,
		v.Finished,
		v.Ignored,
		v.UploadDate,
		metadataJSON,
		now,
		now,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to insert video %q: %w", v.URL, err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get inserted ID for video %q: %w", v.URL, err)
	}
	return id, nil
}

// videoListColumns are the columns scanVideoList reads, in its order.
var videoListColumns = strings.Join([]string{
	consts.QVidID,
	consts.QVidChanID,
	consts.QVidChanURLID,
	consts.QVidChanURL,
	consts.QVidThumbnailURL,
	consts.QVidFinished,
	consts.QVidIgnored,
	consts.QVidURL,
	consts.QVidTitle,
	consts.QVidDescription,
	consts.QVidUploadDate,
	consts.QVidMetarrStatus,
	consts.QVidMetarrError,
	consts.QVidVideoPath,
	consts.QVidJSONPath,
	consts.QVidDuration,
	consts.QVidFilesize,
	consts.QVidCreatedAt,
	consts.QVidUpdatedAt,
}, ", ")

// scanVideoList reads rows selected with videoListColumns into videos, without their metadata.
func scanVideoList(rows *sql.Rows) ([]*models.Video, error) {
	var videos []*models.Video
	for rows.Next() {
		var (
			v                                         models.Video
			channelURLID, duration, filesize          sql.NullInt64
			channelURL, thumbnailURL, videoURL, title sql.NullString
			description, metarrStatus, metarrError    sql.NullString
			videoPath, jsonPath                       sql.NullString
			uploadDate                                sql.NullTime
		)
		if err := rows.Scan(
			&v.ID,
			&v.ChannelID,
			&channelURLID,
			&channelURL,
			&thumbnailURL,
			&v.Finished,
			&v.Ignored,
			&videoURL,
			&title,
			&description,
			&uploadDate,
			&metarrStatus,
			&metarrError,
			&videoPath,
			&jsonPath,
			&duration,
			&filesize,
			&v.CreatedAt,
			&v.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan video: %w", err)
		}
		v.ChannelURLID = channelURLID.Int64
		v.ChannelURL = channelURL.String
		v.ThumbnailURL = thumbnailURL.String
		v.URL = videoURL.String
		v.Title = title.String
		v.Description = description.String
		v.UploadDate = uploadDate.Time
		v.MetarrStatus = consts.MetarrStatus(metarrStatus.String)
		v.MetarrError = metarrError.String
		v.VideoFilePath = videoPath.String
		v.JSONFilePath = jsonPath.String // Edit & Re-Run needs the metadata file.
		v.Duration = int(duration.Int64)
		v.Filesize = filesize.Int64

		videos = append(videos, &v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed while retrieving videos: %w", err)
	}
	return videos, nil
}
