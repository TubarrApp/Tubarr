package repo

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"tubarr/internal/auth"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/domain/vars"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/logging"
	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/sharedtemplates"
	"github.com/TubarrApp/gocommon/sharedvalidation"
	"github.com/spf13/viper"
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

// GetNewVideoURLs returns the slice of new video URLs from the database (used for notification dot).
func (cs *ChannelStore) GetNewVideoURLs(key, val string) ([]string, error) {
	if !consts.ValidChannelKeys[key] {
		return []string{}, fmt.Errorf("key %q is not valid for table. Valid keys: %v", key, consts.ValidChannelKeys)
	}

	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = ?",
		consts.QChanNewVideoURLs,
		consts.DBChannels,
		key,
	)

	var raw []byte
	err := cs.DB.QueryRow(query, val).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []string{}, fmt.Errorf("channel with %s %q does not exist", key, val)
		}
		return []string{}, err
	}

	if len(raw) == 0 {
		return []string{}, nil
	}

	var urls []string
	json.Unmarshal(raw, &urls)
	return urls, nil
}

// UpdateNewVideoURLs stores new video URLs into the database.
func (cs *ChannelStore) UpdateNewVideoURLs(key, val string, newVideoURLs []string) error {
	if !consts.ValidChannelKeys[key] {
		return fmt.Errorf("key %q is not valid for table. Valid keys: %v", key, consts.ValidChannelKeys)
	}

	vidURLJSON, err := json.Marshal(newVideoURLs)
	if err != nil {
		return err
	}

	query := fmt.Sprintf(
		"UPDATE %s SET %s = ? WHERE %s = ?",
		consts.DBChannels,
		consts.QChanNewVideoURLs,
		key,
	)

	if _, err := cs.DB.Exec(query, vidURLJSON, val); err != nil {
		return err
	}
	return nil
}

// GetChannelName returns the channel name from an input key and value.
func (cs *ChannelStore) GetChannelName(key, val string) (string, error) {
	if !consts.ValidChannelKeys[key] {
		return "", fmt.Errorf("key %q is not valid for table. Valid keys: %v", key, consts.ValidChannelKeys)
	}

	var name string
	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = ?",
		consts.QChanName,
		consts.DBChannels,
		key,
	)

	err := cs.DB.QueryRow(query, val).Scan(&name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("channel with %s %q does not exist", key, val)
		}
		return "", err
	}

	return name, nil
}

// GetChannelID gets the channel ID from an input key and value.
func (cs *ChannelStore) GetChannelID(key, val string) (int64, error) {
	if !consts.ValidChannelKeys[key] {
		return 0, fmt.Errorf("key %q is not valid for table. Valid keys: %v", key, consts.ValidChannelKeys)
	}

	var id int64
	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = ?",
		consts.QChanID,
		consts.DBChannels,
		key,
	)

	err := cs.DB.QueryRow(query, val).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("channel with %s %q does not exist", key, val)
		}
		return 0, err
	}

	return id, nil
}

