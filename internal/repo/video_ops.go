package repo

import (
	"database/sql"
	"encoding/json"
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

func GetVideoStore(db *sql.DB) *VideoStore {
	return &VideoStore{
		DB: db,
	}
}

// GetDB returns the database
func (vs *VideoStore) GetDB() *sql.DB {
	return vs.DB
}

// AddVideo adds a new video to the database
func (vs VideoStore) AddVideo(v *models.Video) (int64, error) {
	switch {
	case v.URL == "":
		return 0, fmt.Errorf("must enter a url for video")
	case v.VDir == "":
		return 0, fmt.Errorf("must enter a video directory where downloads will be stored")
	}

	if id, exists := vs.videoExists(consts.QVidURL, v.URL); exists {
		logging.D(2, "Video with URL %q already exists, returning ID", v.URL)
		return id, nil // Return gracefully instead of error
	}

	// JSON dir
	if v.JDir == "" {
		v.JDir = v.VDir
	}
	now := time.Now()

	// Convert metadata map to JSON string
	metadataJSON, err := json.Marshal(v.MetadataMap)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal metadata to JSON: %w", err)
	}

	// Convert settings to JSON
	settingsJSON, err := json.Marshal(v.Settings)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal settings to JSON: %w", err)
	}

	// Convert metarr settings to JSON
	metarrJSON, err := json.Marshal(v.MetarrArgs)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal metarr settings to JSON: %w", err)
	}

	query := squirrel.
		Insert(consts.DBVideos).
		Columns(
			consts.QVidChanID,
			consts.QVidDownloaded,
			consts.QVidURL,
			consts.QVidTitle,
			consts.QVidDescription,
			consts.QVidVDir,
			consts.QVidJDir,
			consts.QVidUploadDate,
			consts.QVidMetadata,
			consts.QVidSettings,
			consts.QVidMetarr,
			consts.QVidCreatedAt,
			consts.QVidUpdatedAt,
		).
		Values(
			v.ChannelID,
			v.Downloaded,
			v.URL,
			v.Title,
			v.Description,
			v.VDir,
			v.JDir,
			v.UploadDate,
			metadataJSON,
			settingsJSON,
			metarrJSON,
			now,
			now,
		).
		RunWith(vs.DB)

	result, err := query.Exec()
	if err != nil {
		return 0, fmt.Errorf("failed to insert video: %w", err)
	}

	return result.LastInsertId()
}

// UpdateVideo updates the status of the video in the database
func (vs VideoStore) UpdateVideo(v *models.Video) error {

	// JSON conversions for storage in DB
	metadataJSON, err := json.Marshal(v.MetadataMap)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata to JSON: %w", err)
	}

	settingsJSON, err := json.Marshal(v.Settings)
	if err != nil {
		return fmt.Errorf("failed to marshal settings to JSON: %w", err)
	}

	metarrJSON, err := json.Marshal(v.MetarrArgs)
	if err != nil {
		return fmt.Errorf("failed to marshal metarr settings to JSON: %w", err)
	}

	query := squirrel.
		Update(consts.DBVideos).
		Set(consts.QVidDownloaded, v.Downloaded).
		Set(consts.QVidTitle, v.Title).
		Set(consts.QVidDescription, v.Description).
		Set(consts.QVidVDir, v.VDir).
		Set(consts.QVidMetadata, metadataJSON).
		Set(consts.QVidSettings, settingsJSON).
		Set(consts.QVidMetarr, metarrJSON).
		Set(consts.QVidUploadDate, v.UploadDate).
		Set(consts.QVidUpdatedAt, time.Now()).
		Where(squirrel.Eq{consts.QVidURL: v.URL}).
		RunWith(vs.DB)

	result, err := query.Exec()
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

	logging.D(1, "Updated video in database: %s (Title: %s)", v.URL, v.Title)
	return nil
}

// DeleteVideo deletes an existent downloaded video from the database
func (vs VideoStore) DeleteVideo(key, val string) error {
	if key == "" || val == "" {
		return fmt.Errorf("please pass in a key and value to delete a video entry")
	}

	query := squirrel.
		Delete(consts.DBVideos).
		Where(squirrel.Eq{key: val}).
		RunWith(vs.DB)

	if _, err := query.Exec(); err != nil {
		if err == sql.ErrNoRows {
			fmt.Printf("No video exists with key %q and value %q", key, val)
		} else {
			return err
		}
	}

	return nil
}

// Private /////////////////////////////////////////////////////////////////////

// channelExists returns true if the channel exists in the database
func (vs VideoStore) videoExists(key, val string) (int64, bool) {
	var exists bool
	query := squirrel.
		Select("1").
		From(consts.DBVideos).
		Where(squirrel.Eq{key: val}).
		RunWith(vs.DB)

	if err := query.QueryRow().Scan(&exists); err != nil {
		logging.E(0, err.Error())
		return 0, false
	}

	idQuery := squirrel.
		Select(consts.QVidID).
		Where(squirrel.Eq{key: val}).
		RunWith(vs.DB)

	rtn, err := idQuery.Exec()
	if err != nil {
		logging.E(0, "Failed to retrieve ID for video with key %q and value %q", key, val)
		return 0, exists
	}

	id, err := rtn.LastInsertId()
	if err != nil {
		logging.E(0, "Error grabbing last insert ID for video with key %q and value %q", key, val)
		return id, exists
	}
	return id, exists
}
