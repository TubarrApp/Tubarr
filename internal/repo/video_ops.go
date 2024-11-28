package repo

import (
	"database/sql"
	"encoding/json"
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

// AddVideos adds an array of videos to the database.
func (vs VideoStore) AddVideos(videos []*models.Video, c *models.Channel) (ok bool, errArray []error) {

	// Query start
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
		RunWith(vs.DB)

	errArray = make([]error, 0, len(videos))
	validVideos := make([][]interface{}, 0, len(videos))

	for i, v := range videos {
		switch {
		case v.URL == "":
			errArray = append(errArray, fmt.Errorf("must enter a url for video #%d in channel %q", i, c.Name))
			continue
		case v.VDir == "":
			errArray = append(errArray, fmt.Errorf("must enter a video output directory for video #%d in channel %q", i, c.Name))
			continue
		case v.ChannelID == 0:
			if c.ID == 0 {
				errArray = append(errArray, fmt.Errorf("video #%d has no channel ID and no ID found in channel model %q, skipping", i, c.Name))
				continue
			} else {
				v.ChannelID = c.ID
				logging.W("Video with URL %q had empty channel ID, filled with passed in channel ID %d", v.URL, c.ID)
			}
		}

		if id, exists := vs.videoExists(v); exists {
			logging.D(1, "video %q already exists in the database for channel ID %d, attempting update", v.URL, v.ChannelID)
			if err := vs.UpdateVideo(v); err != nil {
				errArray = append(errArray, fmt.Errorf("video (ID: %d) in channel (ID: %d) URL %q already exists in database, and update failed", id, v.ChannelID, v.URL))
				continue
			}
		}

		// JSON dir
		if v.JDir == "" {
			v.JDir = v.VDir
		}
		now := time.Now()

		var (
			metadataJSON []byte
			settingsJSON []byte
			metarrJSON   []byte
			err          error
		)

		if metadataJSON, settingsJSON, metarrJSON, err = vs.marshalVideoJSON(v); err != nil {
			errArray = append(errArray, fmt.Errorf("failed to marshal JSON elements for video with ID %d and URL %q: %w", v.ID, v.URL, err))
			continue
		}

		validVideos = append(validVideos, []interface{}{
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
		})
	}

	if len(validVideos) > 0 {
		for _, values := range validVideos {
			query = query.Values(values...)
		}

		if _, err := query.Exec(); err != nil {
			errArray = append(errArray, fmt.Errorf("batch insert failed for videos in channel %q: %w", c.Name, err))
		} else {
			ok = len(validVideos) > len(errArray)
		}
	}
	return ok, errArray
}

// AddVideo adds a new video to the database.
func (vs VideoStore) AddVideo(v *models.Video) (int64, error) {
	switch {
	case v.URL == "":
		return 0, errors.New("must enter a url for video")
	case v.VDir == "":
		return 0, errors.New("must enter a video directory where downloads will be stored")
	}

	if id, exists := vs.videoExists(v); exists {
		logging.D(1, "Video %q already exists in the database", v.URL)
		if err := vs.UpdateVideo(v); err != nil { // Attempt an update if add is not appropriate
			return id, err
		}
		return id, nil
	}

	// JSON dir
	if v.JDir == "" {
		v.JDir = v.VDir
	}
	now := time.Now()

	var (
		metadataJSON []byte
		settingsJSON []byte
		metarrJSON   []byte
		err          error
	)

	// Convert metadata map to JSON string
	if v.MetadataMap != nil {
		metadataJSON, err = json.Marshal(v.MetadataMap)
		if err != nil {
			return 0, fmt.Errorf("failed to marshal metadata to JSON: %w", err)
		}
	}

	// Convert settings to JSON
	settingsJSON, err = json.Marshal(v.Settings)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal settings to JSON: %w", err)
	}

	// Convert metarr settings to JSON
	metarrJSON, err = json.Marshal(v.MetarrArgs)
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

// UpdateVideo updates the status of the video in the database.
func (vs VideoStore) UpdateVideo(v *models.Video) error {
	metadataJSON, err := json.Marshal(v.MetadataMap)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	settingsJSON, err := json.Marshal(v.Settings)
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	metarrJSON, err := json.Marshal(v.MetarrArgs)
	if err != nil {
		return fmt.Errorf("failed to marshal metarr: %w", err)
	}

	query := squirrel.
		Update(consts.DBVideos).
		Set(consts.QVidDownloaded, v.Downloaded).
		Set(consts.QVidTitle, v.Title).
		Set(consts.QVidDescription, v.Description).
		Set(consts.QVidVDir, v.VDir).
		Set(consts.QVidJDir, v.JDir).
		Set(consts.QVidVPath, v.VPath).
		Set(consts.QVidJPath, v.JPath).
		Set(consts.QVidUploadDate, v.UploadDate).
		Set(consts.QVidMetadata, metadataJSON).
		Set(consts.QVidSettings, settingsJSON).
		Set(consts.QVidMetarr, metarrJSON).
		Set(consts.QVidUpdatedAt, time.Now()).
		Where(squirrel.And{
			squirrel.Eq{consts.QVidURL: v.URL},
			squirrel.Eq{consts.QVidChanID: v.ChannelID},
		}).
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

	logging.S(0, "Updated video: %s (Title: %s) Downloaded: %v", v.URL, v.Title, v.Downloaded)
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
	logging.D(1, "Video %q already exists", v.ID)
	return id, true
}

// marshalVideoJSON marshals all JSON elements for a video model.
func (vs VideoStore) marshalVideoJSON(v *models.Video) (metadata, settings, metarr []byte, err error) {
	if v.MetadataMap != nil {
		metadata, err = json.Marshal(v.MetadataMap)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("metadata marshal: %w", err)
		}
	}

	settings, err = json.Marshal(v.Settings)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("settings marshal: %w", err)
	}

	metarr, err = json.Marshal(v.MetarrArgs)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("metarr marshal: %w", err)
	}

	return metadata, settings, metarr, nil
}