// GetAuth retrieves authentication details for a specific URL in a channel.
func (cs *ChannelStore) GetAuth(channelID int64, url string) (username, password, loginURL string, err error) {
	var u, p, l sql.NullString // Use sql.NullString to handle NULL values

	query := fmt.Sprintf(
		"SELECT %s, %s, %s FROM %s WHERE %s = ? AND %s = ?",
		consts.QChanURLUsername,
		consts.QChanURLPassword,
		consts.QChanURLLoginURL,
		consts.DBChannelURLs,
		consts.QChanURLChannelID,
		consts.QChanURLURL,
	)

	if err = cs.DB.QueryRow(query, channelID, url).Scan(&u, &p, &l); err != nil {
		logger.Pl.I("No auth details found in database for channel ID: %d, URL: %s", channelID, url)
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

// DeleteVideosByURLs deletes a URL from the downloaded database list.
func (cs *ChannelStore) DeleteVideosByURLs(channelID int64, urls []string) error {
	if !cs.channelExistsID(channelID) {
		return fmt.Errorf("channel with ID %d does not exist", channelID)
	}

	placeholders := make([]string, len(urls))
	args := make([]any, 0, len(urls)+1)

	for i := range urls {
		placeholders[i] = "?"
		args = append(args, urls[i])
	}

	query := fmt.Sprintf(
		"SELECT %s, %s, %s FROM %s WHERE %s = ? AND %s IN (%s)",
		consts.QVidURL,
		consts.QVidVideoPath,
		consts.QVidJSONPath,
		consts.DBVideos,
		consts.QVidChanID,
		consts.QVidURL,
		strings.Join(placeholders, ","), // join only the placeholders, not values
	)

	args = append([]any{channelID}, args...) // prepend channelID

	rows, err := cs.DB.Query(query, args...)
	if err != nil {
		return fmt.Errorf("failed to retrieve videos: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Pl.E("Failed to close rows for delete video request in channel with ID %d", channelID)
		}
	}()

	// Temp variable holder for file delete iteration
	type vPaths struct {
		url   sql.NullString
		vPath sql.NullString
		jPath sql.NullString
	}

	// File deletion iterator
	for rows.Next() {
		var tmp vPaths

		if err := rows.Scan(&tmp.url, &tmp.vPath, &tmp.jPath); err != nil {
			return fmt.Errorf("failed to scan video paths for URL %q in channel %d: %w", tmp.url.String, channelID, err)
		}

		tmpURL := tmp.url.String
		if tmp.vPath.Valid {
			if tmp.vPath.String != "" {
				if err := os.Remove(tmp.vPath.String); err != nil {
					logger.Pl.W("URL %q: Could not delete video file at path %q: %v", tmpURL, tmp.vPath, err)
				}
			}
		}

		if tmp.jPath.Valid {
			if tmp.jPath.String != "" {
				if err := os.Remove(tmp.jPath.String); err != nil {
					logger.Pl.W("URL %q: Could not delete JSON file at path %q: %v", tmpURL, tmp.jPath, err)
				}
			}
		}
	}

	// Remove videos from database
	deletePlaceholders := make([]string, len(urls))
	deleteArgs := make([]any, 0, len(urls)+1)

	for i := range urls {
		deletePlaceholders[i] = "?"
		deleteArgs = append(deleteArgs, urls[i])
	}

	deleteQuery := fmt.Sprintf(
		"DELETE FROM %s WHERE %s = ? AND %s IN (%s)",
		consts.DBVideos,
		consts.QVidChanID,
		consts.QVidURL,
		strings.Join(deletePlaceholders, ","),
	)
	deleteArgs = append([]any{channelID}, deleteArgs...)

	deleteResult, err := cs.DB.Exec(deleteQuery, deleteArgs...)
	if err != nil {
		return err
	}

	deletedRows, err := deleteResult.RowsAffected()
	if deletedRows == 0 && err == nil {
		logger.Pl.I("No videos deleted for URLs: %v", urls)
		return nil
	} else if err != nil {
		logger.Pl.E("Failed to retrieve affected rows: %v", err)
	}

	// Lock for array update.
	vars.UpdateNewVideoURLMutex.Lock()
	defer vars.UpdateNewVideoURLMutex.Unlock()

	// Get channel ID as a string.
	cIDStr := strconv.FormatInt(channelID, 10)

	// Get new video URLs from database.
	newVideoURLs, err := cs.GetNewVideoURLs(consts.QChanID, cIDStr)
	if err != nil {
		logger.Pl.E("Could not fetch 'new video URLs' from database: %v", err)
	} else {
		// On no error, update the slice.
		updated := make([]string, 0, len(newVideoURLs))

		// Make deleted URLs lookup map.
		deletedURLs := make(map[string]bool, len(urls))
		for _, u := range urls {
			deletedURLs[u] = true
		}

		// Check if any 'new video urls' were deleted.
		for _, nvu := range newVideoURLs {
			if !deletedURLs[nvu] {
				updated = append(updated, nvu)
			}
		}

		// Store updated URLs back to DB.
		if err := cs.UpdateNewVideoURLs(consts.QChanID, cIDStr, updated); err != nil {
			logger.Pl.E("Could not store 'new video url' array in database: %v", err)
		} else {
			if len(updated) == 0 {
				if err := cs.UpdateChannelValue(consts.QChanID, cIDStr, consts.QChanNewVideoNotification, false); err != nil {
					logger.Pl.E("Failed to clear notification dot: %v", err)
				}
			}
		}
	}

	logger.Pl.S("Channel ID %d: Deleted videos for URLs %v", channelID, urls)
	return nil
}

// UpdateChannelFromConfig updates the channel settings from a config file if it exists.
func (cs ChannelStore) UpdateChannelFromConfig(c *models.Channel) (err error) {
	if c == nil {
		return fmt.Errorf("dev error: channel sent in nil")
	}

	cfgFile := c.ChannelConfigFile
	if cfgFile == "" {
		logger.Pl.D(2, "No config file path, nothing to apply")
		return nil
	}

	logger.Pl.I("Applying configurations to channel %q from config file %q...", c.Name, cfgFile)
	if _, _, err := sharedvalidation.ValidateFile(cfgFile, false, sharedtemplates.AllTemplatesMap); err != nil {
		return err
	}

	// Use Viper to load in flags
	v := viper.New()
	if err := file.LoadConfigFile(v, cfgFile); err != nil {
		return err
	}

	// Make []byte copy of settings before
	settingsBeforeJSON, metarrBeforeJSON := makeSettingsMetarrArgsCopy(c.ChanSettings, c.ChanMetarrArgs, c.Name)

	// Apply changes to model
	if err := cs.applyConfigChannelSettings(v, c); err != nil {
		return err
	}

	if err := cs.applyConfigChannelMetarrSettings(v, c); err != nil {
		return err
	}

	// []byte copy of settings after for comparison
	settingsAfterJSON, metarrAfterJSON := makeSettingsMetarrArgsCopy(c.ChanSettings, c.ChanMetarrArgs, c.Name)

	// Return early if unchanged
	if bytes.Equal(settingsBeforeJSON, settingsAfterJSON) &&
		bytes.Equal(metarrBeforeJSON, metarrAfterJSON) {
		logger.Pl.D(1, "No changes to channel from config file.")
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
	c.URLModels, err = cs.GetChannelURLModels(c, true)
	if err != nil {
		return fmt.Errorf("failed to reload URL models after cascade: %w", err)
	}

	logger.Pl.S("Updated channel %q from config file", c.Name)
	return nil
}

// AddURLToIgnore adds a URL into the database to ignore in subsequent crawls.
func (cs *ChannelStore) AddURLToIgnore(channelID int64, ignoreURL string) error {
	if !cs.channelExistsID(channelID) {
		return fmt.Errorf("channel with ID %d does not exist", channelID)
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s, %s, %s) VALUES (?, ?, ?)",
		consts.DBVideos,
		consts.QVidChanID,
		consts.QVidURL,
		consts.QVidFinished,
	)

	if _, err := cs.DB.Exec(query, channelID, ignoreURL, true); err != nil {
		return err
	}

	logger.Pl.S("Added URL %q to ignore list for channel with ID '%d'", ignoreURL, channelID)
	return nil
}

// GetNotifyURLs returns all notification URLs for a given channel.
func (cs *ChannelStore) GetNotifyURLs(id int64) ([]*models.Notification, error) {
	query := fmt.Sprintf(
		"SELECT %s, %s, %s, %s FROM %s WHERE %s = ?",
		consts.QNotifyChanID,
		consts.QNotifyName,
		consts.QNotifyChanURL,
		consts.QNotifyURL,
		consts.DBNotifications,
		consts.QNotifyChanID,
	)

	// Execute query to get rows
	rows, err := cs.DB.Query(query, id)
	if err != nil {
		return nil, fmt.Errorf("failed to query notification URLs: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Pl.E("Failed to close rows for notify URLs in channel with ID %d", id)
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

	urlPlaceholders := make([]string, len(urls))
	urlArgs := make([]any, 0, len(urls))
	for i := range urls {
		urlPlaceholders[i] = "?"
		urlArgs = append(urlArgs, urls[i])
	}

	namePlaceholders := make([]string, len(names))
	nameArgs := make([]any, 0, len(names))
	for i := range names {
		namePlaceholders[i] = "?"
		nameArgs = append(nameArgs, names[i])
	}

	query := fmt.Sprintf(
		"DELETE FROM %s WHERE %s = ? AND ((%s IN (%s)) OR (%s IN (%s)))",
		consts.DBNotifications,
		consts.QNotifyChanID,
		consts.QNotifyURL,
		strings.Join(urlPlaceholders, ","),
		consts.QNotifyName,
		strings.Join(namePlaceholders, ","),
	)

	args := make([]any, 0, 1+len(urlArgs)+len(nameArgs))
	args = append(args, channelID)
	args = append(args, urlArgs...)
	args = append(args, nameArgs...)

	if _, err := cs.DB.Exec(query, args...); err != nil {
		return err
	}

	switch {
	case len(urls) > 0 && len(names) == 0:
		logger.Pl.S("Deleted notify URLs %v for channel with ID '%d'.", urls, channelID)
	case len(urls) == 0 && len(names) > 0:
		logger.Pl.S("Deleted notify URLs with friendly names %v for channel with ID '%d'.", names, channelID)
	case len(urls) > 0 && len(names) > 0:
		logger.Pl.S("Deleted notify URLs: %v and notify URLs with friendly names %v for channel with ID '%d'.", urls, names, channelID)
	default:
		logger.Pl.S("No notify URLs to delete.")
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
				logger.Pl.E("Panic rollback failed for channel %d: %v", channelID, rbErr)
			}
			panic(p)
		} else if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				logger.Pl.E("Rollback failed for channel %d (original error: %v): %v", channelID, err, rbErr)
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
		logger.Pl.D(1, "No authorization details to add for channel with ID %d", chanID)
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

		query := fmt.Sprintf(
			"UPDATE %s SET %s = ?, %s = ?, %s = ? WHERE %s = ?",
			consts.DBChannelURLs,
			consts.QChanURLUsername,
			consts.QChanURLPassword,
			consts.QChanURLLoginURL,
			consts.QChanURLChannelID,
		)

		if _, err := cs.DB.Exec(query, a.Username, a.EncryptedPassword, a.LoginURL, chanID); err != nil {
			return err
		}
		logger.Pl.S("Added authentication details for URL %q in channel with ID %d", chanURL, chanID)
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

	// Validate complete models at DB write boundary
	if err := validation.ValidateSettingsModel(c.ChanSettings); err != nil {
		return 0, fmt.Errorf("cannot insert channel with invalid settings: %w", err)
	}
	if err := validation.ValidateMetarrArgsModel(c.ChanMetarrArgs); err != nil {
		return 0, fmt.Errorf("cannot insert channel with invalid metarr config: %w", err)
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

	// Convert empty slice to JSON
	newVideoURLs, err := json.Marshal([]string{})
	if err != nil {
		return 0, fmt.Errorf("failed to marshal empty new video URL slice: %w", err)
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
				logger.Pl.E("Panic rollback failed for channel %q: %v", c.Name, rbErr)
			}
			panic(p)
		} else if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				logger.Pl.E("Rollback failed for channel %q (original error: %v): %v", c.Name, err, rbErr)
			}
		}
	}()

	// Insert into the channels table
	now := time.Now()
	query := fmt.Sprintf(
		"INSERT INTO %s (%s, %s, %s, %s, %s, %s, %s, %s) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		consts.DBChannels,
		consts.QChanName,
		consts.QChanConfigFile,
		consts.QChanSettings,
		consts.QChanMetarr,
		consts.QChanNewVideoURLs,
		consts.QChanLastScan,
		consts.QChanCreatedAt,
		consts.QChanUpdatedAt,
	)

	result, err := tx.Exec(
		query,
		c.Name,
		c.ChannelConfigFile,
		settingsJSON,
		metarrJSON,
		newVideoURLs,
		now,
		now,
		now,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to insert channel: %w", err)
	}

	// Get the new channel ID
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last insert ID: %w", err)
	}

	// Insert URLs into the channel_urls table
	logger.Pl.D(1, "Inserting %d channel URLs for channel ID %d", len(c.URLModels), id)
	for i, cu := range c.URLModels {
		logger.Pl.D(1, "Inserting URL %d: %q", i+1, cu.URL)

		// Validate URL format
		if _, urlErr := url.ParseRequestURI(cu.URL); urlErr != nil {
			err = fmt.Errorf("invalid URL %q: %w", cu.URL, urlErr)
			return 0, err
		}

		if cu.EncryptedPassword == "" && cu.Password != "" {
			cu.EncryptedPassword, err = cs.PasswordMgr.Encrypt(cu.Password)
		}

		// Check if custom args exist
		var (
			customMetarrArgs []byte
			customSettings   []byte
		)
		if !models.MetarrArgsAllZero(cu.ChanURLMetarrArgs) && !models.ChildMetarrArgsMatchParent(c.ChanMetarrArgs, cu.ChanURLMetarrArgs) {
			if customMetarrArgs, err = json.Marshal(cu.ChanURLMetarrArgs); err != nil {
				return 0, fmt.Errorf("failed to marshal settings: %w", err)
			}
		}
		if !models.SettingsAllZero(cu.ChanURLSettings) && !models.ChildSettingsMatchParent(c.ChanSettings, cu.ChanURLSettings) {
			if customSettings, err = json.Marshal(cu.ChanURLSettings); err != nil {
				return 0, fmt.Errorf("failed to marshal settings: %w", err)
			}
		}

		urlInsertQuery := fmt.Sprintf(
			"INSERT INTO %s (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			consts.DBChannelURLs,
			consts.QChanURLChannelID,
			consts.QChanURLURL,
			consts.QChanURLUsername,
			consts.QChanURLPassword,
			consts.QChanURLLoginURL,
			consts.QChanURLIsManual,
			consts.QChanURLMetarr,
			consts.QChanURLSettings,
			consts.QChanURLLastScan,
			consts.QChanURLCreatedAt,
			consts.QChanURLUpdatedAt,
		)

		urlInsertResult, err := tx.Exec(
			urlInsertQuery,
			id,
			cu.URL,
			cu.Username,
			cu.EncryptedPassword,
			cu.LoginURL,
			false,
			customMetarrArgs,
			customSettings,
			now,
			now,
			now,
		)
		if err != nil {
			return 0, fmt.Errorf("failed to insert URL %q for channel ID %d: %w", cu.URL, id, err)
		}

		urlID, err := urlInsertResult.LastInsertId()
		if err != nil {
			return 0, fmt.Errorf("failed to get last insert ID for URL %q: %w", cu.URL, err)
		}
		cu.ID = urlID

		logger.Pl.D(1, "Successfully inserted URL %q with ID %d", cu.URL, urlID)
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	cURLs := c.GetURLs()
	logger.Pl.S("Successfully added channel (ID: %d)\n\nName: %s\nURLs: %v\nCrawl Frequency: %d minutes\nFilters: %v\nSettings: %+v\nMetarr Operations: %+v",
		id, c.Name, cURLs, c.ChanSettings.CrawlFreq, c.ChanSettings.Filters, c.ChanSettings, c.ChanMetarrArgs)

	return id, nil
}

