package repo

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"

	"github.com/Masterminds/squirrel"
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
	defer func() {
		if p := recover(); p != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				logger.Pl.E("Panic rollback failed for channel %d: %v", channelID, rbErr)
			}
			panic(p)
		} else if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				logger.Pl.E("Rollback failed for channel %d (original error: %v): %v", channelID, err, rbErr)
			}
		}
	}()

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
			updateQuery := squirrel.Update(consts.DBVideos).
				Set(consts.QVidFinished, v.Finished).
				Set(consts.QVidIgnored, v.Ignored).
				Set(consts.QVidUpdatedAt, now).
				Where(squirrel.Eq{consts.QVidChanID: channelID, consts.QVidURL: v.URL}).
				RunWith(tx)

			if _, err := updateQuery.Exec(); err != nil {
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
			insertQuery := squirrel.Insert(consts.DBVideos).
				Columns(
					consts.QVidChanID,
					consts.QVidChanURLID,
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
				).
				Values(
					channelID,
					v.ChannelURLID,
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
				).
				RunWith(tx)

			result, err := insertQuery.Exec()
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
		sqlQuery := `
			INSERT INTO ` + consts.DBDownloads + ` (` + consts.QDLVidID + `, ` + consts.QDLStatus + `, ` + consts.QDLPct + `, ` + consts.QDLCreatedAt + `, ` + consts.QDLUpdatedAt + `)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(` + consts.QDLVidID + `) DO UPDATE SET
				` + consts.QDLStatus + ` = excluded.` + consts.QDLStatus + `,
				` + consts.QDLPct + ` = excluded.` + consts.QDLPct + `,
				` + consts.QDLUpdatedAt + ` = excluded.` + consts.QDLUpdatedAt + `
		`
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

	// Begin transaction
	tx, err := vs.DB.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				logger.Pl.E("Panic rollback failed for channel %d: %v", channelID, rbErr)
			}
			panic(p)
		} else if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				logger.Pl.E("Rollback failed for channel %d (original error: %v): %v", channelID, err, rbErr)
			}
		}
	}()

	now := time.Now()

	// Insert into videos table
	vidQuery := squirrel.Insert(consts.DBVideos).
		Columns(
			consts.QVidChanID,
			consts.QVidChanURLID,
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
		).
		Values(
			channelID,
			channelURLID,
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
		).
		RunWith(tx)

	result, err := vidQuery.Exec()
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

	dlSQL, dlArgs, err := squirrel.Insert(consts.DBDownloads).
		Columns(consts.QDLVidID, consts.QDLStatus, consts.QDLPct).
		Values(videoID, v.DownloadStatus.Status, v.DownloadStatus.Percent).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("failed to build download insert SQL: %w", err)
	}
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
	tx, err := vs.DB.Begin()

	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				logger.Pl.E("Panic rollback failed for channel %d: %v", channelID, rbErr)
			}
			panic(p)
		} else if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				logger.Pl.E("Rollback failed for channel %d (original error: %v): %v", channelID, err, rbErr)
			}
		}
	}()

	metadataJSON, err := marshalVideoMetadataJSON(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON for video with URL %q: %w", v.URL, err)
	}

	// Update videos table
	videoQuery := squirrel.
		Update(consts.DBVideos).
		Set(consts.QVidThumbnailURL, v.ThumbnailURL).
		Set(consts.QVidTitle, v.Title).
		Set(consts.QVidDescription, v.Description).
		Set(consts.QVidVideoPath, v.VideoPath).
		Set(consts.QVidJSONPath, v.JSONPath).
		Set(consts.QVidFinished, v.Finished).
		Set(consts.QVidIgnored, v.Ignored).
		Set(consts.QVidUploadDate, v.UploadDate).
		Set(consts.QVidMetadata, metadataJSON).
		Set(consts.QVidUpdatedAt, time.Now()).
		Where(squirrel.And{
			squirrel.Eq{consts.QVidURL: v.URL},
			squirrel.Eq{consts.QVidChanID: channelID},
		}).
		RunWith(tx)

	result, err := videoQuery.Exec()
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
	dlQuery := squirrel.
		Update(consts.DBDownloads).
		Set(consts.QDLStatus, v.DownloadStatus.Status).
		Set(consts.QDLPct, v.DownloadStatus.Percent).
		Where(squirrel.Eq{consts.QDLVidID: v.ID}).
		RunWith(tx)

	if _, err := dlQuery.Exec(); err != nil {
		return fmt.Errorf("failed to update download status: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction for video %q: %w", v.URL, err)
	}

	logger.Pl.S("Updated video with URL: %s", v.URL)
	return nil
}

// DeleteVideo deletes an existent downloaded video from the database.
func (vs *VideoStore) DeleteVideo(videoURL string, channelID int64) error {
	if videoURL == "" || channelID == 0 {
		return errors.New("needs a video URL to delete, and channel ID to delete from")
	}

	query := squirrel.
		Delete(consts.DBVideos).
		Where(squirrel.And{
			squirrel.Eq{consts.QVidURL: videoURL},
			squirrel.Eq{consts.QVidChanID: channelID},
		}).
		RunWith(vs.DB)

	if _, err := query.Exec(); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("no video exists for channel %d with URL %q", channelID, videoURL)
		}
		return err
	}
	return nil
}

// GetVideoURLByID returns a video's URL by its ID in the database.
func (vs *VideoStore) GetVideoURLByID(videoID int64) (videoURL string, err error) {
	query := squirrel.
		Select(consts.QVidURL).
		From(consts.DBVideos).
		Where(squirrel.Eq{consts.QVidID: videoID}).
		RunWith(vs.DB)

	if err = query.QueryRow().Scan(&videoURL); err != nil {
		logger.Pl.I("Could not scan video URL from database for video ID: %d, URL: %q", videoID, videoURL)
		return "", err
	}
	return videoURL, nil
}

// ******************************** Private ********************************

// videoExists returns true if the video exists in the database.
func (vs *VideoStore) videoExists(v *models.Video, channelID int64) (int64, bool) {
	var id int64
	query := squirrel.
		Select(consts.QVidID).
		From(consts.DBVideos).
		Where(squirrel.And{
			squirrel.Eq{consts.QVidURL: v.URL},
			squirrel.Eq{consts.QVidChanID: channelID},
		}).
		RunWith(vs.DB)

	err := query.QueryRow().Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false
	} else if err != nil {
		logger.Pl.E("Error checking if video exists: %v", err)
		return 0, false
	}

	logger.Pl.D(1, "Video with URL %q already exists in the database with ID %d", v.URL, v.ID)
	return id, true
}
