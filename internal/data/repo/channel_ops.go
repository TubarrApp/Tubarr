package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	case consts.QChanName, consts.QChanID:
		if val == "" {
			return 0, fmt.Errorf("please enter a value for key %q", key)
		}
	default:
		return 0, errors.New("please input a unique constrained value, such as URL or name")
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

// GetAuth retrieves authentication details for a specific URL in a channel.
func (cs *ChannelStore) GetAuth(channelID int64, url string) (username, password, loginURL string, err error) {
	var u, p, l sql.NullString // Use sql.NullString to handle NULL values

	query := squirrel.
		Select(consts.QChanURLsUsername, consts.QChanURLsPassword, consts.QChanURLsLoginURL).
		From(consts.DBChannelURLs).
		Where(squirrel.And{
			squirrel.Eq{consts.QChanURLsChannelID: channelID},
			squirrel.Eq{consts.QChanURLsURL: url},
		}).
		RunWith(cs.DB)

	if err = query.QueryRow().Scan(&u, &p, &l); err != nil {
		logging.I("No auth details found in database for channel ID: %d, URL: %s", channelID, url)
		return "", "", "", err
	}

	return u.String, p.String, l.String, nil
}

// DeleteVideoURL deletes a URL from the downloaded database list.
func (cs *ChannelStore) DeleteVideoURLs(channelID int64, urls []string) error {

	if !cs.channelExistsID(channelID) {
		return fmt.Errorf("channel with ID %d does not exist", channelID)
	}

	query := squirrel.
		Delete(consts.DBVideos).
		Where(squirrel.Eq{
			consts.QVidChanID: channelID,
			consts.QVidURL:    urls,
		}).
		RunWith(cs.DB)

	if _, err := query.Exec(); err != nil {
		return err
	}
	logging.S(0, "Deleted URLs %q for channel with ID '%d'", urls, channelID)
	return nil
}

