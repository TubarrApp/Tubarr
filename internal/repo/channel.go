package repo

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"tubarr/internal/abstractions"
	"tubarr/internal/auth"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/paths"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"
	"tubarr/internal/validation"

	"github.com/Masterminds/squirrel"
)

// ChannelStore holds a pointer to the sql.DB.
type ChannelStore struct {
	DB          *sql.DB
	PasswordMgr *auth.PasswordManager
}

// GetChannelStore returns a channel store instance with injected database.
func GetChannelStore(db *sql.DB) (*ChannelStore, error) {
	pm, err := auth.NewPasswordManager(paths.HomeTubarrDir)
	if err != nil {
		return nil, err
	}

	return &ChannelStore{
		DB:          db,
		PasswordMgr: pm,
	}, nil
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
		return 0, errors.New("please input a unique constrained value, such as ID or name")
	}

	var id int64
	query := squirrel.
		Select(consts.QChanID).
		From(consts.DBChannels).
		Where(squirrel.Eq{key: val}).
		RunWith(cs.DB)

	if err := query.QueryRow().Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("channel with %s %q does not exist", key, val)
		}
		return 0, err
	}

	return id, nil
}

// GetLatestDownloadedVideos returns the latest 'limit' downloaded videos.
func (cs *ChannelStore) GetLatestDownloadedVideos(channel *models.Channel, limit int) ([]models.Video, error) {
	var videos []models.Video

	query := squirrel.
		Select(
			consts.QVidID,
			consts.QVidChanID,
			consts.QVidChanURLID,
			consts.QVidThumbnailURL,
			consts.QVidFinished,
			consts.QVidURL,
			consts.QVidTitle,
			consts.QVidDescription,
			consts.QVidVideoPath,
			consts.QVidJSONPath,
			consts.QVidUploadDate,
			consts.QVidMetadata,
			consts.QVidDLStatus,
			consts.QVidCreatedAt,
			consts.QVidUpdatedAt,
		).
		From(consts.DBVideos).
		Where(squirrel.Eq{consts.QVidChanID: channel.ID}).
		Where(squirrel.Eq{consts.QVidFinished: 1}).            // Only finished downloads
		OrderBy(fmt.Sprintf("%s DESC", consts.QVidUpdatedAt)). // Most recent first
		Limit(uint64(limit))

	sqlPlaceholder, args, err := query.PlaceholderFormat(squirrel.Question).ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	rows, err := cs.DB.Query(sqlPlaceholder, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query videos: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var video models.Video
		var thumbnailURL sql.NullString
		var description sql.NullString
		var videoPath sql.NullString
		var jsonPath sql.NullString
		var uploadDate sql.NullTime
		var metadataStr sql.NullString
		var channelURLID sql.NullInt64
		var downloadStatusStr sql.NullString

		err := rows.Scan(
			&video.ID,
			&video.ChannelID,
			&channelURLID,
			&thumbnailURL,
			&video.Finished,
			&video.URL,
			&video.Title,
			&description,
			&videoPath,
			&jsonPath,
			&uploadDate,
			&metadataStr,
			&downloadStatusStr,
			&video.CreatedAt,
			&video.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan video row: %w", err)
		}

		// Unmarshal download status JSON
		if downloadStatusStr.Valid && downloadStatusStr.String != "" {
			if err := json.Unmarshal([]byte(downloadStatusStr.String), &video.DownloadStatus); err != nil {
				logging.W("Failed to unmarshal download status for video %d: %v", video.ID, err)
			}
		}

		// Handle nullable fields
		if channelURLID.Valid {
			video.ChannelURLID = channelURLID.Int64
		}
		if thumbnailURL.Valid {
			video.ThumbnailURL = thumbnailURL.String
		}
		if description.Valid {
			video.Description = description.String
		}
		if videoPath.Valid {
			video.VideoPath = videoPath.String
		}
		if jsonPath.Valid {
			video.JSONPath = jsonPath.String
		}
		if uploadDate.Valid {
			video.UploadDate = uploadDate.Time
		}
		videos = append(videos, video)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return videos, nil
}

// GetAuth retrieves authentication details for a specific URL in a channel.
func (cs *ChannelStore) GetAuth(channelID int64, url string) (username, password, loginURL string, err error) {
	var u, p, l sql.NullString // Use sql.NullString to handle NULL values

	query := squirrel.
		Select(
			consts.QChanURLUsername,
			consts.QChanURLPassword,
			consts.QChanURLLoginURL,
		).
		From(consts.DBChannelURLs).
		Where(squirrel.And{
			squirrel.Eq{consts.QChanURLChannelID: channelID},
			squirrel.Eq{consts.QChanURLURL: url},
		}).
		RunWith(cs.DB)

	if err = query.QueryRow().Scan(&u, &p, &l); err != nil {
		logging.I("No auth details found in database for channel ID: %d, URL: %s", channelID, url)
		return "", "", "", err
	}

	var decryptedPassword string
	if p.String != "" {
		decryptedPassword, err = cs.PasswordMgr.Decrypt(p.String)
		if err != nil {
			return "", "", "", err
		}
	}
	return u.String, decryptedPassword, l.String, nil
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
	logging.S("Deleted URLs %q for channel with ID '%d'", urls, channelID)
	return nil
}

// UpdateChannelFromConfig updates the channel settings from a config file if it exists.
func (cs ChannelStore) UpdateChannelFromConfig(c *models.Channel) (err error) {
	if c == nil {
		return fmt.Errorf("dev error: channel sent in nil")
	}

	cfgFile := c.ChanSettings.ChannelConfigFile
	if cfgFile == "" {
		logging.D(2, "No config file path, nothing to apply")
		return nil
	}

	logging.I("Applying configurations to channel %q from config file %q...", c.Name, cfgFile)
	if _, err := validation.ValidateFile(cfgFile, false); err != nil {
		return err
	}

	// Use Viper to load in flags
	if err := file.LoadConfigFile(cfgFile); err != nil {
		return err
	}

	// Make []byte copy of settings before
	settingsBeforeJSON, metarrBeforeJSON := makeSettingsMetarrArgsCopy(c.ChanSettings, c.ChanMetarrArgs, c.Name)

	// Apply changes to model
	if err := cs.applyConfigChannelSettings(c); err != nil {
		return err
	}

	if err := cs.applyConfigChannelMetarrSettings(c); err != nil {
		return err
	}

	// []byte copy of settings after for comparison
	settingsAfterJSON, metarrAfterJSON := makeSettingsMetarrArgsCopy(c.ChanSettings, c.ChanMetarrArgs, c.Name)

	// Return early if unchanged
	if bytes.Equal(settingsBeforeJSON, settingsAfterJSON) &&
		bytes.Equal(metarrBeforeJSON, metarrAfterJSON) {
		logging.D(1, "No changes to channel from config file.")
		return nil
	}

	// Propagate into database
	chanID := strconv.FormatInt(c.ID, 10)
	_, err = cs.UpdateChannelSettingsJSON(consts.QChanID, chanID, func(s *models.Settings) error {
		if c.ChanSettings == nil {
			return fmt.Errorf("c.ChanSettings is nil")
		}
		*s = *c.ChanSettings
		return nil
	})
	if err != nil {
		return err
	}

	_, err = cs.UpdateChannelMetarrArgsJSON(consts.QChanID, chanID, func(m *models.MetarrArgs) error {
		if c.ChanMetarrArgs == nil {
			return fmt.Errorf("c.ChanMetarrArgs is nil")
		}
		*m = *c.ChanMetarrArgs
		return nil
	})
	if err != nil {
		return err
	}

	// Reload URL models
	c.URLModels, err = cs.GetChannelURLModels(c)
	if err != nil {
		return fmt.Errorf("failed to reload URL models after cascade: %w", err)
	}

	logging.S("Updated channel %q from config file", c.Name)
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
	logging.S("Added URL %q to ignore list for channel with ID '%d'", ignoreURL, channelID)
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
		logging.S("Deleted notify URLs %v for channel with ID '%d'.", urls, channelID)
	case len(urls) == 0 && len(names) > 0:
		logging.S("Deleted notify URLs with friendly names %v for channel with ID '%d'.", names, channelID)
	case len(urls) > 0 && len(names) > 0:
		logging.S("Deleted notify URLs: %v and notify URLs with friendly names %v for channel with ID '%d'.", urls, names, channelID)
	default:
		logging.S("No notify URLs to delete.")
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
		if p := recover(); p != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				logging.E("Panic rollback failed for channel %d: %v", channelID, rbErr)
			}
			panic(p)
		} else if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				logging.E("Rollback failed for channel %d (original error: %v): %v", channelID, err, rbErr)
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

