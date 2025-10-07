package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
	"tubarr/internal/app"
	"tubarr/internal/domain/consts"
	"tubarr/internal/interfaces"
	"tubarr/internal/models"
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

// GetChannelID gets the channel ID from an input key and value.
func (cs *ChannelStore) GetChannelID(key, val string) (int64, error) {
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
		Select(consts.QChanID).
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
		Select(
			consts.QChanURLsUsername,
			consts.QChanURLsPassword,
			consts.QChanURLsLoginURL,
		).
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

// DeleteVideoURLs deletes a URL from the downloaded database list.
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
func (cs *ChannelStore) GetNotifyURLs(id int64) ([]*models.Notification, error) {
	query := squirrel.
		Select(
			consts.QNotifyChanID,
			consts.QNotifyName,
			consts.QNotifyChanURL,
			consts.QNotifyURL,
		).
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
			logging.E("Failed to close rows for notify URLs in channel with ID %d", id)
		}
	}()

	// Collect notification models
	var notificationModels []*models.Notification

	for rows.Next() {
		var nModel models.Notification

		err := rows.Scan(
			&nModel.ChannelID,
			&nModel.Name,
			&nModel.ChannelURL,
			&nModel.NotifyURL,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan notification URL: %w", err)
		}

		notificationModels = append(notificationModels, &nModel)
	}

	// Check for errors from iterating over rows
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating notification URLs: %w", err)
	}

	return notificationModels, nil
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

	switch {
	case len(urls) > 0 && len(names) == 0:
		logging.S(0, "Deleted notify URLs %v for channel with ID '%d'.", urls, channelID)
	case len(urls) == 0 && len(names) > 0:
		logging.S(0, "Deleted notify URLs with friendly names %v for channel with ID '%d'.", names, channelID)
	case len(urls) > 0 && len(names) > 0:
		logging.S(0, "Deleted notify URLs: %v and notify URLs with friendly names %v for channel with ID '%d'.", urls, names, channelID)
	default:
		logging.S(0, "No notify URLs to delete.")
	}
	return nil
}

