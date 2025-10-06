package repo

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"

	"github.com/Masterminds/squirrel"
)

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
func (vs *VideoStore) AddVideos(videos []*models.Video, c *models.Channel) ([]*models.Video, []error) {
	var errArray []error
	tx, err := vs.DB.Begin()
	if err != nil {
		return nil, []error{fmt.Errorf("failed to begin transaction: %w", err)}
	}

	committed := false
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				logging.E(0, "Error rolling back transaction: %v", rollbackErr)
			}
		}
	}()

	validVideos := make([]*models.Video, 0, len(videos))

	// Validate videos
	for i, v := range videos {
		if v.URL == "" {
			errArray = append(errArray, fmt.Errorf("video #%d must have a URL", i))
			continue
		}
		if c.ID == 0 {
			errArray = append(errArray, fmt.Errorf("video #%d has no channel ID", i))
			continue
		}
		validVideos = append(validVideos, v)
	}

	for _, v := range validVideos {
		videoID, exists := vs.videoExists(v, c)
		if exists {
			// Update finished status only
			updateQuery := squirrel.Update(consts.DBVideos).
				Set(consts.QVidFinished, v.Finished).
				Where(squirrel.Eq{consts.QVidChanID: c.ID, consts.QVidURL: v.URL}).
				RunWith(tx)

			if _, err := updateQuery.Exec(); err != nil {
				errArray = append(errArray, fmt.Errorf("failed to update video %q: %w", v.URL, err))
				continue
			}
			v.ID = videoID
		} else {
			// Insert new video
			insertQuery := squirrel.Insert(consts.DBVideos).
				Columns(
					consts.QVidChanID,
					consts.QVidChanURLID,
					consts.QVidURL,
					consts.QVidFinished,
				).
				Values(
					c.ID,
					v.ChannelURLID,
					v.URL,
					v.Finished,
				).
				RunWith(tx)

			result, err := insertQuery.Exec()
			if err != nil {
				errArray = append(errArray, fmt.Errorf("failed to insert video %q: %w", v.URL, err))
				continue
			}

			id, err := result.LastInsertId()
			if err != nil {
				errArray = append(errArray, fmt.Errorf("failed to get inserted ID for video %q: %w", v.URL, err))
				continue
			}
			v.ID = id
		}

		// Insert or update download status
		dlQuery := squirrel.Insert(consts.DBDownloads).
			Columns(consts.QDLVidID, consts.QDLStatus, consts.QDLPct).
			Values(v.ID, v.DownloadStatus.Status, v.DownloadStatus.Pct).
			RunWith(tx)

		if _, err := dlQuery.Exec(); err != nil {
			errArray = append(errArray, fmt.Errorf("failed to insert download status for video %d: %w", v.ID, err))
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, append(errArray, fmt.Errorf("failed to commit transaction: %w", err))
	}
	committed = true

	return validVideos, errArray
}