// DeleteChannel deletes a channel from the database with a given key/value.
func (cs *ChannelStore) DeleteChannel(key, val string) error {
	if !consts.ValidChannelKeys[key] {
		return fmt.Errorf("key %q is not valid for table. Valid keys: %v", key, consts.ValidChannelKeys)
	}

	query := fmt.Sprintf(
		"DELETE FROM %s WHERE %s = ?",
		consts.DBChannels,
		key,
	)

	result, err := cs.DB.Exec(query, val)
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
	if !c.IsBlocked() {
		return true, nil // Not blocked, consider it "unlocked".
	}
	logger.Pl.W("Channel %q is currently blocked by %v", c.Name, c.ChanSettings.BotBlockedHostnames)
	if len(c.ChanSettings.BotBlockedHostnames) == 0 {
		return false, nil // Invalid state, keep blocked.
	}

	// Initialize timestamps map if nil.
	if c.ChanSettings.BotBlockedTimestamps == nil {
		c.ChanSettings.BotBlockedTimestamps = make(map[string]time.Time)
	}

	// Check each blocked hostname to see if any have exceeded timeout.
	stillBlockedHostnames := make([]string, 0, len(c.ChanSettings.BotBlockedHostnames))
	anyUnlocked := false

	// Iterate over blocked hostnames.
	for _, hostname := range c.ChanSettings.BotBlockedHostnames {

		// Determine timeout for this hostname.
		var timeoutMinutes float64
		var exists bool
		for k, v := range consts.BotTimeoutMap {
			if strings.Contains(hostname, k) {
				timeoutMinutes = v
				exists = true
				break
			}
		}

		// If no specific timeout found, use default from settings.
		if !exists {
			timeoutMinutes = 720.0
		}

		// Get the timestamp for this specific hostname.
		blockedTime, timestampExists := c.ChanSettings.BotBlockedTimestamps[hostname]

		// Check if timeout has expired or if timestamp is missing/zero.
		minutesSinceBlock := time.Since(blockedTime).Minutes()
		if minutesSinceBlock >= timeoutMinutes || (!timestampExists || blockedTime.IsZero()) {
			// This hostname's timeout has expired.
			logger.Pl.S("Unlocking hostname %q for channel %d (%s) after timeout", hostname, c.ID, c.Name)
			anyUnlocked = true
			delete(c.ChanSettings.BotBlockedTimestamps, hostname)
		} else {
			// Still blocked.
			stillBlockedHostnames = append(stillBlockedHostnames, hostname)

			// Print time remaining until unlock.
			blockedAt := c.ChanSettings.BotBlockedTimestamps[hostname]
			timeout := time.Duration(timeoutMinutes) * time.Minute
			unlockTime := blockedAt.Add(timeout)
			remainingDuration := time.Until(unlockTime)

			logger.Pl.W("%v remaining before channel unlocks for domain %q (Blocked on: %v)",
				remainingDuration.Round(time.Second),
				hostname,
				blockedAt.Local().Format("Mon, Jan 2 2006, 15:04:05 MST"))
		}
	}

	// Update the channel settings.
	if anyUnlocked {

		// Update in-memory copy.
		c.ChanSettings.BotBlockedHostnames = stillBlockedHostnames

		// If no hostnames remain blocked, clear the blocked state entirely.
		if len(stillBlockedHostnames) == 0 {
			c.ChanSettings.BotBlocked = false
			c.ChanSettings.BotBlockedTimestamps = make(map[string]time.Time)
		}

		// Persist changes to database.
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

	// Return true only if ALL hostnames are now unlocked.
	if len(stillBlockedHostnames) == 0 {
		logger.Pl.S("Channel %d (%s) fully unlocked - all hostnames cleared", c.ID, c.Name)
		return true, nil
	}

	// Still some blocked hostnames remaining.
	logger.Pl.W("Unlock channel %q manually for hostnames %v with:\n\ntubarr channel unblock -n %q\n",
		c.Name, stillBlockedHostnames, c.Name)
	return false, nil
}

// GetChannelModel fills the channel model from the database.
func (cs *ChannelStore) GetChannelModel(key, val string, mergeURLsWithParent bool) (*models.Channel, bool, error) {
	if !consts.ValidChannelKeys[key] {
		return nil, false, fmt.Errorf("key %q is not valid for table. Valid keys: %v", key, consts.ValidChannelKeys)
	}
	var (
		settings, metarrJSON json.RawMessage
		err                  error
	)

	query := fmt.Sprintf(
		"SELECT %s, %s, %s, %s, %s, %s, %s, %s, %s FROM %s WHERE %s = ?",
		consts.QChanID,
		consts.QChanName,
		consts.QChanConfigFile,
		consts.QChanSettings,
		consts.QChanMetarr,
		consts.QChanNewVideoNotification,
		consts.QChanLastScan,
		consts.QChanCreatedAt,
		consts.QChanUpdatedAt,
		consts.DBChannels,
		key,
	)

	var c models.Channel
	err = cs.DB.QueryRow(query, val).Scan(
		&c.ID,
		&c.Name,
		&c.ChannelConfigFile,
		&settings,
		&metarrJSON,
		&c.NewVideoNotification,
		&c.LastScan,
		&c.CreatedAt,
		&c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, true, fmt.Errorf("failed to scan channel: %w", err)
	}

	// Unmarshal settings
	if len(settings) > 0 {
		if err := json.Unmarshal(settings, &c.ChanSettings); err != nil {
			return nil, true, fmt.Errorf("failed to unmarshal channel settings: %w", err)
		}
	}

	// Unmarshal metarr settings
	if len(metarrJSON) > 0 {
		if err := json.Unmarshal(metarrJSON, &c.ChanMetarrArgs); err != nil {
			return nil, true, fmt.Errorf("failed to unmarshal metarr settings: %w", err)
		}
	}

	// Validate models at DB read boundary
	if err := validation.ValidateSettingsModel(c.ChanSettings); err != nil {
		return nil, true, fmt.Errorf("invalid settings from database: %w", err)
	}
	if err := validation.ValidateMetarrArgsModel(c.ChanMetarrArgs); err != nil {
		return nil, true, fmt.Errorf("invalid metarr config from database: %w", err)
	}

	// Get URL models
	c.URLModels, err = cs.GetChannelURLModels(&c, mergeURLsWithParent)
	if err != nil {
		return nil, true, fmt.Errorf("failed to fetch URL models for channel: %w", err)
	}

	return &c, true, nil
}

// GetAllChannels retrieves all channels in the database.
func (cs *ChannelStore) GetAllChannels(mergeURLsWithParent bool) (channels []*models.Channel, hasRows bool, err error) {
	query := fmt.Sprintf(
		"SELECT %s, %s, %s, %s, %s, %s, %s, %s, %s FROM %s ORDER BY %s",
		consts.QChanID,
		consts.QChanName,
		consts.QChanConfigFile,
		consts.QChanSettings,
		consts.QChanMetarr,
		consts.QChanLastScan,
		consts.QChanCreatedAt,
		consts.QChanUpdatedAt,
		consts.QChanNewVideoNotification,
		consts.DBChannels,
		consts.QChanName,
	)

	rows, err := cs.DB.Query(query)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	} else if err != nil {
		return nil, true, fmt.Errorf("failed to query channels: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Pl.E("Failed to close rows: %v", err)
		}
	}()

	for rows.Next() {
		var c models.Channel
		var settingsJSON, metarrJSON json.RawMessage

		if err := rows.Scan(
			&c.ID,
			&c.Name,
			&c.ChannelConfigFile,
			&settingsJSON,
			&metarrJSON,
			&c.LastScan,
			&c.CreatedAt,
			&c.UpdatedAt,
			&c.NewVideoNotification,
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

		// Validate models at DB read boundary
		if err := validation.ValidateSettingsModel(c.ChanSettings); err != nil {
			return nil, true, fmt.Errorf("invalid settings from database for channel %q: %w", c.Name, err)
		}
		if err := validation.ValidateMetarrArgsModel(c.ChanMetarrArgs); err != nil {
			return nil, true, fmt.Errorf("invalid metarr config from database for channel %q: %w", c.Name, err)
		}

		if c.URLModels, err = cs.GetChannelURLModels(&c, mergeURLsWithParent); err != nil {
			return nil, true, err
		}
		channels = append(channels, &c)
	}

	// Iterate all channels
	for _, c := range channels {
		// Check custom URL settings
		for _, cURL := range c.URLModels {
			if !models.SettingsAllZero(cURL.ChanURLSettings) && !models.ChildSettingsMatchParent(c.ChanSettings, cURL.ChanURLSettings) {
				if err := validation.ValidateSettingsModel(cURL.ChanURLSettings); err != nil {
					return nil, true, err
				}
			}
			if !models.MetarrArgsAllZero(cURL.ChanURLMetarrArgs) && !models.ChildMetarrArgsMatchParent(c.ChanMetarrArgs, cURL.ChanURLMetarrArgs) {
				if err := validation.ValidateMetarrArgsModel(cURL.ChanURLMetarrArgs); err != nil {
					return nil, true, err
				}
			}
		}
	}

	return channels, len(channels) > 0, nil
}

// UpdateChannelMetarrArgsJSON updates args for Metarr output.
func (cs *ChannelStore) UpdateChannelMetarrArgsJSON(key, val string, updateFn func(*models.MetarrArgs) error) (int64, error) {
	if !consts.ValidChannelKeys[key] {
		return 0, fmt.Errorf("key %q is not valid for table. Valid keys: %v", key, consts.ValidChannelKeys)
	}

	var metarrArgs json.RawMessage
	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = ?",
		consts.QChanMetarr,
		consts.DBChannels,
		key,
	)

	err := cs.DB.QueryRow(query, val).Scan(&metarrArgs)
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

	// Validate updated MetarrArgs at DB write boundary
	if err := validation.ValidateMetarrArgsModel(&args); err != nil {
		return 0, fmt.Errorf("updated metarr config is invalid: %w", err)
	}

	// Marshal updated settings
	updatedArgs, err := json.Marshal(args)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal updated settings: %w", err)
	}

	// Print the updated settings
	logger.Pl.S("Updated MetarrArgs: %s", string(updatedArgs))

	// Update the database with the new settings
	updateQuery := fmt.Sprintf(
		"UPDATE %s SET %s = ? WHERE %s = ?",
		consts.DBChannels,
		consts.QChanMetarr,
		key,
	)

	rtn, err := cs.DB.Exec(updateQuery, updatedArgs, val)
	if err != nil {
		return 0, fmt.Errorf("failed to update channel settings in database: %w", err)
	}

	return rtn.RowsAffected()
}