// AddNotifyURLs sets notification pairs in the database.
func (cs *ChannelStore) AddNotifyURLs(channelID int64, notifications []*models.Notification) error {
	tx, err := cs.DB.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if err := tx.Rollback(); err != nil {
				logging.E("Failed to abort transaction for channel ID: %d. Could not abort transacting notifications: %v: %v", channelID, notifications, err)
			}
		}
	}()

	for _, n := range notifications {
		chanURL := n.ChannelURL
		notifyURL := n.NotifyURL
		name := n.Name

		if err := cs.addNotifyURL(tx, channelID, chanURL, notifyURL, name); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// addNotifyURL sets a notification table entry for a channel with a given ID.
func (cs *ChannelStore) addNotifyURL(tx *sql.Tx, id int64, chanURL, notifyURL, notifyName string) error {
	if notifyURL == "" {
		return errors.New("please enter a notification URL")
	}
	if notifyName == "" {
		notifyName = notifyURL
	}

	const querySuffix = "ON CONFLICT (channel_id, notify_url) DO UPDATE SET notify_url = EXCLUDED.notify_url, updated_at = EXCLUDED.updated_at"

	now := time.Now()
	query := squirrel.
		Insert(consts.DBNotifications).
		Columns(
			consts.QNotifyChanID,
			consts.QNotifyName,
			consts.QNotifyChanURL,
			consts.QNotifyURL,
			consts.QNotifyCreatedAt,
			consts.QNotifyUpdatedAt,
		).
		Values(
			id,
			notifyName,
			chanURL,
			notifyURL,
			now,
			now,
		).
		Suffix(querySuffix).
		RunWith(tx)

	if _, err := query.Exec(); err != nil {
		return err
	}

	logging.S(0, "Added notification URL %q to channel with ID: %d", notifyURL, id)
	return nil
}

// AddAuth adds authentication details to a channel.
func (cs ChannelStore) AddAuth(chanID int64, authDetails map[string]*models.ChannelAccessDetails) error {
	if !cs.channelExistsID(chanID) {
		return fmt.Errorf("channel with ID %d does not exist", chanID)
	}

	if authDetails == nil {
		logging.D(1, "No authorization details to add for channel with ID %d", chanID)
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
			Where(squirrel.Eq{consts.QChanURLsChannelID: chanID}).
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
	case len(c.URLModels) == 0:
		return 0, errors.New("must enter at least one URL for the channel")
	case c.ChanSettings.VideoDir == "":
		return 0, errors.New("must enter a video directory where downloads will be stored")
	}

	if cs.channelExists(consts.QChanName, c.Name) {
		return 0, fmt.Errorf("channel with name %q already exists", c.Name)
	}

	// JSON dir
	if c.ChanSettings.JSONDir == "" {
		c.ChanSettings.JSONDir = c.ChanSettings.VideoDir
	}

	// Convert settings to JSON
	settingsJSON, err := json.Marshal(c.ChanSettings)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal settings: %w", err)
	}

	// Convert metarr settings to JSON
	metarrJSON, err := json.Marshal(c.ChanMetarrArgs)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal metarr settings: %w", err)
	}

	// Insert into the channels table
	now := time.Now()
	query := squirrel.
		Insert(consts.DBChannels).
		Columns(
			consts.QChanName,
			consts.QChanSettings,
			consts.QChanMetarr,
			consts.QChanLastScan,
			consts.QChanCreatedAt,
			consts.QChanUpdatedAt,
		).
		Values(
			c.Name,
			settingsJSON,
			metarrJSON,
			now,
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
	for _, cu := range c.URLModels {
		urlQuery := squirrel.
			Insert(consts.DBChannelURLs).
			Columns(
				consts.QChanURLsChannelID,
				consts.QChanURLsURL,
				consts.QChanURLsUsername,
				consts.QChanURLsPassword,
				consts.QChanURLsLoginURL,
				consts.QChanURLsLastScan,
				consts.QChanURLsCreatedAt,
				consts.QChanURLsUpdatedAt,
			).
			Values(
				id,
				cu.URL,
				cu.Username,
				cu.Password,
				cu.LoginURL,
				now,
				now,
				now,
			).
			RunWith(cs.DB)

		if _, err := urlQuery.Exec(); err != nil {
			return 0, fmt.Errorf("failed to insert URL %q for channel ID %d: %w", cu.URL, id, err)
		}
	}

	cURLs := c.GetURLs()
	logging.S(0, "Successfully added channel (ID: %d)\n\nName: %s\nURLs: %v\nCrawl Frequency: %d minutes\nFilters: %v\nSettings: %+v\nMetarr Operations: %+v",
		id, c.Name, cURLs, c.ChanSettings.CrawlFreq, c.ChanSettings.Filters, c.ChanSettings, c.ChanMetarrArgs)

	return id, nil
}