// AddURLToIgnore adds a URL into the database to ignore in subsequent crawls.
func (cs *ChannelStore) AddURLToIgnore(channelID int64, ignoreURL string) error {

	if !cs.channelExistsID(channelID) {
		return fmt.Errorf("channel with ID %d does not exist", channelID)
	}

	query := squirrel.
		Insert(consts.DBVideos).
		Columns(consts.QVidChanID, consts.QVidURL, consts.QVidFinished).
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
	defer func() {
		if err := rows.Close(); err != nil {
			logging.E(0, "Failed to close rows for notify URLs in channel with ID %d", id)
		}
	}()

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

// DeleteNotifyURLs deletes notify URLs from the channel.
func (cs *ChannelStore) DeleteNotifyURLs(channelID int64, urls, names []string) error {

	if !cs.channelExistsID(channelID) {
		return fmt.Errorf("channel with ID %d does not exist", channelID)
	}

	query := squirrel.
		Delete(consts.DBNotifications).
		Where(squirrel.Eq{
			consts.QVidChanID: channelID,
		}).
		Where(squirrel.Or{
			squirrel.Eq{consts.QNotifyURL: urls},
			squirrel.Eq{consts.QNotifyName: names},
		}).
		RunWith(cs.DB)

	if _, err := query.Exec(); err != nil {
		return err
	}
	logging.S(0, "Deleted notify URLs %q for channel with ID '%d'", urls, channelID)
	return nil
}

// AddNotifyURL sets a notification table entry for a channel with a given ID.
func (cs *ChannelStore) AddNotifyURL(id int64, notifyName, notifyURL string) error {

	if notifyURL == "" {
		return errors.New("please enter a notification URL")
	}

	if notifyName == "" {
		notifyName = notifyURL
	}

	const (
		querySuffix = "ON CONFLICT (channel_id, notify_url) DO UPDATE SET notify_url = EXCLUDED.notify_url, updated_at = EXCLUDED.updated_at"
	)

	query := squirrel.
		Insert(consts.DBNotifications).
		Columns(consts.QNotifyChanID, consts.QNotifyName, consts.QNotifyURL, consts.QNotifyCreatedAt, consts.QNotifyUpdatedAt).
		Values(id, notifyName, notifyURL, time.Now(), time.Now()).
		Suffix(querySuffix).
		RunWith(cs.DB)

	if _, err := query.Exec(); err != nil {
		return err
	}

	logging.S(0, "Added notification URL %q to channel with ID: %d", notifyURL, id)
	return nil
}

// AddAuth adds authentication details to a channel.
func (cs ChannelStore) AddAuth(chanID int64, authDetails map[string]*models.ChanURLAuthDetails) error {
	if !cs.channelExistsID(chanID) {
		return fmt.Errorf("channel with ID %d does not exist", chanID)
	}

	if authDetails == nil {
		logging.I("No authorization details to add for channel with ID %d", chanID)
		return nil
	}

	for chanURL, a := range authDetails {
		if !cs.channelURLExists(consts.QChanURLsURL, chanURL) {
			return fmt.Errorf("channel with URL %q does not exist", chanURL)
		}

		query := squirrel.
			Update(consts.DBChannelURLs).
			Set(consts.QChanURLsUsername, a.Username).
			Set(consts.QChanURLsPassword, a.Password).
			Set(consts.QChanURLsLoginURL, a.LoginURL).
			Where(squirrel.Eq{consts.QChanURLsURL: chanURL}).
			RunWith(cs.DB)

		if _, err := query.Exec(); err != nil {
			return err
		}
		logging.S(0, "Added authentication details for URL %q in channel with ID %d", chanURL, chanID)
	}
	return nil
}

// AddChannel adds a new channel to the database.
func (cs ChannelStore) AddChannel(c *models.Channel) (int64, error) {
	switch {
	case len(c.URLs) == 0:
		return 0, errors.New("must enter at least one URL for the channel")
	case c.VideoDir == "":
		return 0, errors.New("must enter a video directory where downloads will be stored")
	}

	if cs.channelExists(consts.QChanName, c.Name) {
		return 0, fmt.Errorf("channel with name %q already exists", c.Name)
	}

	// JSON dir
	if c.JSONDir == "" {
		c.JSONDir = c.VideoDir
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

	// Insert into the channels table
	query := squirrel.
		Insert(consts.DBChannels).
		Columns(
			consts.QChanName,
			consts.QChanVideoDir,
			consts.QChanJSONDir,
			consts.QChanSettings,
			consts.QChanMetarr,
			consts.QChanLastScan,
			consts.QChanPaused,
			consts.QChanCreatedAt,
			consts.QChanUpdatedAt,
		).
		Values(
			c.Name,
			c.VideoDir,
			c.JSONDir,
			settingsJSON,
			metarrJSON,
			now,
			false,
			now,
			now,
		).
		RunWith(cs.DB)

	result, err := query.Exec()
	if err != nil {
		return 0, fmt.Errorf("failed to insert channel: %w", err)
	}

	// Get the new channel ID
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last insert ID: %w", err)
	}

	// Insert URLs into the channel_urls table
	for _, url := range c.URLs {
		urlQuery := squirrel.
			Insert(consts.DBChannelURLs).
			Columns(consts.QChanURLsChannelID, consts.QChanURLsURL).
			Values(id, url).
			RunWith(cs.DB)

		if _, err := urlQuery.Exec(); err != nil {
			return 0, fmt.Errorf("failed to insert URL %q for channel ID %d: %w", url, id, err)
		}
	}

	logging.S(0, "Successfully added channel (ID: %d)\n\nName: %s\nURLs: %v\nCrawl Frequency: %d minutes\nFilters: %v\nSettings: %+v\nMetarr Operations: %+v",
		id, c.Name, c.URLs, c.Settings.CrawlFreq, c.Settings.Filters, c.Settings, c.MetarrArgs)

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
func (cs *ChannelStore) CrawlChannelIgnore(key, val string, s interfaces.Store, ctx context.Context) error {
	var (
		c                    models.Channel
		settings, metarrJSON json.RawMessage
	)

	query := squirrel.
		Select(
			consts.QChanID,
			consts.QChanName,
			consts.QChanVideoDir,
			consts.QChanJSONDir,
			consts.QChanSettings,
			consts.QChanMetarr,
			consts.QChanLastScan,
			consts.QChanPaused,
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
			&c.Name,
			&c.VideoDir,
			&c.JSONDir,
			&settings,
			&metarrJSON,
			&c.LastScan,
			&c.Paused,
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

	// Initialize URL list
	c.URLs = []string{}

	// Fetch all URLs for the channel
	urlQuery := squirrel.
		Select("url").
		From(consts.DBChannelURLs).
		Where(squirrel.Eq{"channel_id": c.ID}).
		RunWith(cs.DB)

	urlRows, err := urlQuery.Query()
	if err != nil {
		return fmt.Errorf("failed to fetch URLs for channel: %w", err)
	}
	defer urlRows.Close()

	for urlRows.Next() {
		var url string
		if err := urlRows.Scan(&url); err != nil {
			return fmt.Errorf("failed to scan URL: %w", err)
		}
		c.URLs = append(c.URLs, url)
	}

	logging.D(1, "Retrieved channel (ID: %d) with URLs: %+v", c.ID, c.URLs)

	// Process crawling using the updated channel object
	if err := process.CrawlIgnoreNew(s, &c, ctx); err != nil {
		return err
	}
	return nil
}

// CrawlChannel crawls a channel and finds video URLs which have not yet been downloaded.
func (cs *ChannelStore) CrawlChannel(key, val string, s interfaces.Store, ctx context.Context) error {
	var (
		settings, metarrJSON json.RawMessage
	)

	query := squirrel.
		Select(
			consts.QChanID,
			consts.QChanName,
			consts.QChanVideoDir,
			consts.QChanJSONDir,
			consts.QChanSettings,
			consts.QChanMetarr,
			consts.QChanLastScan,
			consts.QChanPaused,
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
			&c.Name,
			&c.VideoDir,
			&c.JSONDir,
			&settings,
			&metarrJSON,
			&c.LastScan,
			&c.Paused,
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

	// Initialize URL list
	c.URLs = []string{}

	// Fetch all URLs for the channel
	urlQuery := squirrel.
		Select("url").
		From(consts.DBChannelURLs).
		Where(squirrel.Eq{"channel_id": c.ID}).
		RunWith(cs.DB)

	urlRows, err := urlQuery.Query()
	if err != nil {
		return fmt.Errorf("failed to fetch URLs for channel: %w", err)
	}
	defer urlRows.Close()

	for urlRows.Next() {
		var url string
		if err := urlRows.Scan(&url); err != nil {
			return fmt.Errorf("failed to scan URL: %w", err)
		}
		c.URLs = append(c.URLs, url)
	}

	logging.D(1, "Retrieved channel (ID: %d) with URLs: %+v", c.ID, c.URLs)

	// Process crawling using the updated channel object
	return process.ChannelCrawl(s, &c, ctx)
}

// FetchChannel returns a single channel from the database along with its associated URLs.
func (cs *ChannelStore) FetchChannel(id int64) (*models.Channel, error, bool) {
	var (
		settingsJSON, metarrJSON json.RawMessage
	)
	query := squirrel.
		Select(
			consts.QChanID,
			consts.QChanName,
			consts.QChanVideoDir,
			consts.QChanJSONDir,
			consts.QChanSettings,
			consts.QChanMetarr,
			consts.QChanLastScan,
			consts.QChanPaused,
			consts.QChanCreatedAt,
			consts.QChanUpdatedAt,
		).
		From(consts.DBChannels).
		Where(squirrel.Eq{consts.QChanID: id}).
		RunWith(cs.DB)

	row := query.QueryRow()

	c := new(models.Channel)
	if err := row.Scan(
		&c.ID,
		&c.Name,
		&c.VideoDir,
		&c.JSONDir,
		&settingsJSON,
		&metarrJSON,
		&c.LastScan,
		&c.Paused,
		&c.CreatedAt,
		&c.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, false
		}
		return nil, fmt.Errorf("failed to scan channel: %w", err), false
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

	// Initialize URL list
	c.URLs = []string{}

	// Fetch URLs associated with this channel
	urlQuery := squirrel.
		Select("url").
		From(consts.DBChannelURLs).
		Where(squirrel.Eq{"channel_id": c.ID}).
		RunWith(cs.DB)

	urlRows, err := urlQuery.Query()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URLs for channel: %w", err), true
	}
	defer urlRows.Close()

	for urlRows.Next() {
		var url string
		if err := urlRows.Scan(&url); err != nil {
			return nil, fmt.Errorf("failed to scan URL: %w", err), true
		}
		c.URLs = append(c.URLs, url)
	}

	return c, nil, true
}

// FetchAllChannels retrieves all channels in the database.
func (cs *ChannelStore) FetchAllChannels() (channels []*models.Channel, err error, hasRows bool) {
	query := squirrel.
		Select(
			consts.QChanID,
			consts.QChanName,
			consts.QChanVideoDir,
			consts.QChanJSONDir,
			consts.QChanSettings,
			consts.QChanMetarr,
			consts.QChanLastScan,
			consts.QChanPaused,
			consts.QChanCreatedAt,
			consts.QChanUpdatedAt,
		).
		From(consts.DBChannels).
		OrderBy(consts.QChanName).
		RunWith(cs.DB)

	rows, err := query.Query()
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, false
	} else if err != nil {
		return nil, fmt.Errorf("failed to query channels: %w", err), true
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logging.E(0, "Failed to close channel rows")
		}
	}()

	channelsMap := make(map[int64]*models.Channel)
	channelList := []*models.Channel{}

	for rows.Next() {
		var c models.Channel
		var settingsJSON, metarrJSON []byte

		err := rows.Scan(
			&c.ID,
			&c.Name,
			&c.VideoDir,
			&c.JSONDir,
			&settingsJSON,
			&metarrJSON,
			&c.LastScan,
			&c.Paused,
			&c.CreatedAt,
			&c.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan channel: %w", err), true
		}

		// Unmarshal JSON fields
		if len(settingsJSON) > 0 {
			if err := json.Unmarshal(settingsJSON, &c.Settings); err != nil {
				return nil, fmt.Errorf("failed to unmarshal settings: %w", err), true
			}
		}
		if len(metarrJSON) > 0 {
			if err := json.Unmarshal(metarrJSON, &c.MetarrArgs); err != nil {
				return nil, fmt.Errorf("failed to unmarshal metarr settings: %w", err), true
			}
		}

		c.URLs = []string{}
		channelsMap[c.ID] = &c
		channelList = append(channelList, &c)
	}

	urlQuery := squirrel.
		Select(consts.QChanURLsChannelID, consts.QChanURLsURL).
		From(consts.DBChannelURLs).
		RunWith(cs.DB)

	urlRows, err := urlQuery.Query()
	if err != nil {
		return nil, fmt.Errorf("failed to query URLs: %w", err), true
	}
	defer urlRows.Close()

	for urlRows.Next() {
		var channelID int64
		var url string

		if err := urlRows.Scan(&channelID, &url); err != nil {
			return nil, fmt.Errorf("failed to scan URLs: %w", err), true
		}

		if channel, exists := channelsMap[channelID]; exists {
			channel.URLs = append(channel.URLs, url)
		}
	}

	return channelList, nil, len(channelList) > 0
}

// UpdateChannelMetarrArgsJSON updates args for Metarr output.
func (cs ChannelStore) UpdateChannelMetarrArgsJSON(key, val string, updateFn func(*models.MetarrArgs) error) (int64, error) {
	var metarrArgs json.RawMessage
	query := squirrel.
		Select(consts.QChanMetarr).
		From(consts.DBChannels).
		Where(squirrel.Eq{key: val}).
		RunWith(cs.DB)

	err := query.QueryRow().Scan(&metarrArgs)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("no channel found with key %q and value '%v'", key, val)
	} else if err != nil {
		return 0, fmt.Errorf("failed to get channel settings: %w", err)
	}

	// Unmarshal current settings
	var args models.MetarrArgs
	if err := json.Unmarshal(metarrArgs, &args); err != nil {
		return 0, fmt.Errorf("failed to unmarshal settings: %w", err)
	}

	// Apply the update
	if err := updateFn(&args); err != nil {
		return 0, fmt.Errorf("failed to update settings: %w", err)
	}

	// Marshal updated settings
	updatedArgs, err := json.Marshal(args)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal updated settings: %w", err)
	}

	// Print the updated settings
	logging.S(0, "Updated MetarrArgs: %s", string(updatedArgs))

	// Update the database with the new settings
	updateQuery := squirrel.
		Update(consts.DBChannels).
		Set(consts.QChanMetarr, updatedArgs).
		Where(squirrel.Eq{key: val}).
		RunWith(cs.DB)

	rtn, err := updateQuery.Exec()
	if err != nil {
		return 0, fmt.Errorf("failed to update channel settings in database: %w", err)
	}

	return rtn.RowsAffected()
}

