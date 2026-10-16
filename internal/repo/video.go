package repo

import (
	"cmp"
	"database/sql"
	"errors"
	"fmt"
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
// Videos without URLs or channel IDs are skipped, and errors for each are returned.
func (vs *VideoStore) AddVideos(videos []*models.Video, channelID int64) (videoModels []*models.Video, err error) {
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
		if channelID == 0 {
			errs = append(errs, fmt.Errorf("video #%d has no channel ID", i))
			continue
		}
		validVideos = append(validVideos, v)
	}

	now := time.Now()

	for _, v := range validVideos {
		videoID, exists := vs.videoExists(v, channelID)
		if exists {
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

			_, err := tx.Exec(updateQuery, v.Finished, v.Ignored, now, channelID, v.URL)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to update video %q: %w", v.URL, err))
				continue
			}
			v.ID = videoID
		} else {
			// Marshal metadata JSON
			metadataJSON, err := marshalVideoMetadataJSON(v)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to marshal metadata for video %q: %w", v.URL, err))
				continue
			}

			// Insert new video with all fields
			insertQuery := fmt.Sprintf(
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

			result, err := tx.Exec(
				insertQuery,
				channelID,
				v.ChannelURLID,
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
				errs = append(errs, fmt.Errorf("failed to insert video %q: %w", v.URL, err))
				continue
			}

			id, err := result.LastInsertId()
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to get inserted ID for video %q: %w", v.URL, err))
				continue
			}

			v.ID = id
		}

		// Insert or update download status using SQLite UPSERT (INSERT ... ON CONFLICT)
		//
		// Squirrel doesn't support ON CONFLICT clause natively
		sqlQuery := fmt.Sprintf(
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

		if _, err := tx.Exec(sqlQuery, v.ID, v.DownloadStatus.Status, v.DownloadStatus.Percent, now, now); err != nil {
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
	if id, exists := vs.videoExists(v, channelID); exists {
		logger.Pl.D(1, "Video %q already exists in the database with ID %d", v.URL, id)
		v.ID = id // UpdateVideo sets the download status by ID.
		if err := vs.UpdateVideo(v, channelID); err != nil {
			return id, fmt.Errorf("failed to update existing video: %w", err)
		}
		return id, nil
	}

	// Marshal JSON fields
	metadataJSON, err := marshalVideoMetadataJSON(v)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal JSON for video %q: %w", v.URL, err)
	}

	tx, err := vs.DB.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer rollback(tx)

	now := time.Now()

	// Insert into videos table
	vidQuery := fmt.Sprintf(
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

	result, err := tx.Exec(
		vidQuery,
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

	videoID, err = result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get inserted video ID for %q: %w", v.URL, err)
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

// UpdateVideo updates the status of the video in the database.
func (vs *VideoStore) UpdateVideo(v *models.Video, channelID int64) error {
	metadataJSON, err := marshalVideoMetadataJSON(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON for video with URL %q: %w", v.URL, err)
	}

	tx, err := vs.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer rollback(tx)

	// Update videos table
	videoQuery := fmt.Sprintf(
		"UPDATE %s SET %s = COALESCE(NULLIF(?, ''), %s), %s = COALESCE(NULLIF(?, ''), %s), "+
			"%s = ?, %s = ?, %s = ?, %s = ?, %s = ?, %s = ?, %s = ?, %s = ?, "+
			"%s = COALESCE(NULLIF(?, ''), %s) "+
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
		consts.QVidMetadata,
		consts.QVidUpdatedAt,
		consts.QVidMetarrStatus, consts.QVidMetarrStatus,
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
		metadataJSON,
		time.Now(),
		v.MetarrStatus,
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

// GetVideosAwaitingMetarr returns finished videos in a channel which Metarr has not yet successfully processed.
func (vs *VideoStore) GetVideosAwaitingMetarr(channelID int64) (videos []*models.Video, err error) {
	query := fmt.Sprintf(
		"SELECT %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s FROM %s "+
			"WHERE %s = ? AND %s = 1 AND %s = 0 AND %s != ?",
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
		consts.DBVideos,
		consts.QVidChanID,
		consts.QVidFinished,
		consts.QVidIgnored,
		consts.QVidMetarrStatus,
	)

	rows, err := vs.DB.Query(query, channelID, consts.MetarrStatusDone)
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
			videoPath, jsonPath, metarrStatus            sql.NullString
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
		); err != nil {
			return nil, fmt.Errorf("failed to scan video: %w", err)
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

// AddAvailableVideos stores newly found videos as Available, skipping any already in the channel.
//
// Existing rows are left alone, so a video being downloaded (or failed) isn't reset to Available.
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
	for _, v := range videos {
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
		"SELECT v.%s, v.%s, v.%s, v.%s, v.%s, v.%s, v.%s, v.%s, v.%s, v.%s, d.%s, c.%s "+
			"FROM %s v JOIN %s d ON d.%s = v.%s JOIN %s c ON c.%s = v.%s "+
			"WHERE v.%s = 0 AND v.%s = 0 AND d.%s IN (?%s) "+
			"ORDER BY v.%s DESC, v.%s DESC",
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
		consts.QChanName,
		consts.DBVideos,
		consts.DBDownloads, consts.QDLVidID, consts.QVidID,
		consts.DBChannels, consts.QChanID, consts.QVidChanID,
		consts.QVidFinished,
		consts.QVidIgnored,
		consts.QDLStatus, strings.Repeat(", ?", len(statuses)-1),
		consts.QVidUploadDate,
		consts.QVidCreatedAt,
	)
	args := make([]any, len(statuses))
	for i, s := range statuses {
		args[i] = s
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
			v                               models.AvailableVideo
			channelURLID                    sql.NullInt64
			channelURL, title, thumbnailURL sql.NullString
			uploadDate                      sql.NullTime
			duration                        sql.NullInt64
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
			&v.ChannelName,
		); err != nil {
			return nil, fmt.Errorf("failed to scan available video: %w", err)
		}
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
// Claiming as Ignored also marks the video ignored, so crawls skip it.
func (vs *VideoStore) ClaimVideos(channelID int64, videoIDs []int64, from []consts.DownloadStatus, status consts.DownloadStatus) (claimed []*models.Video, err error) {
	if len(videoIDs) == 0 || len(from) == 0 {
		return nil, nil
	}
	tx, err := vs.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer rollback(tx)

	claimQuery := fmt.Sprintf(
		"UPDATE %s SET %s = ?, %s = 0, %s = ? WHERE %s = ? AND %s IN (?%s) "+
			"AND %s IN (SELECT %s FROM %s WHERE %s = ? AND %s = 0 AND %s = 0)",
		consts.DBDownloads,
		consts.QDLStatus,
		consts.QDLPct,
		consts.QDLUpdatedAt,
		consts.QDLVidID,
		consts.QDLStatus, strings.Repeat(", ?", len(from)-1),
		consts.QDLVidID, consts.QVidID, consts.DBVideos, consts.QVidChanID, consts.QVidFinished, consts.QVidIgnored,
	)
	selectQuery := fmt.Sprintf(
		"SELECT %s, %s, %s FROM %s WHERE %s = ?",
		consts.QVidURL,
		consts.QVidChanURLID,
		consts.QVidChanURL,
		consts.DBVideos,
		consts.QVidID,
	)
	ignoreQuery := fmt.Sprintf(
		"UPDATE %s SET %s = 1, %s = ? WHERE %s = ?",
		consts.DBVideos,
		consts.QVidIgnored,
		consts.QVidUpdatedAt,
		consts.QVidID,
	)

	now := time.Now()
	for _, id := range videoIDs {
		args := []any{status, now, id}
		for _, s := range from {
			args = append(args, s)
		}
		result, err := tx.Exec(claimQuery, append(args, channelID)...)
		if err != nil {
			return nil, fmt.Errorf("failed to claim video %d: %w", id, err)
		}
		if n, err := result.RowsAffected(); err != nil || n == 0 {
			continue // Not available (already claimed, finished, or in another channel).
		}

		v := &models.Video{ID: id, ChannelID: channelID}
		var channelURLID sql.NullInt64
		var channelURL sql.NullString
		if err := tx.QueryRow(selectQuery, id).Scan(&v.URL, &channelURLID, &channelURL); err != nil {
			return nil, fmt.Errorf("failed to read claimed video %d: %w", id, err)
		}
		v.ChannelURLID = channelURLID.Int64
		v.ChannelURL = channelURL.String
		v.DownloadStatus.Status = status

		if status == consts.DLStatusIgnored {
			if _, err := tx.Exec(ignoreQuery, now, id); err != nil {
				return nil, fmt.Errorf("failed to ignore video %d: %w", id, err)
			}
			v.Ignored = true
		}
		claimed = append(claimed, v)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}
	return claimed, nil
}

// ******************************** Private ***************************************************************************************

// videoExists returns true if the video exists in the database.
func (vs *VideoStore) videoExists(v *models.Video, channelID int64) (int64, bool) {
	var id int64
	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = ? AND %s = ?",
		consts.QVidID,
		consts.DBVideos,
		consts.QVidURL,
		consts.QVidChanID,
	)

	err := vs.DB.QueryRow(query, v.URL, channelID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false
	} else if err != nil {
		logger.Pl.E("Error checking if video exists: %v", err)
		return 0, false
	}

	logger.Pl.D(1, "Video with URL %q already exists in the database with ID %d", v.URL, v.ID)
	return id, true
}