// DeleteChannel deletes a channel from the database with a given key/value.
func (cs *ChannelStore) DeleteChannel(key, val string) error {
	if key == "" || val == "" {
		return errors.New("please provide key and value to delete a channel")
	}

	query := squirrel.
		Delete(consts.DBChannels).
		Where(squirrel.Eq{key: val}).
		RunWith(cs.DB)

	result, err := query.Exec()
	if err != nil {
		return fmt.Errorf("failed to delete channel: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("no channel found with %q = %q", key, val)
	}

	return nil
}

// CheckOrUnlockChannel checks if a blocked channel has exceeded its timeout and unlocks it.
//
// Returns true if the channel was unlocked, false if still blocked.
func (cs *ChannelStore) CheckOrUnlockChannel(c *models.Channel) (bool, error) {

	// NOT BLOCKED:
	if !c.IsBlocked() {
		return true, nil // Not blocked, consider it "unlocked"
	}

	// BLOCKED LOGIC:
	logging.W("Channel %q is currently blocked by %v", c.Name, c.ChanSettings.BotBlockedHostnames)

	if len(c.ChanSettings.BotBlockedHostnames) == 0 {
		return false, nil // Invalid state, keep blocked
	}

	// Initialize timestamps map if nil
	if c.ChanSettings.BotBlockedTimestamps == nil {
		c.ChanSettings.BotBlockedTimestamps = make(map[string]time.Time)
	}

	// Check each blocked hostname to see if any have exceeded timeout
	stillBlockedHostnames := make([]string, 0, len(c.ChanSettings.BotBlockedHostnames))
	anyUnlocked := false

	for _, hostname := range c.ChanSettings.BotBlockedHostnames {
		timeoutMinutes, exists := consts.BotTimeoutMap[hostname]
		if !exists {
			// No timeout configured, keep it blocked
			stillBlockedHostnames = append(stillBlockedHostnames, hostname)
			continue
		}

		// Get the timestamp for this specific hostname
		blockedTime, exists := c.ChanSettings.BotBlockedTimestamps[hostname]
		if !exists || blockedTime.IsZero() {
			// No timestamp found for this hostname, keep it blocked
			stillBlockedHostnames = append(stillBlockedHostnames, hostname)
			logging.W("No timestamp found for hostname %q, keeping blocked", hostname)
			continue
		}

		minutesSinceBlock := time.Since(blockedTime).Minutes()
		if minutesSinceBlock >= timeoutMinutes {
			// This hostname's timeout has expired
			logging.S(0, "Unlocking hostname %q for channel %d (%s) after timeout", hostname, c.ID, c.Name)
			anyUnlocked = true
			// Remove from timestamps map
			delete(c.ChanSettings.BotBlockedTimestamps, hostname)
		} else {
			// Still blocked
			stillBlockedHostnames = append(stillBlockedHostnames, hostname)
			logging.W("%.0f more minute(s) before channel unlocks for domain %q. (Blocked on: %v)",
				(timeoutMinutes - minutesSinceBlock), hostname, c.ChanSettings.BotBlockedTimestamps[hostname])
		}
	}

	// Update the channel settings
	if anyUnlocked {
		// Update in-memory copy
		c.ChanSettings.BotBlockedHostnames = stillBlockedHostnames

		// If no hostnames remain blocked, clear the blocked state entirely
		if len(stillBlockedHostnames) == 0 {
			c.ChanSettings.BotBlocked = false
			c.ChanSettings.BotBlockedTimestamps = make(map[string]time.Time)
		}

		// Persist changes
		_, err := cs.UpdateChannelSettingsJSON(consts.QChanID, strconv.FormatInt(c.ID, 10), func(s *models.ChannelSettings) error {
			s.BotBlocked = c.ChanSettings.BotBlocked
			s.BotBlockedHostnames = c.ChanSettings.BotBlockedHostnames
			s.BotBlockedTimestamps = c.ChanSettings.BotBlockedTimestamps
			return nil
		})
		if err != nil {
			return false, fmt.Errorf("failed to unlock channel: %w", err)
		}
	}

	// Return true only if ALL hostnames are now unlocked
	if len(stillBlockedHostnames) == 0 {
		logging.S(0, "Channel %d (%s) fully unlocked - all hostnames cleared", c.ID, c.Name)
		return true, nil
	}

	// Still some blocked hostnames remaining
	logging.W("Unlock channel %q manually for hostnames %v with:\n\ntubarr channel unblock -n %q\n",
		c.Name, stillBlockedHostnames, c.Name)
	return false, nil
}

// CrawlChannelIgnore crawls a channel and adds the latest videos to the ignore list.
func (cs *ChannelStore) CrawlChannelIgnore(key, val string, s interfaces.Store, ctx context.Context) error {
	var (
		c                    models.Channel
		settings, metarrJSON json.RawMessage
		err                  error
	)

	query := squirrel.
		Select(
			consts.QChanID,
			consts.QChanName,
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
			&c.Name,
			&settings,
			&metarrJSON,
			&c.LastScan,
			&c.CreatedAt,
			&c.UpdatedAt,
		); err != nil {
		return fmt.Errorf("failed to scan channel: %w", err)
	}

	// Unmarshal settings
	if err := json.Unmarshal(settings, &c.ChanSettings); err != nil {
		return fmt.Errorf("parsing channel settings: %w", err)
	}

	// Unmarshal metarr settings
	if len(metarrJSON) > 0 {
		if err := json.Unmarshal(metarrJSON, &c.ChanMetarrArgs); err != nil {
			return fmt.Errorf("parsing metarr settings: %w", err)
		}
	}

	// Initialize URL list
	c.URLModels, err = cs.FetchChannelURLModels(c.ID)
	if err != nil {
		return fmt.Errorf("failed to fetch URL models for channel: %w", err)
	}

	cURLs := c.GetURLs()
	logging.D(1, "Retrieved channel (ID: %d) with URLs: %+v", c.ID, cURLs)

	// Process crawling using the updated channel object
	if err := app.ChannelCrawlIgnoreNew(s, &c, ctx); err != nil {
		return err
	}
	return nil
}

// DownloadVideoURLs downloads new video URLs to a channel.
func (cs *ChannelStore) DownloadVideoURLs(key, val string, c *models.Channel, s interfaces.Store, videoURLs []string, ctx context.Context) error {

	cURLs := c.GetURLs()
	logging.D(1, "Retrieved channel (ID: %d) with URLs: %+v", c.ID, cURLs)

	// Process crawling using the updated channel object
	return app.DownloadVideosToChannel(s, cs, c, videoURLs, ctx)
}

// CrawlChannel crawls a channel and finds video URLs which have not yet been downloaded.
func (cs *ChannelStore) CrawlChannel(key, val string, c *models.Channel, s interfaces.Store, ctx context.Context) error {

	cURLs := c.GetURLs()
	logging.D(1, "Retrieved channel (ID: %d) with URLs: %+v", c.ID, cURLs)

	// Process crawling using the updated channel object
	return app.ChannelCrawl(s, cs, c, ctx)
}

// FetchChannelModel fills the channel model from the database.
func (cs *ChannelStore) FetchChannelModel(key, val string) (*models.Channel, bool, error) {
	var (
		settings, metarrJSON json.RawMessage
		err                  error
	)

	query := squirrel.
		Select(
			consts.QChanID,
			consts.QChanName,
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
			&c.Name,
			&settings,
			&metarrJSON,
			&c.LastScan,
			&c.CreatedAt,
			&c.UpdatedAt,
		); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, true, fmt.Errorf("failed to scan channel: %w", err)
	}

	// Unmarshal settings
	if err := json.Unmarshal(settings, &c.ChanSettings); err != nil {
		return nil, true, fmt.Errorf("parsing channel settings: %w", err)
	}

	// Unmarshal metarr settings
	if len(metarrJSON) > 0 {
		if err := json.Unmarshal(metarrJSON, &c.ChanMetarrArgs); err != nil {
			return nil, true, fmt.Errorf("parsing metarr settings: %w", err)
		}
	}

	c.URLModels, err = cs.FetchChannelURLModels(c.ID)
	if err != nil {
		return nil, true, fmt.Errorf("failed to fetch URL models for channel: %w", err)
	}

	return &c, true, nil
}

