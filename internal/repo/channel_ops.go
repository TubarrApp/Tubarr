package repo

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
	"tubarr/internal/process"
	"tubarr/internal/utils/logging"

	"github.com/Masterminds/squirrel"
)

type ChannelStore struct {
	DB *sql.DB
}

// GetChannelStore returns a channel store instance with injected database.
func GetChannelStore(db *sql.DB) *ChannelStore {
	return &ChannelStore{
		DB: db,
	}
}

// GetDB returns the database.
func (cs *ChannelStore) GetDB() *sql.DB {
	return cs.DB
}

// GetID gets the channel ID from an input key and value.
func (cs *ChannelStore) GetID(key, val string) (int64, error) {
	switch key {
	case "url", "name", "id":
		if val == "" {
			return 0, fmt.Errorf("please enter a value for key %q", key)
		}
	default:
		return 0, fmt.Errorf("please input a unique constrained value, such as URL or name")
	}
	var id int64
	query := squirrel.
		Select("id").
		From(consts.DBChannels).
		Where(squirrel.Eq{key: val}).
		RunWith(cs.DB)

	if err := query.QueryRow().Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// AddURLToIgnore adds a URL into the database to ignore in subsequent crawls.
func (cs *ChannelStore) AddURLToIgnore(channelID int64, ignoreURL string) error {

	if !cs.channelExistsID(channelID) {
		return fmt.Errorf("channel with ID %d does not exist", channelID)
	}

	query := squirrel.
		Insert(consts.DBVideos).
		Columns(consts.QVidChanID, consts.QVidURL, consts.QVidDownloaded).
		Values(channelID, ignoreURL, true).
		RunWith(cs.DB)

	if _, err := query.Exec(); err != nil {
		return err
	}
	logging.S(0, "Added URL %q to ignore list for channel with ID '%d'", ignoreURL, channelID)
	return nil
}

// GetNotifyURLs returns all notification URLs for a given channel.
func (cs *ChannelStore) GetNotifyURLs(id int64) ([]string, error) {
	query := squirrel.
		Select(consts.QNotifyURL).
		From(consts.DBNotifications).
		Where(squirrel.Eq{consts.QNotifyChanID: id}).
		RunWith(cs.DB)

	// Execute query to get rows
	rows, err := query.Query()
	if err != nil {
		return nil, fmt.Errorf("failed to query notification URLs: %w", err)
	}
	defer rows.Close()

	// Collect all URLs
	var urls []string
	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err != nil {
			return nil, fmt.Errorf("failed to scan notification URL: %w", err)
		}
		urls = append(urls, url)
	}

	// Check for errors from iterating over rows
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating notification URLs: %w", err)
	}

	return urls, nil
}

// AddNotifyURL sets a notification table entry for a channel with a given ID.
func (cs *ChannelStore) AddNotifyURL(id int64, notifyName, notifyURL string) error {

	if notifyURL == "" {
		return fmt.Errorf("please enter a notification URL")
	}

	if notifyName == "" {
		notifyName = notifyURL
	}

	query := squirrel.
		Insert(consts.DBNotifications).
		Columns(consts.QNotifyChanID, consts.QNotifyName, consts.QNotifyURL, consts.QNotifyCreatedAt, consts.QNotifyUpdatedAt).
		Values(id, notifyName, notifyURL, time.Now(), time.Now()).
		Suffix("ON CONFLICT (channel_id, notify_url) DO UPDATE SET notify_url = EXCLUDED.notify_url, updated_at = EXCLUDED.updated_at").
		RunWith(cs.DB)

	if _, err := query.Exec(); err != nil {
		return err
	}
	return nil
}