// UpdateChannelSettingsJSON updates specific settings in the channel's settings JSON.
func (cs *ChannelStore) UpdateChannelSettingsJSON(key, val string, updateFn func(*models.Settings) error) (int64, error) {
	if !consts.ValidChannelKeys[key] {
		return 0, fmt.Errorf("key %q is not valid for table. Valid keys: %v", key, consts.ValidChannelKeys)
	}

	var settingsJSON json.RawMessage
	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = ?",
		consts.QChanSettings,
		consts.DBChannels,
		key,
	)

	err := cs.DB.QueryRow(query, val).Scan(&settingsJSON)
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

	// Validate updated Settings at DB write boundary
	if err := validation.ValidateSettingsModel(&settings); err != nil {
		return 0, fmt.Errorf("updated settings are invalid: %w", err)
	}

	// Marshal updated settings
	updatedSettings, err := json.Marshal(settings)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal updated settings: %w", err)
	}

	// Print the updated settings
	logger.Pl.S("Updated ChannelSettings: %s", string(updatedSettings))

	// Update the database with the new settings
	updateQuery := fmt.Sprintf(
		"UPDATE %s SET %s = ? WHERE %s = ?",
		consts.DBChannels,
		consts.QChanSettings,
		key,
	)

	rtn, err := cs.DB.Exec(updateQuery, updatedSettings, val)
	if err != nil {
		return 0, fmt.Errorf("failed to update channel settings in database: %w", err)
	}

	return rtn.RowsAffected()
}