// FetchAllChannels retrieves all channels in the database.
func (cs *ChannelStore) FetchAllChannels() (channels []*models.Channel, hasRows bool, err error) {
	query := squirrel.
		Select(
			consts.QChanID,
			consts.QChanName,
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
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	} else if err != nil {
		return nil, true, fmt.Errorf("failed to query channels: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logging.E("Failed to close rows: %v", err)
		}
	}()

	channelsMap := make(map[int64]*models.Channel)
	channelList := []*models.Channel{}

	for rows.Next() {
		var c models.Channel
		var settingsJSON, metarrJSON []byte

		if err := rows.Scan(
			&c.ID,
			&c.Name,
			&settingsJSON,
			&metarrJSON,
			&c.LastScan,
			&c.CreatedAt,
			&c.UpdatedAt,
		); err != nil {
			return nil, true, fmt.Errorf("failed to scan channel: %w", err)
		}

		if len(settingsJSON) > 0 {
			if err := json.Unmarshal(settingsJSON, &c.ChanSettings); err != nil {
				return nil, true, fmt.Errorf("failed to unmarshal settings: %w", err)
			}
		}
		if len(metarrJSON) > 0 {
			if err := json.Unmarshal(metarrJSON, &c.ChanMetarrArgs); err != nil {
				return nil, true, fmt.Errorf("failed to unmarshal metarr settings: %w", err)
			}
		}

		c.URLModels = []*models.ChannelURL{}
		channelsMap[c.ID] = &c
		channelList = append(channelList, &c)
	}

	urlMap, err := cs.fetchChannelURLModelsMap(0)
	if err != nil {
		return nil, true, err
	}

	for _, c := range channelList {
		if urls, ok := urlMap[c.ID]; ok {
			c.URLModels = urls
		}
	}

	return channelList, len(channelList) > 0, nil
}