// AddAuth adds authentication details to a channel.
func (cs ChannelStore) AddAuth(chanID int64, authDetails map[string]*models.ChannelAccessDetails) (err error) {
	if !cs.channelExistsID(chanID) {
		return fmt.Errorf("channel with ID %d does not exist", chanID)
	}

	if authDetails == nil {
		logging.D(1, "No authorization details to add for channel with ID %d", chanID)
		return nil
	}

	for chanURL, a := range authDetails {
		if !cs.channelURLExists(consts.QChanURLURL, chanURL) {
			return fmt.Errorf("channel with URL %q does not exist", chanURL)
		}

		if a.EncryptedPassword == "" && a.Password != "" {
			a.EncryptedPassword, err = cs.PasswordMgr.Encrypt(a.Password)
			if err != nil {
				return err
			}
		}

		query := squirrel.
			Update(consts.DBChannelURLs).
			Set(consts.QChanURLUsername, a.Username).
			Set(consts.QChanURLPassword, a.EncryptedPassword).
			Set(consts.QChanURLLoginURL, a.LoginURL).
			Where(squirrel.Eq{consts.QChanURLChannelID: chanID}).
			RunWith(cs.DB)

		if _, err := query.Exec(); err != nil {
			return err
		}
		logging.S("Added authentication details for URL %q in channel with ID %d", chanURL, chanID)
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

	// Begin transaction
	tx, err := cs.DB.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Ensure rollback on error
	defer func() {
		if p := recover(); p != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				logging.E("Panic rollback failed for channel %q: %v", c.Name, rbErr)
			}
			panic(p)
		} else if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				logging.E("Rollback failed for channel %q (original error: %v): %v", c.Name, err, rbErr)
			}
		}
	}()

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
		RunWith(tx)

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
	logging.D(1, "Inserting %d channel URLs for channel ID %d", len(c.URLModels), id)
	for i, cu := range c.URLModels {
		logging.D(1, "Inserting URL %d: %q", i+1, cu.URL)

		// Validate URL format
		if _, urlErr := url.ParseRequestURI(cu.URL); urlErr != nil {
			err = fmt.Errorf("invalid URL %q: %w", cu.URL, urlErr)
			return 0, err
		}

		if cu.EncryptedPassword == "" && cu.Password != "" {
			cu.EncryptedPassword, err = cs.PasswordMgr.Encrypt(cu.Password)
		}

		urlQuery := squirrel.
			Insert(consts.DBChannelURLs).
			Columns(
				consts.QChanURLChannelID,
				consts.QChanURLURL,
				consts.QChanURLUsername,
				consts.QChanURLPassword,
				consts.QChanURLLoginURL,
				consts.QChanURLIsManual,
				consts.QChanURLLastScan,
				consts.QChanURLCreatedAt,
				consts.QChanURLUpdatedAt,
			).
			Values(
				id,
				cu.URL,
				cu.Username,
				cu.EncryptedPassword,
				cu.LoginURL,
				false, // Not a manual URL
				now,
				now,
				now,
			).
			RunWith(tx)

		result, err = urlQuery.Exec()
		if err != nil {
			return 0, fmt.Errorf("failed to insert URL %q for channel ID %d: %w", cu.URL, id, err)
		}

		urlID, err := result.LastInsertId()
		if err != nil {
			return 0, fmt.Errorf("failed to get last insert ID for URL %q: %w", cu.URL, err)
		}
		cu.ID = urlID

		logging.D(1, "Successfully inserted URL %q with ID %d", cu.URL, urlID)
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	cURLs := c.GetURLs()
	logging.S("Successfully added channel (ID: %d)\n\nName: %s\nURLs: %v\nCrawl Frequency: %d minutes\nFilters: %v\nSettings: %+v\nMetarr Operations: %+v",
		id, c.Name, cURLs, c.GetCrawlFreq(), c.ChanSettings.Filters, c.ChanSettings, c.ChanMetarrArgs)

	return id, nil
}

