package repo

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
	"tubarr/internal/utils/jsonutils"
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

// AddVideos adds an array of videos into the database.
//
// Returns with the same video array but filled with IDs.
func (vs VideoStore) AddVideos(videos []*models.Video, c *models.Channel) ([]*models.Video, []error) {
	var errArray []error
	tx, err := vs.DB.Begin()
	if err != nil {
		return nil, append(errArray, fmt.Errorf("failed to begin transaction: %w", err))
	}

	var committed bool
	defer func() {
		if !committed && tx != nil {
			if err := tx.Rollback(); err != nil {
				logging.E(0, "Error rolling back: %v", err)
			}
		}
	}()

	validVideos := make([]*models.Video, 0, len(videos))

	for i, v := range videos {
		switch {
		case v.URL == "":
			errArray = append(errArray, fmt.Errorf("must enter a url for video #%d", i))
			continue
		case v.ChannelID == 0:
			if c.ID == 0 {
				errArray = append(errArray, fmt.Errorf("video #%d has no channel ID", i))
				continue
			}
			v.ChannelID = c.ID
		}

		validVideos = append(validVideos, v)
	}

	if len(validVideos) > 0 {
		for _, v := range validVideos {

			var (
				result sql.Result
			)

			_, exists := vs.videoExists(v)
			if !exists {
				query := squirrel.Insert(consts.DBVideos).
					Columns(
						consts.QVidChanID,
						consts.QVidURL,
						consts.QVidFinished,
					).
					Values(
						v.ChannelID,
						v.URL,
						v.Finished,
					).
					RunWith(tx)

				result, err = query.Exec()
				if err != nil {
					errArray = append(errArray, fmt.Errorf("failed to insert video %s: %w", v.URL, err))
					continue
				}
			} else {
				query := squirrel.Update(consts.DBVideos).
					Set(consts.QVidFinished, v.Finished).
					Where(squirrel.Eq{consts.QVidChanID: v.ChannelID, consts.QVidURL: v.URL}).
					RunWith(tx)

				result, err = query.Exec()
				if err != nil {
					errArray = append(errArray, fmt.Errorf("failed to insert video %s: %w", v.URL, err))
					continue
				}
			}

			id, err := result.LastInsertId()
			if err != nil {
				errArray = append(errArray, fmt.Errorf("failed to get last insert ID: %w", err))
				continue
			}
			v.ID = id

			dlQuery := squirrel.Insert(consts.DBDownloads).
				Columns(consts.QDLVidID, consts.QDLStatus, consts.QDLPct).
				Values(id, v.DownloadStatus.Status, v.DownloadStatus.Pct).
				RunWith(tx)

			if _, err := dlQuery.Exec(); err != nil {
				errArray = append(errArray, fmt.Errorf("failed to insert download status for video %d: %w", id, err))
				continue
			}
		}

		if err := tx.Commit(); err != nil {
			return nil, append(errArray, fmt.Errorf("failed to commit: %w", err))
		}

		committed = true
		return validVideos, errArray
	}

	return nil, errArray
}

// AddVideo adds a new video to the database.
func (vs VideoStore) AddVideo(v *models.Video) (int64, error) {
	if v.URL == "" {
		return 0, errors.New("must enter a url for video")
	}

	if id, exists := vs.videoExists(v); exists {
		logging.D(1, "Video %q already exists in the database", v.URL)
		if err := vs.UpdateVideo(v); err != nil { // Attempt an update if add is not appropriate
			return id, err
		}
		return id, nil
	}

	var (
		metadataJSON,
		settingsJSON,
		metarrJSON []byte
		err       error
		committed bool
	)

	// Convert metadata map to JSON string
	if metadataJSON, settingsJSON, metarrJSON, err = jsonutils.MarshalVideoJSON(v); err != nil {
		return 0, fmt.Errorf("failed to marshal JSON for video with URL %q: %w", v.URL, err)
	}

	tx, err := vs.DB.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if !committed && tx != nil {
			if err := tx.Rollback(); err != nil {
				logging.E(0, "Error rolling back: %v", err)
			}
		}
	}()

	now := time.Now()
	vidQuery := squirrel.Insert(consts.DBVideos).
		Columns(
			consts.QVidChanID, consts.QVidURL, consts.QVidTitle,
			consts.QVidDescription, consts.QVidFinished, consts.QVidUploadDate,
			consts.QVidMetadata, consts.QVidSettings, consts.QVidMetarr,
			consts.QVidCreatedAt, consts.QVidUpdatedAt,
		).
		Values(
			v.ChannelID, v.URL, v.Title,
			v.Description, v.Finished, v.UploadDate,
			metadataJSON, settingsJSON, metarrJSON,
			now, now,
		).
		RunWith(tx)

	result, err := vidQuery.Exec()
	if err != nil {
		return 0, fmt.Errorf("failed to insert video: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get inserted video ID: %w", err)
	}

	dlQuery := squirrel.Insert(consts.DBDownloads).
		Columns(consts.QDLVidID, consts.QDLStatus, consts.QDLPct).
		Values(id, v.DownloadStatus.Status, v.DownloadStatus.Pct).
		RunWith(tx)

	if _, err := dlQuery.Exec(); err != nil {
		return 0, fmt.Errorf("failed to insert download status: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}
	committed = true
	return id, nil
}

// UpdateVideo updates the status of the video in the database.
func (vs VideoStore) UpdateVideo(v *models.Video) error {
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

	metadataJSON, settingsJSON, metarrJSON, err := jsonutils.MarshalVideoJSON(v)
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
			squirrel.Eq{consts.QVidChanID: v.ChannelID},
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

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	committed = true
	logging.S(0, "Updated video with URL: %s", v.URL)
	return nil
}

// DeleteVideo deletes an existent downloaded video from the database.
func (vs VideoStore) DeleteVideo(key, val string, chanID int64) error {
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

// Private /////////////////////////////////////////////////////////////////////

// videoExists returns true if the video exists in the database.
func (vs VideoStore) videoExists(v *models.Video) (int64, bool) {
	var id int64
	query := squirrel.
		Select(consts.QVidID).
		From(consts.DBVideos).
		Where(squirrel.And{
			squirrel.Eq{consts.QVidURL: v.URL},
			squirrel.Eq{consts.QVidChanID: v.ChannelID},
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