// UpdateChannelSettingsJSON updates specific settings in the channel's settings JSON.
func (cs ChannelStore) UpdateChannelSettingsJSON(key, val string, updateFn func(*models.ChannelSettings) error) (int64, error) {
	var settingsJSON json.RawMessage
	query := squirrel.
		Select(consts.QChanSettings).
		From(consts.DBChannels).
		Where(squirrel.Eq{key: val}).
		RunWith(cs.DB)

	err := query.QueryRow().Scan(&settingsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("no channel found with key %q and value '%v'", key, val)
	} else if err != nil {
		return 0, fmt.Errorf("failed to get channel settings: %w", err)
	}

	// Unmarshal current settings
	var settings models.ChannelSettings
	if err := json.Unmarshal(settingsJSON, &settings); err != nil {
		return 0, fmt.Errorf("failed to unmarshal settings: %w", err)
	}

	// Apply the update
	if err := updateFn(&settings); err != nil {
		return 0, fmt.Errorf("failed to update settings: %w", err)
	}

	// Marshal updated settings
	updatedSettings, err := json.Marshal(settings)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal updated settings: %w", err)
	}

	// Print the updated settings
	logging.S(0, "Updated ChannelSettings: %s", string(updatedSettings))

	// Update the database with the new settings
	updateQuery := squirrel.
		Update(consts.DBChannels).
		Set(consts.QChanSettings, updatedSettings).
		Where(squirrel.Eq{key: val}).
		RunWith(cs.DB)

	rtn, err := updateQuery.Exec()
	if err != nil {
		return 0, fmt.Errorf("failed to update channel settings in database: %w", err)
	}

	return rtn.RowsAffected()
}