// DeleteChannel deletes a channel from the database with a given key/value.
func (cs *ChannelStore) DeleteChannel(key, val string) error {
	if err := validation.ValidateColumnKeyVal(key, val); err != nil {
		return err
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
			logging.S("Unlocking hostname %q for channel %d (%s) after timeout", hostname, c.ID, c.Name)
			anyUnlocked = true
			// Remove from timestamps map
			delete(c.ChanSettings.BotBlockedTimestamps, hostname)
		} else {
			// Still blocked
			stillBlockedHostnames = append(stillBlockedHostnames, hostname)

			// Print time remaining until unlock
			blockedAt := c.ChanSettings.BotBlockedTimestamps[hostname]
			timeout := time.Duration(timeoutMinutes) * time.Minute
			unlockTime := blockedAt.Add(timeout)
			remainingDuration := time.Until(unlockTime)

			logging.W("%v remaining before channel unlocks for domain %q (Blocked on: %v)",
				remainingDuration.Round(time.Second),
				hostname,
				blockedAt.Local().Format("Mon, Jan 2 2006, 15:04:05 MST"))
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
		_, err := cs.UpdateChannelSettingsJSON(consts.QChanID, strconv.FormatInt(c.ID, 10), func(s *models.Settings) error {
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
		logging.S("Channel %d (%s) fully unlocked - all hostnames cleared", c.ID, c.Name)
		return true, nil
	}

	// Still some blocked hostnames remaining
	logging.W("Unlock channel %q manually for hostnames %v with:\n\ntubarr channel unblock -n %q\n",
		c.Name, stillBlockedHostnames, c.Name)
	return false, nil
}

// GetChannelModel fills the channel model from the database.
func (cs *ChannelStore) GetChannelModel(key, val string) (*models.Channel, bool, error) {
	if err := validation.ValidateColumnKeyVal(key, val); err != nil {
		return nil, false, err
	}

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

	c.URLModels, err = cs.GetChannelURLModels(&c)
	if err != nil {
		return nil, true, fmt.Errorf("failed to fetch URL models for channel: %w", err)
	}

	return &c, true, nil
}

// GetAllChannels retrieves all channels in the database.
func (cs *ChannelStore) GetAllChannels() (channels []*models.Channel, hasRows bool, err error) {
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

	urlMap, err := cs.getChannelURLModelsMap(0)
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
	if err := validation.ValidateColumnKeyVal(key, val); err != nil {
		return 0, err
	}

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
	logging.S("Updated MetarrArgs: %s", string(updatedArgs))

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
func (cs *ChannelStore) UpdateChannelSettingsJSON(key, val string, updateFn func(*models.Settings) error) (int64, error) {
	if err := validation.ValidateColumnKeyVal(key, val); err != nil {
		return 0, err
	}

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
	var settings models.Settings
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
	logging.S("Updated ChannelSettings: %s", string(updatedSettings))

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
	}

	logging.S("Successfully updated channel [%s=%s]: %q column was set to value %+v", key, val, col, newVal)
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

// GetDownloadedOrIgnoredURLs loads already downloaded or ignored URLs from the database.
func (cs *ChannelStore) GetDownloadedOrIgnoredURLs(c *models.Channel) (urls []string, err error) {
	if c.ID == 0 {
		return nil, errors.New("model entered has no ID")
	}

	query := squirrel.
		Select(consts.QVidURL).
		From(consts.DBVideos).
		Where(squirrel.And{
			squirrel.Eq{consts.QVidChanID: c.ID},
			squirrel.Or{
				squirrel.Eq{consts.QVidFinished: 1},
				squirrel.Eq{consts.QVidIgnored: 1},
			},
		}).
		RunWith(cs.DB)

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

// DisplaySettings displays fields relevant to a channel.
func (cs *ChannelStore) DisplaySettings(c *models.Channel) {
	notifyURLs, err := cs.GetNotifyURLs(c.ID)
	if err != nil {
		logging.E("Unable to fetch notification URLs for channel %q: %v", c.Name, err)
	}

	s := c.ChanSettings
	m := c.ChanMetarrArgs

	fmt.Printf("\n%s[ Channel: %q ]%s\n", consts.ColorGreen, c.Name, consts.ColorReset)

	cURLs := c.GetURLs()
	cURLs = slices.DeleteFunc(cURLs, func(url string) bool {
		return url == consts.ManualDownloadsCol
	})

	// Channel basic info
	fmt.Printf("\n%sBasic Info:%s\n", consts.ColorCyan, consts.ColorReset)
	fmt.Printf("ID: %d\n", c.ID)
	fmt.Printf("Name: %s\n", c.Name)
	fmt.Printf("URLs: %+v\n", cURLs)

	// Channel settings
	if s == nil {
		fmt.Printf("(Settings not configured)\n")
		return
	}
	fmt.Printf("Paused: %v\n", s.Paused)

	fmt.Printf("\n%sChannel Settings:%s\n", consts.ColorCyan, consts.ColorReset)
	fmt.Printf("Video Directory: %s\n", s.VideoDir)
	fmt.Printf("JSON Directory: %s\n", s.JSONDir)
	fmt.Printf("Config File: %s\n", s.ChannelConfigFile)
	fmt.Printf("Crawl Frequency: %d minutes\n", c.GetCrawlFreq())
	fmt.Printf("Concurrency: %d\n", s.Concurrency)
	fmt.Printf("Cookie Source: %s\n", s.CookiesFromBrowser)
	fmt.Printf("Retries: %d\n", s.Retries)
	fmt.Printf("External Downloader: %s\n", s.ExternalDownloader)
	fmt.Printf("External Downloader Args: %s\n", s.ExternalDownloaderArgs)
	fmt.Printf("Filter Ops: %v\n", s.Filters)
	fmt.Printf("Filter File: %s\n", s.FilterFile)
	fmt.Printf("From Date: %q\n", parsing.HyphenateYyyyMmDd(s.FromDate))
	fmt.Printf("To Date: %q\n", parsing.HyphenateYyyyMmDd(s.ToDate))
	fmt.Printf("Max Filesize: %s\n", s.MaxFilesize)
	fmt.Printf("Move Ops: %v\n", s.MoveOps)
	fmt.Printf("Move Ops File: %s\n", s.MoveOpFile)
	fmt.Printf("Use Global Cookies: %v\n", s.UseGlobalCookies)
	fmt.Printf("Yt-dlp Output Extension: %s\n", s.YtdlpOutputExt)
	fmt.Printf("Yt-dlp Extra Video Args: %s\n", s.ExtraYTDLPVideoArgs)
	fmt.Printf("Yt-dlp Extra Metadata Args: %s\n", s.ExtraYTDLPMetaArgs)

	// Metarr settings
	fmt.Printf("\n%sMetarr Settings:%s\n", consts.ColorCyan, consts.ColorReset)
	if m == nil {
		fmt.Printf("(Metarr arguments not configured)\n")
		return
	}
	fmt.Printf("Default Output Directory: %s\n", m.OutputDir)
	fmt.Printf("URL-Specific Output Directories: %v\n", m.URLOutputDirs)
	fmt.Printf("Output Filetype: %s\n", m.OutputExt)
	fmt.Printf("Metarr Concurrency: %d\n", m.Concurrency)
	fmt.Printf("Max CPU: %.2f\n", m.MaxCPU)
	fmt.Printf("Min Free Memory: %s\n", m.MinFreeMem)
	fmt.Printf("HW Acceleration: %s\n", m.UseGPU)
	fmt.Printf("HW Acceleration Directory: %s\n", m.GPUDir)
	fmt.Printf("Video Codec: %s\n", m.TranscodeCodec)
	fmt.Printf("Audio Codec: %s\n", m.TranscodeAudioCodec)
	fmt.Printf("Transcode Quality: %s\n", m.TranscodeQuality)
	fmt.Printf("Rename Style: %s\n", m.RenameStyle)
	fmt.Printf("Meta Operations: %v\n", m.MetaOps)
	fmt.Printf("Meta Operations File: %v\n", m.MetaOpsFile)
	fmt.Printf("Filtered Meta Operations: %v\n", m.FilteredMetaOps)
	fmt.Printf("Filtered Meta Operations File: %v\n", m.FilteredMetaOpsFile)
	fmt.Printf("Filename Operations: %s\n", m.FilenameOps)
	fmt.Printf("Filename Operations File: %s\n", m.FilenameOpsFile)
	fmt.Printf("Filtered Filename Operations: %v\n", m.FilteredFilenameOps)
	fmt.Printf("Filtered Filename Operations File: %v\n", m.FilteredFilenameOpsFile)

	// Extra arguments
	fmt.Printf("Extra FFmpeg Arguments: %s\n", m.ExtraFFmpegArgs)

	// Notification URLs
	nURLs := make([]string, 0, len(notifyURLs))
	for _, n := range notifyURLs {
		newNUrl := n.NotifyURL
		if n.ChannelURL != "" {
			newNUrl = n.ChannelURL + "|" + n.NotifyURL
		}
		nURLs = append(nURLs, newNUrl)
	}
	fmt.Printf("\n%sNotify URLs:%s\n", consts.ColorCyan, consts.ColorReset)
	fmt.Printf("Notification URLs: %v\n", nURLs)

	fmt.Printf("\n%sAuthentication:%s\n", consts.ColorCyan, consts.ColorReset)

	// Auth details
	gotAuthModels := false
	for _, cu := range c.URLModels {

		if cu.Password == "" && cu.EncryptedPassword != "" {
			cu.Password, err = cs.PasswordMgr.Decrypt(cu.EncryptedPassword)
			if err != nil {
				logging.W("Failed to decrypt password (encrypted: %s) for channel %q", cu.EncryptedPassword, c.Name)
			}
		}

		if cu.Username != "" || cu.LoginURL != "" || cu.Password != "" {
			fmt.Printf("Channel URL: %s, Username: %s, Password: %s, Login URL: %s\n",
				cu.URL,
				cu.Username,
				auth.StarPassword(cu.Password),
				cu.LoginURL)

			if !gotAuthModels {
				gotAuthModels = true
			}
		}
	}
	if !gotAuthModels {
		fmt.Printf("[]\n")
	}

	fmt.Println()
}

// ******************************** Private ********************************

// applyConfigChannelSettings applies the channel settings to the model and saves to database.
func (cs *ChannelStore) applyConfigChannelSettings(c *models.Channel) (err error) {
	// Initialize settings model if nil
	if c.ChanSettings == nil {
		c.ChanSettings = &models.Settings{}
	}

	// Channel config file location
	if v, ok := getConfigValue[string](keys.ChannelConfigFile); ok {
		if _, err = validation.ValidateFile(v, false); err != nil {
			return err
		}
		c.ChanSettings.ChannelConfigFile = v
	}

	// Concurrency limit
	if v, ok := getConfigValue[int](keys.ConcurrencyLimitInput); ok {
		c.ChanSettings.Concurrency = validation.ValidateConcurrencyLimit(v)
	}

	// Cookie source
	if v, ok := getConfigValue[string](keys.CookiesFromBrowser); ok {
		c.ChanSettings.CookiesFromBrowser = v // No check for this currently! (cookies-from-browser)
	}

	// Crawl frequency
	if v, ok := getConfigValue[int](keys.CrawlFreq); ok {
		c.ChanSettings.CrawlFreq = max(v, -1)
	}

	// Download retries
	if v, ok := getConfigValue[int](keys.DLRetries); ok {
		c.ChanSettings.Retries = v
	}

	// External downloader
	if v, ok := getConfigValue[string](keys.ExternalDownloader); ok {
		c.ChanSettings.ExternalDownloader = v // No checks for this yet.
	}

	// External downloader arguments
	if v, ok := getConfigValue[string](keys.ExternalDownloaderArgs); ok {
		c.ChanSettings.ExternalDownloaderArgs = v // No checks for this yet.
	}

	// Filter ops file
	if v, ok := getConfigValue[string](keys.FilterOpsFile); ok {
		if _, err := validation.ValidateFile(v, false); err != nil {
			return err
		}
		c.ChanSettings.FilterFile = v
	}

	// From date
	if v, ok := getConfigValue[string](keys.FromDate); ok {
		if c.ChanSettings.FromDate, err = validation.ValidateToFromDate(v); err != nil {
			return err
		}
	}

	// JSON directory
	if v, ok := getConfigValue[string](keys.JSONDir); ok {
		if _, err = validation.ValidateDirectory(v, false); err != nil {
			return err
		}
		c.ChanSettings.JSONDir = v
	}

	// Max filesize to download
	if v, ok := getConfigValue[string](keys.MaxFilesize); ok {
		c.ChanSettings.MaxFilesize = v
	}

	// Move ops file
	if v, ok := getConfigValue[string](keys.MoveOpsFile); ok {
		if _, err := validation.ValidateFile(v, false); err != nil {
			return err
		}
		c.ChanSettings.MoveOpFile = v
	}

	// Pause channel
	if v, ok := getConfigValue[bool](keys.Pause); ok {
		c.ChanSettings.Paused = v
	}

	// To date
	if v, ok := getConfigValue[string](keys.ToDate); ok {
		if c.ChanSettings.ToDate, err = validation.ValidateToFromDate(v); err != nil {
			return err
		}
	}

	// Use global cookies?
	if v, ok := getConfigValue[bool](keys.UseGlobalCookies); ok {
		c.ChanSettings.UseGlobalCookies = v
	}

	// Video directory
	if v, ok := getConfigValue[string](keys.VideoDir); ok {
		if _, err = validation.ValidateDirectory(v, false); err != nil {
			return err
		}
		c.ChanSettings.VideoDir = v
	}

	// YTDLP output format
	if v, ok := getConfigValue[string](keys.YtdlpOutputExt); ok {
		if err := validation.ValidateYtdlpOutputExtension(v); err != nil {
			return err
		}
		c.ChanSettings.YtdlpOutputExt = v
	}

	// Additional video download args
	if v, ok := getConfigValue[string](keys.ExtraYTDLPVideoArgs); ok {
		c.ChanSettings.ExtraYTDLPVideoArgs = v
	}

	// Additional meta download args
	if v, ok := getConfigValue[string](keys.ExtraYTDLPMetaArgs); ok {
		c.ChanSettings.ExtraYTDLPMetaArgs = v
	}
	return nil
}

// applyConfigChannelMetarrSettings applies the Metarr settings to the model and saves to database.
func (cs *ChannelStore) applyConfigChannelMetarrSettings(c *models.Channel) (err error) {
	// Initialize MetarrArgs model if nil
	if c.ChanMetarrArgs == nil {
		c.ChanMetarrArgs = &models.MetarrArgs{}
	}

	var (
		gpuDirGot, gpuGot string
		videoCodecGot     string
	)

	// Metarr output extension
	if v, ok := getConfigValue[string](keys.MOutputExt); ok {
		if _, err := validation.ValidateOutputFiletype(c.ChanSettings.ChannelConfigFile); err != nil {
			return fmt.Errorf("metarr output filetype %q in config file %q is invalid", v, c.ChanSettings.ChannelConfigFile)
		}
		c.ChanMetarrArgs.OutputExt = v
	}

	// Filename ops
	if v, ok := getConfigValue[[]string](keys.MFilenameOps); ok {
		c.ChanMetarrArgs.FilenameOps, err = validation.ValidateFilenameOps(v)
		if err != nil {
			return err
		}
	}

	// Filename ops file
	if v, ok := getConfigValue[string](keys.MFilenameOpsFile); ok {
		c.ChanMetarrArgs.FilenameOpsFile = v
	}

	// Meta ops
	if v, ok := getConfigValue[[]string](keys.MMetaOps); ok {
		c.ChanMetarrArgs.MetaOps, err = validation.ValidateMetaOps(v)
		if err != nil {
			return err
		}
	}

	// Meta ops file
	if v, ok := getConfigValue[string](keys.MMetaOpsFile); ok {
		c.ChanMetarrArgs.MetaOpsFile = v
	}

	// Filtered meta ops
	if v, ok := getConfigValue[[]string](keys.MFilteredMetaOps); ok {
		c.ChanMetarrArgs.FilteredMetaOps, err = validation.ValidateFilteredMetaOps(v)
		if err != nil {
			return err
		}
	}

	// Meta ops file
	if v, ok := getConfigValue[string](keys.MFilteredMetaOpsFile); ok {
		c.ChanMetarrArgs.FilteredMetaOpsFile = v
	}

	// Rename style
	if v, ok := getConfigValue[string](keys.MRenameStyle); ok {
		if err := validation.ValidateRenameFlag(v); err != nil {
			return err
		}
		c.ChanMetarrArgs.RenameStyle = v
	}

	// Extra FFmpeg arguments
	if v, ok := getConfigValue[string](keys.MExtraFFmpegArgs); ok {
		c.ChanMetarrArgs.ExtraFFmpegArgs = v
	}

	// Default output directory
	if v, ok := getConfigValue[string](keys.MOutputDir); ok {
		if _, err := validation.ValidateDirectory(v, false); err != nil {
			return err
		}
		c.ChanMetarrArgs.OutputDir = v
	}

	// Per-URL output directory
	if v, ok := getConfigValue[[]string](keys.MURLOutputDirs); ok && len(v) != 0 {

		valid := make([]string, 0, len(v))

		for _, d := range v {
			split := strings.Split(d, "|")
			if len(split) == 2 && split[1] != "" {
				valid = append(valid, d)
			} else {
				logging.W("Removed invalid per-URL output directory pair %q", d)
			}
		}

		if len(valid) == 0 {
			c.ChanMetarrArgs.URLOutputDirs = nil
		} else {
			c.ChanMetarrArgs.URLOutputDirs = valid
		}
	}

	// Metarr concurrency
	if v, ok := getConfigValue[int](keys.MConcurrency); ok {
		c.ChanMetarrArgs.Concurrency = validation.ValidateConcurrencyLimit(v)
	}

	// Metarr max CPU
	if v, ok := getConfigValue[float64](keys.MMaxCPU); ok {
		c.ChanMetarrArgs.MaxCPU = v // Handled in Metarr
	}

	// Metarr minimum memory to reserve
	if v, ok := getConfigValue[string](keys.MMinFreeMem); ok {
		c.ChanMetarrArgs.MinFreeMem = v // Handled in Metarr
	}

	// Metarr GPU
	if v, ok := getConfigValue[string](keys.TranscodeGPU); ok {
		gpuGot = v
	}
	if v, ok := getConfigValue[string](keys.TranscodeGPUDir); ok {
		gpuDirGot = v
	}

	// Metarr video filter
	if v, ok := getConfigValue[string](keys.TranscodeVideoFilter); ok {
		c.ChanMetarrArgs.TranscodeVideoFilter, err = validation.ValidateTranscodeVideoFilter(v)
		if err != nil {
			return err
		}
	}

	// Metarr video codec
	if v, ok := getConfigValue[string](keys.TranscodeCodec); ok {
		videoCodecGot = v
	}

	// Metarr audio codec
	if v, ok := getConfigValue[string](keys.TranscodeAudioCodec); ok {
		if c.ChanMetarrArgs.TranscodeAudioCodec, err = validation.ValidateTranscodeAudioCodec(v); err != nil {
			return err
		}
	}

	// Metarr transcode quality
	if v, ok := getConfigValue[string](keys.MTranscodeQuality); ok {
		if c.ChanMetarrArgs.TranscodeQuality, err = validation.ValidateTranscodeQuality(v); err != nil {
			return err
		}
	}

	// Transcode GPU validation
	if gpuGot != "" || gpuDirGot != "" {
		c.ChanMetarrArgs.UseGPU, c.ChanMetarrArgs.GPUDir, err = validation.ValidateGPU(gpuGot, gpuDirGot)
		if err != nil {
			return err
		}
	}

	// Validate video codec against transcode GPU
	if c.ChanMetarrArgs.TranscodeCodec, err = validation.ValidateTranscodeCodec(videoCodecGot, c.ChanMetarrArgs.UseGPU); err != nil {
		return err
	}

	return nil
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

	logging.S("Added notification URL %q to channel with ID: %d", notifyURL, id)
	return nil
}

// channelExists returns true if the channel exists in the database.
func (cs *ChannelStore) channelExists(key, val string) bool {
	if err := validation.ValidateColumnKeyVal(key, val); err != nil {
		logging.E("Error validating column key/value pair, channel cannot exist in database: %v")
		return false
	}

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

// nullString converts an sql.NullString into a string, or "".
func nullString(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}

// getConfigValue normalizes and retrieves values from the config file.
// Supports both kebab-case and snake_case keys.
func getConfigValue[T any](key string) (T, bool) {
	var zero T

	// Try original key first
	if abstractions.IsSet(key) {
		if val, ok := convertConfigValue[T](abstractions.Get(key)); ok {
			return val, true
		}
	}

	// Try snake_case version
	snakeKey := strings.ReplaceAll(key, "-", "_")
	if snakeKey != key && abstractions.IsSet(snakeKey) {
		if val, ok := convertConfigValue[T](abstractions.Get(snakeKey)); ok {
			return val, true
		}
	}

	// Try kebab-case version
	kebabKey := strings.ReplaceAll(key, "_", "-")
	if kebabKey != key && abstractions.IsSet(kebabKey) {
		if val, ok := convertConfigValue[T](abstractions.Get(kebabKey)); ok {
			return val, true
		}
	}
	return zero, false
}

// convertConfigValue handles config entry conversions safely.
func convertConfigValue[T any](v any) (T, bool) {
	var zero T

	// Direct type match
	if val, ok := v.(T); ok {
		return val, true
	}

	switch any(zero).(type) {
	case string:
		if s, ok := v.(string); ok {
			val := any(s).(T)
			return val, true
		}
		str := fmt.Sprintf("%v", v)
		val := any(str).(T)
		return val, true

	case int:
		if i, ok := v.(int); ok {
			val := any(i).(T)
			return val, true
		}
		if i64, ok := v.(int64); ok {
			i := int(i64)
			val := any(i).(T)
			return val, true
		}
		if f, ok := v.(float64); ok {
			i := int(f)
			val := any(i).(T)
			return val, true
		}

	case float64:
		if f, ok := v.(float64); ok {
			val := any(f).(T)
			return val, true
		}
		if i, ok := v.(int); ok {
			f := float64(i)
			val := any(f).(T)
			return val, true
		}

	case bool:
		if b, ok := v.(bool); ok {
			val := any(b).(T)
			return val, true
		}

	case []string:
		if slice, ok := v.([]string); ok {
			val := any(slice).(T)
			return val, true
		}
		if slice, ok := v.([]any); ok {
			strSlice := make([]string, len(slice))
			for i, item := range slice {
				strSlice[i] = fmt.Sprintf("%v", item)
			}
			val := any(strSlice).(T)
			return val, true
		}
	}

	return zero, false
}