// UpdateChannelValue updates a single element in the database.
func (cs *ChannelStore) UpdateChannelValue(key, val, col string, newVal any) error {
	if !consts.ValidChannelKeys[key] {
		return fmt.Errorf("key %q is not valid for table. Valid keys: %v", key, consts.ValidChannelKeys)
	}

	if !cs.channelExists(key, val) {
		return fmt.Errorf("channel with key %q and value %q does not exist", key, val)
	}

	query := fmt.Sprintf(
		"UPDATE %s SET %s = ? WHERE %s = ?",
		consts.DBChannels,
		col,
		key,
	)

	// Print SQL query
	if logging.Level > 1 {
		logger.Pl.P("Executing SQL: %s with args: %v\n", query, []any{newVal, val})
	}

	// Execute query
	res, err := cs.DB.Exec(query, newVal, val)
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

	logger.Pl.S("Successfully updated channel [%s=%s]: %q column was set to value %+v", key, val, col, newVal)
	return nil
}

// UpdateLastScan updates the DB entry for when the channel was last scanned.
func (cs *ChannelStore) UpdateLastScan(channelID int64) error {
	now := time.Now()
	query := fmt.Sprintf(
		"UPDATE %s SET %s = ?, %s = ? WHERE %s = ?",
		consts.DBChannels,
		consts.QChanLastScan,
		consts.QChanUpdatedAt,
		consts.QChanID,
	)

	result, err := cs.DB.Exec(query, now, now, channelID)
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

	logger.Pl.D(1, "Updated last scan time for channel ID %d", channelID)
	return nil
}

// GetDownloadedOrIgnoredVideos loads already downloaded or ignored videos from the database.
func (cs *ChannelStore) GetDownloadedOrIgnoredVideos(c *models.Channel) (videos []*models.Video, hasRows bool, err error) {
	if c.ID == 0 {
		return nil, false, errors.New("model entered has no ID")
	}

	query := fmt.Sprintf(
		"SELECT %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s "+
			"FROM %s WHERE %s = ? AND (%s = 1 OR %s = 1)",
		consts.QVidID,
		consts.QVidChanID,
		consts.QVidChanURLID,
		consts.QVidThumbnailURL,
		consts.QVidFinished,
		consts.QVidIgnored,
		consts.QVidURL,
		consts.QVidTitle,
		consts.QVidDescription,
		consts.QVidUploadDate,
		consts.QVidMetadata,
		consts.QVidCreatedAt,
		consts.QVidUpdatedAt,
		consts.DBVideos,
		consts.QVidChanID,
		consts.QVidFinished,
		consts.QVidIgnored,
	)

	rows, err := cs.DB.Query(query, c.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	} else if err != nil {
		return nil, true, fmt.Errorf("failed to query videos: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Pl.E("Failed to close rows: %v", err)
		}
	}()

	for rows.Next() {
		var (
			v            models.Video
			url          sql.NullString
			title        sql.NullString
			thumbnailURL sql.NullString
			description  sql.NullString
			uploadDate   sql.NullTime
			metadataStr  sql.NullString
			channelURLID sql.NullInt64
		)

		if err := rows.Scan(
			&v.ID,
			&v.ChannelID,
			&channelURLID,
			&thumbnailURL,
			&v.Finished,
			&v.Ignored,
			&url,
			&title,
			&description,
			&uploadDate,
			&metadataStr,
			&v.CreatedAt,
			&v.UpdatedAt,
		); err != nil {
			return nil, true, fmt.Errorf("failed to scan channel: %w", err)
		}

		// Handle nullable fields
		if channelURLID.Valid {
			v.ChannelURLID = channelURLID.Int64
		}
		if url.Valid {
			v.URL = url.String
		}
		if title.Valid {
			v.Title = title.String
		}
		if thumbnailURL.Valid {
			v.ThumbnailURL = thumbnailURL.String
		}
		if description.Valid {
			v.Description = description.String
		}
		if uploadDate.Valid {
			v.UploadDate = uploadDate.Time
		}
		if metadataStr.Valid && metadataStr.String != "" {
			if err := json.Unmarshal([]byte(metadataStr.String), &v.MetadataMap); err != nil {
				logger.Pl.W("Failed to unmarshal metadata for video %d: %v", v.ID, err)
			}
		}

		videos = append(videos, &v)
	}

	logger.Pl.I("Found %d previously downloaded videos for channel %q", len(videos), c.Name)
	return videos, true, nil
}

