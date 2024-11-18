package repo

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/interfaces"
	"tubarr/internal/models"
	"tubarr/internal/process"
	"tubarr/internal/utils/logging"

	"github.com/Masterminds/squirrel"
)

type ChannelStore struct {
	DB *sql.DB
}

func GetChannelStore(db *sql.DB) *ChannelStore {
	return &ChannelStore{
		DB: db,
	}
}

// GetDB returns the database
func (cs *ChannelStore) GetDB() *sql.DB {
	return cs.DB
}

// AddChannel adds a new channel to the database
func (cs ChannelStore) AddChannel(c *models.Channel) (int64, error) {
	switch {
	case c.URL == "":
		return 0, fmt.Errorf("must enter a url for channel")
	case c.VDir == "":
		return 0, fmt.Errorf("must enter a video directory where downloads will be stored")
	}

	if cs.channelExists(consts.QChanURL, c.URL) {
		return 0, fmt.Errorf("channel with URL '%s' already exists", c.URL)
	}

	// JSON dir
	if c.JDir == "" {
		c.JDir = c.VDir
	}
	now := time.Now()

	// Convert settings to JSON
	settingsJSON, err := json.Marshal(c.Settings)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal settings: %w", err)
	}

	// Convert metarr settings to JSON
	metarrJSON, err := json.Marshal(c.MetarrArgs)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal metarr settings: %w", err)
	}

	query := squirrel.
		Insert(consts.DBChannels).
		Columns(
			consts.QChanURL,
			consts.QChanName,
			consts.QChanVDir,
			consts.QChanJDir,
			consts.QChanSettings,
			consts.QChanMetarr, // Add this column
			consts.QChanLastScan,
			consts.QChanCreatedAt,
			consts.QChanUpdatedAt,
		).
		Values(
			c.URL,
			c.Name,
			c.VDir,
			c.JDir,
			settingsJSON,
			metarrJSON, // Add this value
			now,
			now,
			now,
		).
		RunWith(cs.DB)

	result, err := query.Exec()
	if err != nil {
		return 0, fmt.Errorf("failed to insert channel: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last insert ID: %w", err)
	}

	logging.S(0, "Successfully added channel with metarr operations: %+v", c.MetarrArgs)
	return id, nil
}

// DeleteChannel deletes a channel from the database with a given key/value
func (cs *ChannelStore) DeleteChannel(key, val string) error {
	if !cs.channelExists(key, val) {
		return fmt.Errorf("channel with key '%s' and value '%s' does not exist", key, val)
	}

	query := squirrel.
		Delete(consts.DBChannels).
		Where(squirrel.Eq{key: val}).
		RunWith(cs.DB)

	if _, err := query.Exec(); err != nil {
		return err
	}
	return nil
}

// CrawlChannel crawls a channel and finds video URLs which have not yet been downloaded
func (cs *ChannelStore) CrawlChannel(key, val string, s interfaces.Store) error {
	var (
		c                    models.Channel
		settings, metarrJSON json.RawMessage
	)

	query := squirrel.
		Select(
			consts.QChanID,
			consts.QChanURL,
			consts.QChanName,
			consts.QChanVDir,
			consts.QChanJDir,
			consts.QChanSettings,
			consts.QChanMetarr,
			consts.QChanLastScan,
			consts.QChanCreatedAt,
			consts.QChanUpdatedAt,
		).
		From(consts.DBChannels).
		Where(squirrel.Eq{key: val})

	if err := query.
		RunWith(cs.DB).
		QueryRow().
		Scan(
			&c.ID,
			&c.URL,
			&c.Name,
			&c.VDir,
			&c.JDir,
			&settings,
			&metarrJSON,
			&c.LastScan,
			&c.CreatedAt,
			&c.UpdatedAt,
		); err != nil {
		return fmt.Errorf("failed to scan channel: %w", err)
	}

	// Unmarshal settings
	if err := json.Unmarshal(settings, &c.Settings); err != nil {
		return fmt.Errorf("parsing channel settings: %w", err)
	}

	// Unmarshal metarr settings
	if len(metarrJSON) > 0 {
		if err := json.Unmarshal(metarrJSON, &c.MetarrArgs); err != nil {
			return fmt.Errorf("parsing metarr settings: %w", err)
		}
	}

	logging.D(1, "Retrieved channel with Metarr args: %+v", c.MetarrArgs)
	return process.ChannelCrawl(s, c)
}