// AddVideo adds a new video to the database or updates it if it already exists.
func (vs *VideoStore) AddVideo(v *models.Video, c *models.Channel) (videoID int64, err error) {
	if v.URL == "" {
		return 0, errors.New("must enter a url for video")
	}

	// Check if video already exists
	if id, exists := vs.videoExists(v, c); exists {
		logging.D(1, "Video %q already exists in the database with ID %d", v.URL, id)
		if err := vs.UpdateVideo(v, c); err != nil {
			return id, fmt.Errorf("failed to update existing video: %w", err)
		}
		return id, nil
	}

	// Marshal JSON fields
	metadataJSON, settingsJSON, metarrJSON, err := marshalVideoJSON(v)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal JSON for video %q: %w", v.URL, err)
	}

	// Begin transaction
	tx, err := vs.DB.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				logging.E(0, "Error rolling back transaction for video %q: %v", v.URL, rollbackErr)
			}
		}
	}()

	now := time.Now()

	// Insert into videos table
	vidQuery := squirrel.Insert(consts.DBVideos).
		Columns(
			consts.QVidChanID,
			consts.QVidChanURLID,
			consts.QVidURL,
			consts.QVidTitle,
			consts.QVidDescription,
			consts.QVidFinished,
			consts.QVidUploadDate,
			consts.QVidMetadata,
			consts.QVidSettings,
			consts.QVidMetarr,
			consts.QVidCreatedAt,
			consts.QVidUpdatedAt,
		).
		Values(
			c.ID,
			v.ChannelURLID,
			v.URL,
			v.Title,
			v.Description,
			v.Finished,
			v.UploadDate,
			metadataJSON,
			settingsJSON,
			metarrJSON,
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
	dlQuery := squirrel.Insert(consts.DBDownloads).
		Columns(consts.QDLVidID, consts.QDLStatus, consts.QDLPct).
		Values(videoID, v.DownloadStatus.Status, v.DownloadStatus.Pct).
		RunWith(tx)

	if _, err := dlQuery.Exec(); err != nil {
		return 0, fmt.Errorf("failed to insert download status for video %d: %w", videoID, err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction for video %q: %w", v.URL, err)
	}
	committed = true

	logging.D(1, "Inserted video %q with ID %d successfully", v.URL, videoID)
	return videoID, nil
}

// UpdateVideo updates the status of the video in the database.
func (vs *VideoStore) UpdateVideo(v *models.Video, c *models.Channel) error {
	var committed bool
	tx, err := vs.DB.Begin()

	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				logging.E(0, "Error rolling back download status for video with URL %s: %v", v.URL, rollbackErr)
			}
		}
	}()

	metadataJSON, settingsJSON, metarrJSON, err := marshalVideoJSON(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON for video with URL %q: %w", v.URL, err)
	}

	// Update videos table
	videoQuery := squirrel.
		Update(consts.DBVideos).
		Set(consts.QVidTitle, v.Title).
		Set(consts.QVidDescription, v.Description).
		Set(consts.QVidVideoPath, v.VideoPath).
		Set(consts.QVidJSONPath, v.JSONPath).
		Set(consts.QVidFinished, v.Finished).
		Set(consts.QVidUploadDate, v.UploadDate).
		Set(consts.QVidMetadata, metadataJSON).
		Set(consts.QVidSettings, settingsJSON).
		Set(consts.QVidMetarr, metarrJSON).
		Set(consts.QVidUpdatedAt, time.Now()).
		Where(squirrel.And{
			squirrel.Eq{consts.QVidURL: v.URL},
			squirrel.Eq{consts.QVidChanID: c.ID},
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
		Set(consts.QDLPct, v.DownloadStatus.Pct).
		Where(squirrel.Eq{consts.QDLVidID: v.ID}).
		RunWith(tx)

	if _, err := dlQuery.Exec(); err != nil {
		return fmt.Errorf("failed to update download status: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction for video %q: %w", v.URL, err)
	}
	committed = true

	logging.S(0, "Updated video with URL: %s", v.URL)
	return nil
}

// DeleteVideo deletes an existent downloaded video from the database.
func (vs *VideoStore) DeleteVideo(key, val string, chanID int64) error {
	if key == "" || val == "" {
		return errors.New("please pass in a key and value to delete a video entry")
	}

	query := squirrel.
		Delete(consts.DBVideos).
		Where(squirrel.Eq{key: val}).
		RunWith(vs.DB)

	if _, err := query.Exec(); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fmt.Printf("No video exists with key %q and value %q", key, val)
		} else {
			return err
		}
	}

	return nil
}

// ******************************** Private ********************************

// videoExists returns true if the video exists in the database.
func (vs *VideoStore) videoExists(v *models.Video, c *models.Channel) (int64, bool) {
	var id int64
	query := squirrel.
		Select(consts.QVidID).
		From(consts.DBVideos).
		Where(squirrel.And{
			squirrel.Eq{consts.QVidURL: v.URL},
			squirrel.Eq{consts.QVidChanID: c.ID},
		}).
		RunWith(vs.DB)

	err := query.QueryRow().Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false
	} else if err != nil {
		logging.E(0, "Error checking if video exists: %v", err)
		return 0, false
	}

	logging.D(1, "Video with URL %q already exists in the database with ID %d", v.URL, v.ID)
	return id, true
}