// GetDownloadedOrIgnoredVideoURLs loads already downloaded or ignored URLs from the database.
func (cs *ChannelStore) GetDownloadedOrIgnoredVideoURLs(c *models.Channel) (urls []string, err error) {
	if c.ID == 0 {
		return nil, errors.New("model entered has no ID")
	}

	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = ? AND (%s = 1 OR %s = 1)",
		consts.QVidURL,
		consts.DBVideos,
		consts.QVidChanID,
		consts.QVidFinished,
		consts.QVidIgnored,
	)

	rows, err := cs.DB.Query(query, c.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Pl.E("Failed to close rows for channel %v: %v", c.Name, err)
		}
	}()

	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		urls = append(urls, url)
		logger.Pl.D(2, "Found downloaded video: %s", url)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	logger.Pl.I("Found %d previously downloaded videos for channel %q", len(urls), c.Name)
	return urls, nil
}

// DisplaySettings displays fields relevant to a channel.
func (cs *ChannelStore) DisplaySettings(c *models.Channel) {
	notifyURLs, err := cs.GetNotifyURLs(c.ID)
	if err != nil {
		logger.Pl.E("Unable to fetch notification URLs for channel %q: %v", c.Name, err)
	}

	s := c.ChanSettings
	m := c.ChanMetarrArgs

	fmt.Printf("\n%s[ Channel: %q ]%s\n", sharedconsts.ColorGreen, c.Name, sharedconsts.ColorReset)

	cURLs := c.GetURLs()
	cURLs = slices.DeleteFunc(cURLs, func(url string) bool {
		return url == consts.ManualDownloadsCol
	})

	// Channel basic info
	fmt.Printf("\n%sBasic Info:%s\n", sharedconsts.ColorCyan, sharedconsts.ColorReset)
	fmt.Printf("ID: %d\n", c.ID)
	fmt.Printf("Name: %s\n", c.Name)
	fmt.Printf("Config File: %s\n", c.ChannelConfigFile)
	fmt.Printf("URLs: %+v\n", cURLs)

	// Channel settings
	if s == nil {
		logger.Pl.P("(Settings not configured)\n")
		return
	}
	fmt.Printf("Paused: %v\n", s.Paused)

	fmt.Printf("\n%sChannel Settings:%s\n", sharedconsts.ColorCyan, sharedconsts.ColorReset)
	displaySettingsStruct(c.ChanSettings)

	// Metarr settings
	fmt.Printf("\n%sMetarr Settings:%s\n", sharedconsts.ColorCyan, sharedconsts.ColorReset)
	if m == nil {
		fmt.Printf("(Metarr arguments not configured)\n")
		return
	}
	displayMetarrArgsStruct(c.ChanMetarrArgs)

	// For dev debugging.
	if logging.Level > 0 {
		fmt.Printf("\n%sNew Video Notifications:%s\n", sharedconsts.ColorCyan, sharedconsts.ColorReset)
		fmt.Printf("Notification Displayed? %v\n", c.NewVideoNotification)
		newVideoURLs, err := cs.GetNewVideoURLs(consts.QChanName, c.Name)
		if err != nil {
			logger.Pl.E("Failed to get new video URLs: %v", err)
		} else {
			fmt.Printf("Unseen URLs: %v\n", newVideoURLs)
		}
	}

	// Notification URLs
	nURLs := make([]string, 0, len(notifyURLs))
	for _, n := range notifyURLs {
		newNUrl := n.NotifyURL
		if n.ChannelURL != "" {
			newNUrl = n.ChannelURL + "|" + n.NotifyURL
		}
		nURLs = append(nURLs, newNUrl)
	}
	fmt.Printf("\n%sNotify URLs:%s\n", sharedconsts.ColorCyan, sharedconsts.ColorReset)
	fmt.Printf("Notification URLs: %v\n", nURLs)

	fmt.Printf("\n%sAuthentication:%s\n", sharedconsts.ColorCyan, sharedconsts.ColorReset)

	// Auth details
	gotAuthModels := false
	for _, cu := range c.URLModels {

		if cu.Password == "" && cu.EncryptedPassword != "" {
			cu.Password, err = cs.PasswordMgr.Decrypt(cu.EncryptedPassword)
			if err != nil {
				logger.Pl.W("Failed to decrypt password (encrypted: %s) for channel %q", cu.EncryptedPassword, c.Name)
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

	fmt.Printf("\n%s[ URL Models in Channel: %q ]%s\n", sharedconsts.ColorYellow, c.Name, sharedconsts.ColorReset)
	for _, u := range c.URLModels {
		if u == nil {
			continue
		}
		fmt.Printf("%sURL %q%s\n", sharedconsts.ColorCyan, u.URL, sharedconsts.ColorReset)

		fmt.Printf("\n%sSettings:%s\n", sharedconsts.ColorCyan, sharedconsts.ColorReset)
		displaySettingsStruct(u.ChanURLSettings)

		fmt.Printf("\n%sMetarr Args:%s\n", sharedconsts.ColorCyan, sharedconsts.ColorReset)
		displayMetarrArgsStruct(u.ChanURLMetarrArgs)
	}

	fmt.Println()
}

// ******************************** Private ***************************************************************************************

// displaySettingsStruct prints the settings structure.
func displaySettingsStruct(s *models.Settings) {
	fmt.Printf("Video Directory: %s\n", s.VideoDir)
	fmt.Printf("JSON Directory: %s\n", s.JSONDir)
	fmt.Printf("Crawl Frequency: %d minutes\n", s.CrawlFreq)
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
	fmt.Printf("Move Ops: %v\n", s.MetaFilterMoveOps)
	fmt.Printf("Move Ops File: %s\n", s.MetaFilterMoveOpFile)
	fmt.Printf("Use Global Cookies: %v\n", s.UseGlobalCookies)
	fmt.Printf("Yt-dlp Output Extension: %s\n", s.YtdlpOutputExt)
	fmt.Printf("Yt-dlp Extra Video Args: %s\n", s.ExtraYTDLPVideoArgs)
	fmt.Printf("Yt-dlp Extra Metadata Args: %s\n", s.ExtraYTDLPMetaArgs)
}

// displayMetarrArgsStruct prints the Metarr args structure.
func displayMetarrArgsStruct(m *models.MetarrArgs) {
	fmt.Printf("Default Output Directory: %s\n", m.OutputDir)
	fmt.Printf("URL-Specific Output Directories: %v\n", m.URLOutputDirs)
	fmt.Printf("Output Filetype: %s\n", m.OutputExt)
	fmt.Printf("Metarr Concurrency: %d\n", m.Concurrency)
	fmt.Printf("Max CPU: %.2f\n", m.MaxCPU)
	fmt.Printf("Min Free Memory: %s\n", m.MinFreeMem)
	fmt.Printf("HW Acceleration: %s\n", m.TranscodeGPU)
	fmt.Printf("HW Acceleration Directory: %s\n", m.TranscodeGPUDirectory)
	fmt.Printf("Video Codec: %v\n", m.TranscodeVideoCodecs)
	fmt.Printf("Audio Codec: %v\n", m.TranscodeAudioCodecs)
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
	fmt.Printf("Extra FFmpeg Arguments: %s\n", m.ExtraFFmpegArgs)
}

// applyConfigChannelSettings applies the channel settings to the model.
func (cs *ChannelStore) applyConfigChannelSettings(vip *viper.Viper, c *models.Channel) (err error) {
	// Initialize settings model if nil
	if c.ChanSettings == nil {
		c.ChanSettings = &models.Settings{}
	}

	// Channel config file location
	if v, ok := parsing.GetConfigValue[string](vip, keys.ChannelConfigFile); ok {
		if _, _, err = sharedvalidation.ValidateFile(v, false, sharedtemplates.AllTemplatesMap); err != nil {
			return err
		}
		c.ChannelConfigFile = v
	}

	// Concurrency limit
	if v, ok := parsing.GetConfigValue[int](vip, keys.ChanOrURLConcurrencyLimit); ok {
		c.ChanSettings.Concurrency = sharedvalidation.ValidateConcurrencyLimit(v)
	}

	// Cookie source
	if v, ok := parsing.GetConfigValue[string](vip, keys.CookiesFromBrowser); ok {
		c.ChanSettings.CookiesFromBrowser = v // No check for this currently! (cookies-from-browser)
	}

	// Crawl frequency
	if v, ok := parsing.GetConfigValue[int](vip, keys.CrawlFreq); ok {
		c.ChanSettings.CrawlFreq = max(v, 0)
	}

	// Download retries
	if v, ok := parsing.GetConfigValue[int](vip, keys.DLRetries); ok {
		c.ChanSettings.Retries = v
	}

	// External downloader
	if v, ok := parsing.GetConfigValue[string](vip, keys.ExternalDownloader); ok {
		c.ChanSettings.ExternalDownloader = v // No checks for this yet.
	}

	// External downloader arguments
	if v, ok := parsing.GetConfigValue[string](vip, keys.ExternalDownloaderArgs); ok {
		c.ChanSettings.ExternalDownloaderArgs = v // No checks for this yet.
	}

	// Filter ops file
	if v, ok := parsing.GetConfigValue[string](vip, keys.FilterOpsFile); ok {
		if _, _, err := sharedvalidation.ValidateFile(v, false, sharedtemplates.AllTemplatesMap); err != nil {
			return err
		}
		c.ChanSettings.FilterFile = v
	}

	// From date
	if v, ok := parsing.GetConfigValue[string](vip, keys.FromDate); ok {
		if c.ChanSettings.FromDate, err = validation.ValidateToFromDate(v); err != nil {
			return err
		}
	}

	// JSON directory
	if v, ok := parsing.GetConfigValue[string](vip, keys.JSONDir); ok {
		if _, _, err = sharedvalidation.ValidateDirectory(v, false, sharedtemplates.AllTemplatesMap); err != nil {
			return err
		}
		c.ChanSettings.JSONDir = v
	}

	// Max filesize to download
	if v, ok := parsing.GetConfigValue[string](vip, keys.MaxFilesize); ok {
		c.ChanSettings.MaxFilesize = v
	}

	// Move ops file
	if v, ok := parsing.GetConfigValue[string](vip, keys.MoveOpsFile); ok {
		if _, _, err := sharedvalidation.ValidateFile(v, false, sharedtemplates.AllTemplatesMap); err != nil {
			return err
		}
		c.ChanSettings.MetaFilterMoveOpFile = v
	}

	// Pause channel
	if v, ok := parsing.GetConfigValue[bool](vip, keys.Pause); ok {
		c.ChanSettings.Paused = v
	}

	// To date
	if v, ok := parsing.GetConfigValue[string](vip, keys.ToDate); ok {
		if c.ChanSettings.ToDate, err = validation.ValidateToFromDate(v); err != nil {
			return err
		}
	}

	// Use global cookies?
	if v, ok := parsing.GetConfigValue[bool](vip, keys.UseGlobalCookies); ok {
		c.ChanSettings.UseGlobalCookies = v
	}

	// Video directory
	if v, ok := parsing.GetConfigValue[string](vip, keys.VideoDir); ok {
		if _, _, err = sharedvalidation.ValidateDirectory(v, false, sharedtemplates.AllTemplatesMap); err != nil {
			return err
		}
		c.ChanSettings.VideoDir = v
	}

	// YTDLP output format
	if v, ok := parsing.GetConfigValue[string](vip, keys.YtdlpOutputExt); ok {
		if err := validation.ValidateYtdlpOutputExtension(v); err != nil {
			return err
		}
		c.ChanSettings.YtdlpOutputExt = v
	}

	// Additional video download args
	if v, ok := parsing.GetConfigValue[string](vip, keys.ExtraYTDLPVideoArgs); ok {
		c.ChanSettings.ExtraYTDLPVideoArgs = v
	}

	// Additional meta download args
	if v, ok := parsing.GetConfigValue[string](vip, keys.ExtraYTDLPMetaArgs); ok {
		c.ChanSettings.ExtraYTDLPMetaArgs = v
	}

	// Validate complete Settings model at config boundary
	if err := validation.ValidateSettingsModel(c.ChanSettings); err != nil {
		return fmt.Errorf("invalid settings in config file %q: %w", c.ChannelConfigFile, err)
	}

	return nil
}

// applyConfigChannelMetarrSettings applies the Metarr settings to the model.
func (cs *ChannelStore) applyConfigChannelMetarrSettings(vip *viper.Viper, c *models.Channel) (err error) {
	// Initialize MetarrArgs model if nil
	if c.ChanMetarrArgs == nil {
		c.ChanMetarrArgs = &models.MetarrArgs{}
	}

	var (
		gpuDirGot, gpuGot string
	)

	// Metarr output extension
	if v, ok := parsing.GetConfigValue[string](vip, keys.MOutputExt); ok {
		if _, err := sharedvalidation.ValidateFFmpegOutputExt(v); err != nil {
			return fmt.Errorf("metarr output filetype %q in config file %q is invalid", v, c.ChannelConfigFile)
		}
		c.ChanMetarrArgs.OutputExt = v
	}

	// Filename ops
	if v, ok := parsing.GetConfigValue[[]string](vip, keys.MFilenameOps); ok {
		if c.ChanMetarrArgs.FilenameOps, err = parsing.ParseFilenameOps(v); err != nil {
			return fmt.Errorf("failed to parse filename ops: %w", err)
		}
	}

	// Filename ops file
	if v, ok := parsing.GetConfigValue[string](vip, keys.MFilenameOpsFile); ok {
		c.ChanMetarrArgs.FilenameOpsFile = v
	}

	// Meta ops
	if v, ok := parsing.GetConfigValue[[]string](vip, keys.MMetaOps); ok {
		if c.ChanMetarrArgs.MetaOps, err = parsing.ParseMetaOps(v); err != nil {
			return fmt.Errorf("failed to parse meta ops: %w", err)
		}
	}

	// Meta ops file
	if v, ok := parsing.GetConfigValue[string](vip, keys.MMetaOpsFile); ok {
		c.ChanMetarrArgs.MetaOpsFile = v
	}

	// Filtered meta ops
	if v, ok := parsing.GetConfigValue[[]string](vip, keys.MFilteredMetaOps); ok {
		if c.ChanMetarrArgs.FilteredMetaOps, err = parsing.ParseFilteredMetaOps(v); err != nil {
			return fmt.Errorf("failed to parse filtered meta ops: %w", err)
		}
	}

	// Meta ops file
	if v, ok := parsing.GetConfigValue[string](vip, keys.MFilteredMetaOpsFile); ok {
		c.ChanMetarrArgs.FilteredMetaOpsFile = v
	}

	// Rename style
	if v, ok := parsing.GetConfigValue[string](vip, keys.MRenameStyle); ok {
		if err := validation.ValidateRenameFlag(v); err != nil {
			return err
		}
		c.ChanMetarrArgs.RenameStyle = v
	}

	// Extra FFmpeg arguments
	if v, ok := parsing.GetConfigValue[string](vip, keys.MExtraFFmpegArgs); ok {
		c.ChanMetarrArgs.ExtraFFmpegArgs = v
	}

	// Default output directory
	if v, ok := parsing.GetConfigValue[string](vip, keys.MOutputDir); ok {
		if _, _, err := sharedvalidation.ValidateDirectory(v, false, sharedtemplates.AllTemplatesMap); err != nil {
			return err
		}
		c.ChanMetarrArgs.OutputDir = v
	}

	// Per-URL output directory
	if v, ok := parsing.GetConfigValue[[]string](vip, keys.MURLOutputDirs); ok && len(v) != 0 {

		valid := make([]string, 0, len(v))

		for _, d := range v {
			split := strings.Split(d, "|")
			if len(split) == 2 && split[1] != "" {
				valid = append(valid, d)
			} else {
				logger.Pl.W("Removed invalid per-URL output directory pair %q", d)
			}
		}

		if len(valid) == 0 {
			c.ChanMetarrArgs.URLOutputDirs = nil
		} else {
			c.ChanMetarrArgs.URLOutputDirs = valid
		}
	}

	// Metarr concurrency
	if v, ok := parsing.GetConfigValue[int](vip, keys.MConcurrency); ok {
		c.ChanMetarrArgs.Concurrency = sharedvalidation.ValidateConcurrencyLimit(v)
	}

	// Metarr max CPU
	if v, ok := parsing.GetConfigValue[float64](vip, keys.MMaxCPU); ok {
		c.ChanMetarrArgs.MaxCPU = v // Handled in Metarr
	}

	// Metarr minimum memory to reserve
	if v, ok := parsing.GetConfigValue[string](vip, keys.MMinFreeMem); ok {
		c.ChanMetarrArgs.MinFreeMem = v // Handled in Metarr
	}

	// Metarr GPU
	if v, ok := parsing.GetConfigValue[string](vip, keys.TranscodeGPU); ok {
		gpuGot = v
	}
	if v, ok := parsing.GetConfigValue[string](vip, keys.TranscodeGPUDir); ok {
		gpuDirGot = v
	}

	// Metarr video filter
	if v, ok := parsing.GetConfigValue[string](vip, keys.TranscodeVideoFilter); ok {
		if c.ChanMetarrArgs.TranscodeVideoFilter, err = validation.ValidateTranscodeVideoFilter(v); err != nil {
			return err
		}
	}

	// Metarr audio codec
	if v, ok := parsing.GetConfigValue[[]string](vip, keys.TranscodeAudioCodec); ok {
		if c.ChanMetarrArgs.TranscodeAudioCodecs, err = validation.ValidateAudioTranscodeCodecSlice(v); err != nil {
			return err
		}
	}

	// Metarr transcode quality
	if v, ok := parsing.GetConfigValue[string](vip, keys.MTranscodeQuality); ok {
		if c.ChanMetarrArgs.TranscodeQuality, err = sharedvalidation.ValidateTranscodeQuality(v); err != nil {
			return err
		}
	}

	// Transcode GPU validation
	if gpuGot != "" || gpuDirGot != "" {
		if c.ChanMetarrArgs.TranscodeGPU, c.ChanMetarrArgs.TranscodeGPUDirectory, err = validation.ValidateGPU(gpuGot, gpuDirGot); err != nil {
			return err
		}
	}

	// Validate video codec against transcode GPU
	// Metarr video codec
	if v, ok := parsing.GetConfigValue[[]string](vip, keys.TranscodeCodec); ok {
		if c.ChanMetarrArgs.TranscodeVideoCodecs, err = validation.ValidateVideoTranscodeCodecSlice(v, c.ChanMetarrArgs.TranscodeGPU); err != nil {
			return err
		}
	}

	// Validate complete MetarrArgs model at config boundary
	if err := validation.ValidateMetarrArgsModel(c.ChanMetarrArgs); err != nil {
		return fmt.Errorf("invalid metarr config in config file %q: %w", c.ChannelConfigFile, err)
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
	query := fmt.Sprintf(
		"INSERT INTO %s (%s, %s, %s, %s, %s, %s) VALUES (?, ?, ?, ?, ?, ?) %s",
		consts.DBNotifications,
		consts.QNotifyChanID,
		consts.QNotifyName,
		consts.QNotifyChanURL,
		consts.QNotifyURL,
		consts.QNotifyCreatedAt,
		consts.QNotifyUpdatedAt,
		querySuffix, // appended exactly like Squirrel's .Suffix()
	)

	_, err := tx.Exec(
		query,
		id,
		notifyName,
		chanURL,
		notifyURL,
		now,
		now,
	)
	if err != nil {
		return err
	}

	logger.Pl.S("Added notification URL %q to channel with ID: %d", notifyURL, id)
	return nil
}

// channelExists returns true if the channel exists in the database.
func (cs *ChannelStore) channelExists(key, val string) bool {
	if !consts.ValidChannelKeys[key] {
		logger.Pl.E("key %q is not valid for table. Valid keys: %v", key, consts.ValidChannelKeys)
		return false
	}

	var count int
	query := fmt.Sprintf(
		"SELECT COUNT(1) FROM %s WHERE %s = ?",
		consts.DBChannels,
		key,
	)

	if err := cs.DB.QueryRow(query, val).Scan(&count); err != nil {
		logger.Pl.E("failed to check if channel exists with key=%s val=%s: %v", key, val, err)
		return false
	}
	return count > 0
}

// channelExistsID returns true if the channel ID exists in the database.
func (cs ChannelStore) channelExistsID(id int64) bool {
	var exists bool
	query := fmt.Sprintf(
		"SELECT 1 FROM %s WHERE %s = ?",
		consts.DBChannels,
		consts.QChanID,
	)

	if err := cs.DB.QueryRow(query, id).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return false
	} else if err != nil {
		logger.Pl.E("Failed to check if channel with ID %d exists", id)
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