// UpdateChannelValue updates a single element in the database.
func (cs ChannelStore) UpdateChannelValue(key, val, col string, newVal any) error {
	if key == "" {
		return errors.New("please do not enter the key and value blank")
	}

	if !cs.channelExists(key, val) {
		return fmt.Errorf("channel with key %q and value %q does not exist", key, val)
	}

	query := squirrel.
		Update(consts.DBChannels).
		Where(squirrel.Eq{key: val}).
		Set(col, newVal).
		RunWith(cs.DB)

	// Print SQL query
	if logging.Level > 1 {
		sqlStr, args, _ := query.ToSql()
		fmt.Printf("Executing SQL: %s with args: %v\n", sqlStr, args)
	}

	// Execute query
	res, err := query.Exec()
	if err != nil {
		return err
	}

	// Ensure a row was updated
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return fmt.Errorf("update failed: no rows affected")
	} else {
		logging.S(0, "Successfully updated channel [%s=%s]: %q column was set to value %+v", key, val, col, newVal)
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
		return nil, errors.New("model entered has no ID")
	}

	query := squirrel.
		Select(consts.QVidURL).
		From(consts.DBVideos).
		Where(squirrel.And{
			squirrel.Eq{consts.QVidChanID: c.ID},
			squirrel.Eq{consts.QVidFinished: 1},
		}).
		RunWith(cs.DB)

	// const (
	// 	join     = "downloads ON downloads.video_id = videos.id"
	// 	vidURL   = "videos.url"
	// 	vidCID   = "videos.channel_id"
	// 	dlStatus = "downloads.status"
	// )
	// query := squirrel.
	// 	Select(vidURL).
	// 	From(consts.DBVideos).
	// 	Join(join).
	// 	Where(squirrel.And{
	// 		squirrel.Eq{vidCID: c.ID},
	// 		squirrel.Eq{dlStatus: consts.DLStatusCompleted},
	// 	}).
	// 	RunWith(cs.DB)

	logging.D(2, "Executing query to find downloaded videos: %v for channel ID %d", query, c.ID)

	rows, err := query.Query()
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logging.E(0, "Failed to close rows for channel %v: %v", c.Name, err)
		}
	}()

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

	logging.I("Found %d previously downloaded videos for channel ID %d", len(urls), c.ID)
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

	if err := query.QueryRow().Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return false
	} else if err != nil {
		logging.E(0, "Failed to scan for existent channel with key=%s val=%s: %v", key, val, err)
		return exists
	}
	return exists
}

// channelURLExists returns true if the channel exists in the database.
func (cs ChannelStore) channelURLExists(key, val string) bool {
	var exists bool
	query := squirrel.
		Select("1").
		From(consts.DBChannelURLs).
		Where(squirrel.Eq{key: val}).
		RunWith(cs.DB)

	if err := query.QueryRow().Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return false
	} else if err != nil {
		logging.E(0, "Failed to scan for existent channel URL with key=%s val=%s: %v", key, val, err)
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

	if err := query.QueryRow().Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return false
	} else if err != nil {
		logging.E(0, "Failed to check if channel with ID %d exists", id)
		return exists
	}
	return exists
}