// AddChannel adds a new channel to the database.
func (cs ChannelStore) AddChannel(c *models.Channel) (int64, error) {
	switch {
	case c.URL == "":
		return 0, fmt.Errorf("must enter a url for channel")
	case c.VDir == "":
		return 0, fmt.Errorf("must enter a video directory where downloads will be stored")
	}

	if cs.channelExists(consts.QChanURL, c.URL) {
		return 0, fmt.Errorf("channel with URL %q already exists", c.URL)
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

// DeleteChannel deletes a channel from the database with a given key/value.
func (cs *ChannelStore) DeleteChannel(key, val string) error {
	if !cs.channelExists(key, val) {
		return fmt.Errorf("channel with key %q and value %q does not exist", key, val)
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

// CrawlChannelIgnore crawls a channel and adds the latest videos to the ignore list.
func (cs *ChannelStore) CrawlChannelIgnore(key, val string, s models.Store) error {
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

	if err := process.CrawlIgnoreNew(s, &c); err != nil {
		return err
	}
	return nil
}

// CrawlChannel crawls a channel and finds video URLs which have not yet been downloaded.
func (cs *ChannelStore) CrawlChannel(key, val string, s models.Store) error {
	var (
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

	var c models.Channel
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
	return process.ChannelCrawl(s, &c)
}

// ListChannels lists all channels in the database.
func (cs *ChannelStore) ListChannels() (channels []*models.Channel, err error, hasRows bool) {
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
		channels = append(channels, &c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err), true
	}

	return channels, nil, true
}

// UpdateChannelSettings updates specific settings in the channel's settings JSON.
func (cs ChannelStore) UpdateChannelSettings(key, val string, updateFn func(*models.ChannelSettings) error) error {
	var settingsJSON json.RawMessage
	query := squirrel.
		Select(consts.QChanSettings).
		From(consts.DBChannels).
		Where(squirrel.Eq{key: val}).
		RunWith(cs.DB)

	err := query.QueryRow().Scan(&settingsJSON)
	if err == sql.ErrNoRows {
		return fmt.Errorf("no channel found with key %q and value '%v'", key, val)
	} else if err != nil {
		return fmt.Errorf("failed to get channel settings: %w", err)
	}

	// Unmarshal current settings
	var settings models.ChannelSettings
	if err := json.Unmarshal(settingsJSON, &settings); err != nil {
		return fmt.Errorf("failed to unmarshal settings: %w", err)
	}

	// Apply the update
	if err := updateFn(&settings); err != nil {
		return fmt.Errorf("failed to update settings: %w", err)
	}

	// Marshal updated settings
	updatedJSON, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal updated settings: %w", err)
	}

	// Update in database
	updateQuery := squirrel.
		Update(consts.DBChannels).
		Set(consts.QChanSettings, updatedJSON).
		Set(consts.QChanUpdatedAt, time.Now()).
		Where(squirrel.Eq{key: val}).
		RunWith(cs.DB)

	result, err := updateQuery.Exec()
	if err != nil {
		return fmt.Errorf("failed to update channel settings: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("no channel found with key %q and value '%v'", key, val)
	}

	logging.S(1, "Updated settings for channel with key %q and value '%v'", key, val)
	return nil
}

// UpdateCrawlFrequency updates the crawl frequency for a channel.
func (cs ChannelStore) UpdateCrawlFrequency(key, val string, newFreq int) error {
	return cs.UpdateChannelSettings(key, val, func(s *models.ChannelSettings) error {
		s.CrawlFreq = newFreq
		return nil
	})
}

// UpdateExternalDownloader updates the external downloader settings.
func (cs ChannelStore) UpdateExternalDownloader(key, val, downloader, args string) error {
	return cs.UpdateChannelSettings(key, val, func(s *models.ChannelSettings) error {
		s.ExternalDownloader = downloader
		s.ExternalDownloaderArgs = args
		return nil
	})
}

// UpdateChannelRow updates a single element in the database.
func (cs ChannelStore) UpdateChannelRow(key, val, col, newVal string) error {
	if key == "" {
		return fmt.Errorf("please do not enter the key and value blank")
	}

	if !cs.channelExists(key, val) {
		return fmt.Errorf("channel with key %q and value %q does not exist", key, val)
	}

	query := squirrel.
		Update(consts.DBChannels).
		Where(squirrel.Eq{key: val}).
		Set(col, newVal).
		RunWith(cs.DB)

	if _, err := query.Exec(); err != nil {
		return err
	}
	return nil
}

// UpdateLastScan updates the DB entry for when the channel was last scanned.
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

// LoadGrabbedURLs loads already downloaded URLs from the database.
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

// channelExists returns true if the channel exists in the database.
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
		logging.E(0, err.Error())
		return exists
	}
	return exists
}

// channelExistsID returns true if the channel ID exists in the database.
func (cs ChannelStore) channelExistsID(id int64) bool {
	var exists bool
	query := squirrel.
		Select("1").
		From(consts.DBChannels).
		Where(squirrel.Eq{consts.QChanID: id}).
		RunWith(cs.DB)

	if err := query.QueryRow().Scan(&exists); err == sql.ErrNoRows {
		return false
	} else if err != nil {
		logging.E(0, err.Error())
		return exists
	}
	return exists
}