// UpdateChannelMetarrArgsJSON updates args for Metarr output.
func (cs *ChannelStore) UpdateChannelMetarrArgsJSON(key, val string, updateFn func(*models.MetarrArgs) error) (int64, error) {
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
func (cs *ChannelStore) UpdateChannelSettingsJSON(key, val string, updateFn func(*models.ChannelSettings) error) (int64, error) {
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
func (cs *ChannelStore) UpdateChannelValue(key, val, col string, newVal any) error {
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
		sqlStr, args, err := query.ToSql()
		if err != nil {
			logging.W("Cannot print SQL string for update query in channel with %s %q: %v", key, val, err)
		} else {
			logging.P("Executing SQL: %s with args: %v\n", sqlStr, args)
		}
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
	now := time.Now()
	query := squirrel.
		Update(consts.DBChannels).
		Set(consts.QChanLastScan, now).
		Set(consts.QChanUpdatedAt, now).
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
func (cs *ChannelStore) LoadGrabbedURLs(c *models.Channel) (urls []string, err error) {
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

	logging.D(2, "Executing query to find downloaded videos: %v for channel %q", query, c.Name)

	rows, err := query.Query()
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logging.E("Failed to close rows for channel %v: %v", c.Name, err)
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

	logging.I("Found %d previously downloaded videos for channel %q", len(urls), c.Name)
	return urls, nil
}

// FetchChannelURLModels fetches a filled list of ChannelURL models.
func (cs *ChannelStore) FetchChannelURLModels(channelID int64) ([]*models.ChannelURL, error) {
	urlQuery := squirrel.
		Select(
			consts.QChanURLsID,
			consts.QChanURLsURL,
			consts.QChanURLsUsername,
			consts.QChanURLsPassword,
			consts.QChanURLsLoginURL,
			consts.QChanURLsIsManual,
			consts.QChanURLsLastScan,
			consts.QChanURLsCreatedAt,
			consts.QChanURLsUpdatedAt,
		).
		From(consts.DBChannelURLs).
		Where(squirrel.Eq{consts.QChanURLsChannelID: channelID}).
		RunWith(cs.DB)

	rows, err := urlQuery.Query()
	if err != nil {
		return nil, fmt.Errorf("failed to query channel URLs: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logging.E("Failed to close rows: %v", err)
		}
	}()

	var urlModels []*models.ChannelURL
	for rows.Next() {
		cu := &models.ChannelURL{}
		var username, password, loginURL sql.NullString

		if err := rows.Scan(
			&cu.ID,
			&cu.URL,
			&username,
			&password,
			&loginURL,
			&cu.IsManual,
			&cu.LastScan,
			&cu.CreatedAt,
			&cu.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan channel URL: %w", err)
		}

		// Fill credentials
		cu.Username = nullString(username)
		cu.Password = nullString(password)
		cu.LoginURL = nullString(loginURL)

		urlModels = append(urlModels, cu)
	}

	return urlModels, nil
}

// AddChannelURL adds a new channel URL to the database.
func (cs *ChannelStore) AddChannelURL(channelID int64, cu *models.ChannelURL, isManual bool) (chanURLID int64, err error) {
	if !cs.channelExistsID(channelID) {
		return 0, fmt.Errorf("channel with ID %d does not exist", channelID)
	}

	now := time.Now()
	query := squirrel.
		Insert(consts.DBChannelURLs).
		Columns(
			consts.QChanURLsChannelID,
			consts.QChanURLsURL,
			consts.QChanURLsLoginURL,
			consts.QChanURLsPassword,
			consts.QChanURLsLoginURL,
			consts.QChanURLsIsManual,
			consts.QChanURLsLastScan,
			consts.QChanURLsCreatedAt,
			consts.QChanURLsUpdatedAt,
		).
		Values(
			channelID,
			cu.URL,
			cu.Username,
			cu.Password,
			cu.LoginURL,
			isManual,
			now,
			now,
			now,
		).
		RunWith(cs.DB)

	result, err := query.Exec()
	if err != nil {
		return 0, fmt.Errorf("failed to insert channel URL: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last insert ID: %w", err)
	}

	logging.D(1, "Added channel URL %q (ID: %d, is_manual: %v) to channel ID %d", cu.URL, id, isManual, channelID)
	return id, nil
}

// ******************************** Private ********************************

// channelExists returns true if the channel exists in the database.
func (cs *ChannelStore) channelExists(key, val string) bool {
	var count int
	query := squirrel.
		Select("COUNT(1)").
		From(consts.DBChannels).
		Where(squirrel.Eq{key: val}).
		RunWith(cs.DB)

	if err := query.QueryRow().Scan(&count); err != nil {
		logging.E("failed to check if channel exists with key=%s val=%s: %v", key, val, err)
		return false
	}
	return count > 0
}

// channelURLExists returns true if the channel URL exists in the database.
func (cs *ChannelStore) channelURLExists(key, val string) bool {
	var count int
	query := squirrel.
		Select("COUNT(1)").
		From(consts.DBChannelURLs).
		Where(squirrel.Eq{key: val}).
		RunWith(cs.DB)

	if err := query.QueryRow().Scan(&count); err != nil {
		logging.E("failed to check if channel URL exists with key=%s val=%s: %v", key, val, err)
		return false
	}
	return count > 0
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
		logging.E("Failed to check if channel with ID %d exists", id)
		return exists
	}
	return exists
}

// fetchChannelURLs retrieves all ChannelURL rows for a given channel.
//
// If channelID == 0, it fetches URLs for all channels.
func (cs *ChannelStore) fetchChannelURLModelsMap(channelID int64) (map[int64][]*models.ChannelURL, error) {
	query := squirrel.
		Select(
			consts.QChanURLsID,
			consts.QChanURLsChannelID,
			consts.QChanURLsURL,
			consts.QChanURLsUsername,
			consts.QChanURLsPassword,
			consts.QChanURLsLoginURL,
			consts.QChanURLsIsManual,
			consts.QChanURLsLastScan,
			consts.QChanURLsCreatedAt,
			consts.QChanURLsUpdatedAt,
		).
		From(consts.DBChannelURLs)

	if channelID != 0 {
		query = query.Where(squirrel.Eq{consts.QChanURLsChannelID: channelID})
	}

	rows, err := query.RunWith(cs.DB).Query()
	if err != nil {
		return nil, fmt.Errorf("failed to query channel URLs: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logging.E("Failed to close rows: %v", err)
		}
	}()

	urlMap := make(map[int64][]*models.ChannelURL)

	for rows.Next() {
		cu := &models.ChannelURL{}
		var username, password, loginURL sql.NullString
		var cid int64

		if err := rows.Scan(
			&cu.ID,
			&cid,
			&cu.URL,
			&username,
			&password,
			&loginURL,
			&cu.IsManual,
			&cu.LastScan,
			&cu.CreatedAt,
			&cu.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan channel URL: %w", err)
		}

		// Fill credentials
		cu.Username = nullString(username)
		cu.Password = nullString(password)
		cu.LoginURL = nullString(loginURL)

		urlMap[cid] = append(urlMap[cid], cu)
	}

	return urlMap, nil
}

// nullString converts an sql.NullString into a string, or "".
func nullString(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}