// ListChannels lists all channels in the database
func (cs *ChannelStore) ListChannels() (channels []models.Channel, err error, hasRows bool) {
	query := squirrel.
		Select(
			consts.QChanID,
			consts.QChanURL,
			consts.QChanName,
			consts.QChanVDir,
			consts.QChanJDir,
			consts.QChanSettings,
			consts.QChanMetarr,
			consts.QChanLastScan,
			consts.QChanCreatedAt,
			consts.QChanUpdatedAt,
		).
		From(consts.DBChannels).
		OrderBy(consts.QChanName).
		RunWith(cs.DB)

	rows, err := query.Query()
	if err == sql.ErrNoRows {
		return nil, nil, false
	} else if err != nil {
		return nil, fmt.Errorf("failed to query channels: %w", err), true
	}
	defer rows.Close()

	for rows.Next() {
		var c models.Channel
		var settingsJSON, metarrJSON []byte
		err := rows.Scan(
			&c.ID,
			&c.URL,
			&c.Name,
			&c.VDir,
			&c.JDir,
			&settingsJSON,
			&metarrJSON, // Scan Metarr JSON
			&c.LastScan,
			&c.CreatedAt,
			&c.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan channel: %w", err), true
		}

		// Unmarshal settings JSON
		if len(settingsJSON) > 0 {
			if err := json.Unmarshal(settingsJSON, &c.Settings); err != nil {
				return nil, fmt.Errorf("failed to unmarshal settings: %w", err), true
			}
		}

		// Unmarshal Metarr JSON
		if len(metarrJSON) > 0 {
			if err := json.Unmarshal(metarrJSON, &c.MetarrArgs); err != nil {
				return nil, fmt.Errorf("failed to unmarshal metarr settings: %w", err), true
			}
		}

		channels = append(channels, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err), true
	}

	return channels, nil, true
}

// UpdateLastScan updates the DB entry for when the channel was last scanned
func (cs *ChannelStore) UpdateLastScan(channelID int64) error {
	query := squirrel.
		Update(consts.DBChannels).
		Set(consts.QChanLastScan, time.Now()).
		Set(consts.QChanUpdatedAt, time.Now()).
		Where(squirrel.Eq{consts.QChanID: channelID}).
		RunWith(cs.DB)

	result, err := query.Exec()
	if err != nil {
		return fmt.Errorf("failed to update last scan time: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("no channel found with ID %d", channelID)
	}

	logging.D(1, "Updated last scan time for channel ID %d", channelID)
	return nil
}

// LoadGrabbedURLs loads already downloaded URLs from the database
func (cs ChannelStore) LoadGrabbedURLs(c *models.Channel) (urls []string, err error) {
	if c.ID == 0 {
		return nil, fmt.Errorf("model entered has no ID")
	}

	// Select URLs where channel_id matches and downloaded is true
	query := squirrel.
		Select(consts.QVidURL).
		From(consts.DBVideos).
		Where(squirrel.And{
			squirrel.Eq{consts.QVidChanID: c.ID},
			squirrel.Eq{consts.QVidDownloaded: true},
		}).
		RunWith(cs.DB)

	logging.D(2, "Executing query to find downloaded videos: %v for channel ID %d", query, c.ID)

	rows, err := query.Query()
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		urls = append(urls, url)
		logging.D(2, "Found downloaded video: %s", url)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	logging.D(1, "Found %d previously downloaded videos for channel ID %d", len(urls), c.ID)
	return urls, nil
}

// Private /////////////////////////////////////////////////////////////////////

// channelExists returns true if the channel exists in the database
func (cs ChannelStore) channelExists(key, val string) bool {
	var exists bool
	query := squirrel.
		Select("1").
		From(consts.DBChannels).
		Where(squirrel.Eq{key: val}).
		RunWith(cs.DB)

	if err := query.QueryRow().Scan(&exists); err == sql.ErrNoRows {
		return false
	} else if err != nil {
		log.Fatalf("error querying row, aborting program")
	}
	return exists
}
