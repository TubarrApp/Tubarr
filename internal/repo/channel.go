package repo

import (
	"bytes"
	"cmp"
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
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/domain/vars"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/logging"
	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/sharedtemplates"
	"github.com/TubarrApp/gocommon/sharedvalidation"
)

// ChannelStore holds the database, and the transaction its queries run in if it's bound to one.
type ChannelStore struct {
	db          *sql.DB
	q           querier
	tx          *sql.Tx
	PasswordMgr *auth.PasswordManager
	touched     map[int64]struct{} // Channels the transaction changed, checked before it commits.
}

// GetChannelStore returns a channel store instance with injected database.
func GetChannelStore(db *sql.DB) (*ChannelStore, error) {
	pm, err := auth.NewPasswordManager(paths.HomeTubarrDir)
	if err != nil {
		return nil, err
	}

	return &ChannelStore{
		db:          db,
		q:           db,
		PasswordMgr: pm,
	}, nil
}

// GetDB returns the database.
func (cs *ChannelStore) GetDB() *sql.DB {
	return cs.db
}

// WithTx runs fn with a store whose operations all run in one transaction, committed if fn returns nil.
//
// If cs is already bound to a transaction, fn runs in that one.
func (cs *ChannelStore) WithTx(fn func(cs contracts.ChannelStore) error) error {
	return cs.inTx(func(tcs *ChannelStore) error { return fn(tcs) })
}

// inTx runs fn with a store bound to a transaction, reusing cs's own if it has one.
func (cs *ChannelStore) inTx(fn func(tcs *ChannelStore) error) error {
	if cs.tx != nil {
		return fn(cs)
	}
	return inTx(cs.db, func(tx *sql.Tx) error {
		tcs := &ChannelStore{db: cs.db, q: tx, tx: tx, PasswordMgr: cs.PasswordMgr, touched: map[int64]struct{}{}}
		if err := fn(tcs); err != nil {
			return err
		}
		return tcs.checkTouchedChannels()
	})
}

// touch records that the transaction changed a channel, one of its URLs, or the profile of either.
func (cs *ChannelStore) touch(channelID int64) {
	if cs.touched != nil {
		cs.touched[channelID] = struct{}{}
	}
}

// checkTouchedChannels checks each channel the transaction changed once all its changes are written, so settings saved
// together are judged together. It needs a video directory, and Metarr args that work together under each of its URLs.
func (cs *ChannelStore) checkTouchedChannels() error {
	for id := range cs.touched {
		c, found, err := cs.GetChannelModel(consts.QChanID, strconv.FormatInt(id, 10), true)
		if err != nil {
			return fmt.Errorf("failed to read settings of channel %d: %w", id, err)
		}
		if !found {
			continue
		}
		if c.ChanSettings.VideoDir == "" {
			return fmt.Errorf("%w: channel %q needs a video directory where downloads will be stored, its own or its profile's", contracts.ErrInvalidChannel, c.Name)
		}
		if err := validation.ValidateMetarrArgsCombination(c.ChanMetarrArgs); err != nil {
			return fmt.Errorf("%w: Metarr settings: %w", contracts.ErrInvalidChannel, err)
		}
		for _, cu := range c.URLModels {
			if err := validation.ValidateMetarrArgsCombination(cu.ChanURLMetarrArgs); err != nil {
				return fmt.Errorf("%w: Metarr settings for URL %q: %w", contracts.ErrInvalidChannel, cu.URL, err)
			}
		}
	}
	return nil
}

// GetNewVideoURLs returns the slice of new video URLs from the database (used for notification dot).
func (cs *ChannelStore) GetNewVideoURLs(key, val string) ([]string, error) {
	if err := validKey(key, consts.ValidChannelKeys); err != nil {
		return []string{}, err
	}

	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = ?",
		consts.QChanNewVideoURLs,
		consts.DBChannels,
		key,
	)

	var raw []byte
	err := cs.q.QueryRow(query, val).Scan(&raw)
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
	if err := json.Unmarshal(raw, &urls); err != nil {
		logger.Pl.E("Could not unmarshal JSON: %v", err)
	}

	return urls, nil
}

// UpdateNewVideoURLs stores new video URLs into the database.
func (cs *ChannelStore) UpdateNewVideoURLs(key, val string, newVideoURLs []string) error {
	if err := validKey(key, consts.ValidChannelKeys); err != nil {
		return err
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

	if _, err := cs.q.Exec(query, vidURLJSON, val); err != nil {
		return err
	}
	return nil
}

// SetNotificationDot sets the channel's new video notification state in the database.
func (cs *ChannelStore) SetNotificationDot(channelID int64, state bool) error {
	return cs.UpdateChannelValue(consts.QChanID, strconv.FormatInt(channelID, 10), consts.QChanNewVideoNotification, state)
}

// AddNewVideoURLs adds unseen video URLs (deduplicated) and sets the notification dot.
func (cs *ChannelStore) AddNewVideoURLs(channelID int64, urls []string) error {
	if len(urls) == 0 {
		return nil
	}
	vars.UpdateNewVideoURLMutex.Lock()
	defer vars.UpdateNewVideoURLMutex.Unlock()

	cIDStr := strconv.FormatInt(channelID, 10)
	newVideoURLs, err := cs.GetNewVideoURLs(consts.QChanID, cIDStr)
	if err != nil {
		return fmt.Errorf("could not fetch new video URLs: %w", err)
	}
	for _, u := range urls {
		if !slices.Contains(newVideoURLs, u) {
			newVideoURLs = append(newVideoURLs, u)
		}
	}
	if err := cs.UpdateNewVideoURLs(consts.QChanID, cIDStr, newVideoURLs); err != nil {
		return fmt.Errorf("could not store new video URLs: %w", err)
	}
	return cs.SetNotificationDot(channelID, true)
}

// RemoveNewVideoURLs removes video URLs and clears the notification dot if none remain.
func (cs *ChannelStore) RemoveNewVideoURLs(channelID int64, urls []string) error {
	if len(urls) == 0 {
		return nil
	}
	vars.UpdateNewVideoURLMutex.Lock()
	defer vars.UpdateNewVideoURLMutex.Unlock()

	cIDStr := strconv.FormatInt(channelID, 10)
	newVideoURLs, err := cs.GetNewVideoURLs(consts.QChanID, cIDStr)
	if err != nil {
		return fmt.Errorf("could not fetch new video URLs: %w", err)
	}
	updated := slices.DeleteFunc(newVideoURLs, func(u string) bool {
		return slices.Contains(urls, u)
	})
	if err := cs.UpdateNewVideoURLs(consts.QChanID, cIDStr, updated); err != nil {
		return fmt.Errorf("could not store new video URLs: %w", err)
	}
	if len(updated) == 0 {
		return cs.SetNotificationDot(channelID, false)
	}
	return nil
}

// GetChannelName returns the channel name from an input key and value.
func (cs *ChannelStore) GetChannelName(key, val string) (string, error) {
	if err := validKey(key, consts.ValidChannelKeys); err != nil {
		return "", err
	}

	var name string
	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = ?",
		consts.QChanName,
		consts.DBChannels,
		key,
	)

	err := cs.q.QueryRow(query, val).Scan(&name)
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
	if err := validKey(key, consts.ValidChannelKeys); err != nil {
		return 0, err
	}

	var id int64
	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = ?",
		consts.QChanID,
		consts.DBChannels,
		key,
	)

	err := cs.q.QueryRow(query, val).Scan(&id)
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
	var u, p, l sql.NullString // Use sql.NullString to handle NULL values.

	query := fmt.Sprintf(
		"SELECT %s, %s, %s FROM %s WHERE %s = ? AND %s = ?",
		consts.QChanURLUsername,
		consts.QChanURLPassword,
		consts.QChanURLLoginURL,
		consts.DBChannelURLs,
		consts.QChanURLChannelID,
		consts.QChanURLURL,
	)

	if err = cs.q.QueryRow(query, channelID, url).Scan(&u, &p, &l); err != nil {
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

// DeleteVideosByURLs removes videos from a channel's database list, so they can be downloaded again. With deleteFiles,
// their video and metadata files are deleted too, once the rows are.
func (cs *ChannelStore) DeleteVideosByURLs(channelID int64, urls []string, deleteFiles bool) error {
	if !cs.channelExistsID(channelID) {
		return fmt.Errorf("channel with ID %d does not exist", channelID)
	}

	// Read the file paths before their rows are deleted.
	var files []string
	if deleteFiles {
		var err error
		if files, err = cs.videoFilePaths(channelID, urls); err != nil {
			return err
		}
	}

	// Remove videos from database.
	deleteQuery := fmt.Sprintf(
		"DELETE FROM %s WHERE %s = ? AND %s IN (%s)",
		consts.DBVideos,
		consts.QVidChanID,
		consts.QVidURL,
		placeholders(len(urls)),
	)
	deleteArgs := make([]any, 0, len(urls)+1)
	deleteArgs = append(deleteArgs, channelID)
	for _, u := range urls {
		deleteArgs = append(deleteArgs, u)
	}

	deleteResult, err := cs.q.Exec(deleteQuery, deleteArgs...)
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			logger.Pl.W("Could not delete file %q: %v", f, err)
		}
	}

	deletedRows, err := deleteResult.RowsAffected()
	if err != nil {
		logger.Pl.E("Failed to retrieve affected rows: %v", err)
	} else if deletedRows == 0 {
		logger.Pl.I("No videos deleted for URLs: %v", urls)
		return nil
	}

	// Remove deleted videos from the unseen list.
	if err := cs.RemoveNewVideoURLs(channelID, urls); err != nil {
		logger.Pl.E("Failed to update new video URLs for channel ID %d: %v", channelID, err)
	}

	logger.Pl.S("Channel ID %d: Deleted videos for URLs %v (files deleted: %t)", channelID, urls, deleteFiles)
	return nil
}

// videoFilePaths returns the saved video and metadata file paths of a channel's videos.
func (cs *ChannelStore) videoFilePaths(channelID int64, urls []string) ([]string, error) {
	args := make([]any, 0, len(urls)+1)
	args = append(args, channelID)
	for _, u := range urls {
		args = append(args, u)
	}
	query := fmt.Sprintf(
		"SELECT %s, %s FROM %s WHERE %s = ? AND %s IN (%s)",
		consts.QVidVideoPath,
		consts.QVidJSONPath,
		consts.DBVideos,
		consts.QVidChanID,
		consts.QVidURL,
		placeholders(len(urls)),
	)

	rows, err := cs.q.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve video paths: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Pl.E("Failed to close rows for delete video request in channel with ID %d", channelID)
		}
	}()

	var paths []string
	for rows.Next() {
		var videoPath, jsonPath sql.NullString
		if err := rows.Scan(&videoPath, &jsonPath); err != nil {
			return nil, fmt.Errorf("failed to scan video paths in channel %d: %w", channelID, err)
		}
		for _, p := range []string{videoPath.String, jsonPath.String} {
			if p != "" {
				paths = append(paths, p)
			}
		}
	}
	return paths, rows.Err()
}

// UpdateChannelFromConfig updates the channel settings from a config file if it exists.
func (cs *ChannelStore) UpdateChannelFromConfig(c *models.Channel) (err error) {
	if c == nil {
		return errors.New("dev error: channel sent in nil")
	}

	cfgFile := c.ChannelConfigFile
	if cfgFile == "" {
		logger.Pl.D(2, "No config file path, nothing to apply")
		return nil
	}

	logger.Pl.I("Applying configurations to channel %q from config file %q...", c.Name, cfgFile)
	if _, _, err := sharedvalidation.ValidateFile(cfgFile, false, sharedtemplates.NoTemplateTags); err != nil {
		return err
	}
	input, err := parsing.LoadChannelInputFile(cfgFile)
	if err != nil {
		return err
	}

	// The file applies to the channel's own settings, not those its profile fills in.
	chanID := strconv.FormatInt(c.ID, 10)
	saved, found, err := cs.GetChannelModel(consts.QChanID, chanID, false)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("channel %q is no longer in the database", c.Name)
	}

	// Make []byte copy of settings before.
	settingsBeforeJSON, metarrBeforeJSON := makeSettingsMetarrArgsCopy(saved.ChanSettings, saved.ChanMetarrArgs, c.Name)

	// Apply changes to model.
	if err := applyConfigChannel(saved, cfgFile, &input); err != nil {
		return err
	}

	// []byte copy of settings after for comparison.
	settingsAfterJSON, metarrAfterJSON := makeSettingsMetarrArgsCopy(saved.ChanSettings, saved.ChanMetarrArgs, c.Name)
	channelChanged := !bytes.Equal(settingsBeforeJSON, settingsAfterJSON) || !bytes.Equal(metarrBeforeJSON, metarrAfterJSON)

	// Its profile, if the file names one.
	profileID := saved.ProfileID
	if input.Profile != nil {
		if profileID, err = cs.GetProfileID(*input.Profile); err != nil {
			return fmt.Errorf("config file %q: %w", cfgFile, err)
		}
	}
	profileChanged := profileID != saved.ProfileID

	changedURLs, err := cs.applyConfigURLSettings(saved, cfgFile, input.URLSettings)
	if err != nil {
		return err
	}

	// Return early if unchanged.
	if !channelChanged && !profileChanged && len(changedURLs) == 0 {
		logger.Pl.D(1, "No changes to channel from config file.")
		return nil
	}

	// Propagate into database.
	if err := cs.inTx(func(tcs *ChannelStore) error {
		if profileChanged {
			if err := tcs.SetChannelProfile(consts.QChanID, chanID, profileID); err != nil {
				return err
			}
		}
		if channelChanged {
			if _, err := tcs.UpdateChannelSettingsJSON(consts.QChanID, chanID, func(s *models.Settings) error {
				*s = *saved.ChanSettings
				return nil
			}); err != nil {
				return err
			}
			if _, err := tcs.UpdateChannelMetarrArgsJSON(consts.QChanID, chanID, func(m *models.MetarrArgs) error {
				*m = *saved.ChanMetarrArgs
				return nil
			}); err != nil {
				return err
			}
		}
		for _, cu := range changedURLs {
			if err := tcs.UpdateChannelURLSettings(cu); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	// Reload, filled in as jobs use it.
	merged, found, err := cs.GetChannelModel(consts.QChanID, chanID, true)
	if err != nil {
		return fmt.Errorf("failed to reload channel %q after applying its config file: %w", c.Name, err)
	}
	if !found {
		return fmt.Errorf("channel %q is no longer in the database", c.Name)
	}
	c.ChanSettings = merged.ChanSettings
	c.ChanMetarrArgs = merged.ChanMetarrArgs
	c.ProfileID = merged.ProfileID
	c.Profile = merged.Profile
	c.URLModels = merged.URLModels

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

	if _, err := cs.q.Exec(query, channelID, ignoreURL, true); err != nil {
		return err
	}

	logger.Pl.S("Added URL %q to ignore list for channel with ID '%d'", ignoreURL, channelID)
	return nil
}

// GetNotifyURLs returns all notification URLs for a given channel.
func (cs *ChannelStore) GetNotifyURLs(id int64) ([]*models.Notification, error) {
	query := fmt.Sprintf(
		"SELECT %s, %s, %s FROM %s WHERE %s = ?",
		consts.QNotifyChanID,
		consts.QNotifyChanURL,
		consts.QNotifyURL,
		consts.DBNotifications,
		consts.QNotifyChanID,
	)

	// Execute query to get rows.
	rows, err := cs.q.Query(query, id)
	if err != nil {
		return nil, fmt.Errorf("failed to query notification URLs: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Pl.E("Failed to close rows for notify URLs in channel with ID %d", id)
		}
	}()

	// Collect notification models.
	var notificationModels []*models.Notification

	for rows.Next() {
		var nModel models.Notification

		err := rows.Scan(
			&nModel.ChannelID,
			&nModel.ChannelURL,
			&nModel.NotifyURL,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan notification URL: %w", err)
		}

		notificationModels = append(notificationModels, &nModel)
	}

	// Check for errors from iterating over rows.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating notification URLs: %w", err)
	}

	return notificationModels, nil
}

// DeleteNotifyURLs deletes notify URLs from the channel, for every channel URL they're set for.
func (cs *ChannelStore) DeleteNotifyURLs(channelID int64, urls []string) error {

	if !cs.channelExistsID(channelID) {
		return fmt.Errorf("channel with ID %d does not exist", channelID)
	}

	query := fmt.Sprintf(
		"DELETE FROM %s WHERE %s = ? AND %s IN (%s)",
		consts.DBNotifications,
		consts.QNotifyChanID,
		consts.QNotifyURL,
		placeholders(len(urls)),
	)

	args := make([]any, 0, 1+len(urls))
	args = append(args, channelID)
	for _, s := range urls {
		args = append(args, s)
	}

	if _, err := cs.q.Exec(query, args...); err != nil {
		return err
	}

	// Counts only, since notification URLs can hold tokens.
	if len(urls) > 0 {
		logger.Pl.S("Deleted %d notify URL(s) for channel with ID '%d'.", len(urls), channelID)
	} else {
		logger.Pl.S("No notify URLs to delete.")
	}
	return nil
}

// AddNotifyURLs sets notification pairs in the database.
func (cs *ChannelStore) AddNotifyURLs(channelID int64, notifications []*models.Notification) error {
	return cs.inTx(func(tcs *ChannelStore) error {
		for _, n := range notifications {
			if err := tcs.addNotifyURL(channelID, n.ChannelURL, n.NotifyURL); err != nil {
				return err
			}
		}
		return nil
	})
}

// AddAuth adds authentication details to a channel's URLs.
func (cs *ChannelStore) AddAuth(chanID int64, authDetails map[string]*models.ChannelAccessDetails) error {
	if !cs.channelExistsID(chanID) {
		return fmt.Errorf("channel with ID %d does not exist", chanID)
	}

	if authDetails == nil {
		logger.Pl.D(1, "No authorization details to add for channel with ID %d", chanID)
		return nil
	}

	query := fmt.Sprintf(
		"UPDATE %s SET %s = ?, %s = ?, %s = ? WHERE %s = ? AND %s = ?",
		consts.DBChannelURLs,
		consts.QChanURLUsername,
		consts.QChanURLPassword,
		consts.QChanURLLoginURL,
		consts.QChanURLChannelID,
		consts.QChanURLURL,
	)
	return cs.inTx(func(tcs *ChannelStore) error {
		for chanURL, a := range authDetails {
			if a.EncryptedPassword == "" && a.Password != "" {
				encrypted, err := tcs.PasswordMgr.Encrypt(a.Password)
				if err != nil {
					return err
				}
				a.EncryptedPassword = encrypted
			}

			res, err := tcs.q.Exec(query, a.Username, a.EncryptedPassword, a.LoginURL, chanID, chanURL)
			if err != nil {
				return err
			}
			if n, err := res.RowsAffected(); err != nil {
				return err
			} else if n == 0 {
				return fmt.Errorf("%w: channel with ID %d has no URL %q", contracts.ErrInvalidChannel, chanID, chanURL)
			}
			logger.Pl.S("Added authentication details for URL %q in channel with ID %d", chanURL, chanID)
		}
		return nil
	})
}

// checkDownloadDirs returns an error if a video, JSON or move op directory that saving s adds to old (all of them if old
// is nil) isn't available. Tubarr never creates them, as a missing one may be on an unmounted drive.
func checkDownloadDirs(old, s *models.Settings) error {
	existing := downloadDirs(old)
	for _, dir := range downloadDirs(s) {
		if slices.Contains(existing, dir) {
			continue
		}
		if err := validation.CheckDestination(dir); err != nil {
			return err
		}
	}
	return nil
}

// validateURLSettings validates a channel URL's own settings as loading the channel does, so what saves can load.
//
// Its paths are left to checkDownloadDirs and checkOutputDir.
func validateURLSettings(cu *models.ChannelURL) error {
	if cu.ChanURLSettings != nil {
		if err := validation.ValidateSettingsModel(cu.ChanURLSettings, false); err != nil {
			return fmt.Errorf("invalid settings for URL %q: %w", cu.URL, err)
		}
	}
	if cu.ChanURLMetarrArgs != nil {
		if err := validation.ValidateMetarrArgsModel(cu.ChanURLMetarrArgs, false); err != nil {
			return fmt.Errorf("invalid Metarr settings for URL %q: %w", cu.URL, err)
		}
	}
	return nil
}

// checkOutputDir returns an error if the Metarr output directory that saving m adds or changes isn't available.
func checkOutputDir(old, m *models.MetarrArgs) error {
	if m == nil || (old != nil && old.OutputDir == m.OutputDir) {
		return nil
	}
	return validation.CheckDestination(m.OutputDir)
}

// checkFiles returns an error if a file in files that isn't in old (all of them if old is nil) doesn't exist. Unchanged
// files aren't checked, so e.g. saving a URL works while its filter file is unavailable.
func checkFiles(old, files []string) error {
	for _, f := range files {
		if f == "" || slices.Contains(old, f) {
			continue
		}
		if _, _, err := sharedvalidation.ValidateFile(f, false, sharedtemplates.TubarrTemplateTags); err != nil {
			return err
		}
	}
	return nil
}

// settingsFiles returns the filter and move op files of s, and the op files of m.
func settingsFiles(s *models.Settings, m *models.MetarrArgs) []string {
	var files []string
	if s != nil {
		files = append(files, s.FilterFile, s.MetaFilterMoveOpFile)
	}
	if m != nil {
		files = append(files, m.MetaOpsFile, m.FilteredMetaOpsFile, m.FilenameOpsFile, m.FilteredFilenameOpsFile)
	}
	return files
}

// downloadDirs returns the video, JSON and move op directories of s.
func downloadDirs(s *models.Settings) []string {
	if s == nil {
		return nil
	}
	dirs := []string{s.VideoDir, s.JSONDir}
	for _, op := range s.MetaFilterMoveOps {
		dirs = append(dirs, op.OutputDir)
	}
	return dirs
}

// AddChannelWithDetails adds a channel with its logins and notifications in one transaction, setting c.ID.
func (cs *ChannelStore) AddChannelWithDetails(c *models.Channel, authDetails map[string]*models.ChannelAccessDetails, notifications []*models.Notification) error {
	return cs.inTx(func(tcs *ChannelStore) error {
		id, err := tcs.AddChannel(c)
		if err != nil {
			return err
		}
		c.ID = id
		if len(authDetails) > 0 {
			if err := tcs.AddAuth(id, authDetails); err != nil {
				return err
			}
		}
		if len(notifications) > 0 {
			return tcs.AddNotifyURLs(id, notifications)
		}
		return nil
	})
}

// AddChannel adds a new channel to the database.
func (cs *ChannelStore) AddChannel(c *models.Channel) (int64, error) {
	if err := cs.checkProfile(c.ProfileID); err != nil {
		return 0, err
	}
	for _, cu := range c.URLModels {
		if err := validation.ValidateChannelURL(cu.URL); err != nil {
			return 0, fmt.Errorf("%w: %w", contracts.ErrInvalidChannel, err)
		}
		if err := cs.checkProfile(cu.ProfileID); err != nil {
			return 0, err
		}
	}

	if cs.channelExists(consts.QChanName, c.Name) {
		return 0, fmt.Errorf("%w: channel with name %q already exists", contracts.ErrInvalidChannel, c.Name)
	}

	// Validate complete models at DB write boundary.
	if err := validation.ValidateSettingsModel(c.ChanSettings, true); err != nil {
		return 0, fmt.Errorf("%w: invalid settings: %w", contracts.ErrInvalidChannel, err)
	}
	if err := validation.ValidateMetarrArgsModel(c.ChanMetarrArgs, true); err != nil {
		return 0, fmt.Errorf("%w: invalid metarr config: %w", contracts.ErrInvalidChannel, err)
	}

	// Channel URLs, whose destinations aren't checked by the validation above.
	for _, cu := range c.URLModels {
		if err := validateURLSettings(cu); err != nil {
			return 0, fmt.Errorf("%w: %w", contracts.ErrInvalidChannel, err)
		}
		if err := checkFiles(nil, settingsFiles(cu.ChanURLSettings, cu.ChanURLMetarrArgs)); err != nil {
			return 0, fmt.Errorf("%w: %w", contracts.ErrInvalidChannel, err)
		}
		if err := checkDownloadDirs(nil, cu.ChanURLSettings); err != nil {
			return 0, fmt.Errorf("%w: %w", contracts.ErrInvalidChannel, err)
		}
		if err := checkOutputDir(nil, cu.ChanURLMetarrArgs); err != nil {
			return 0, fmt.Errorf("%w: %w", contracts.ErrInvalidChannel, err)
		}
	}

	// Convert settings to JSON.
	settingsJSON, err := json.Marshal(c.ChanSettings)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal settings: %w", err)
	}

	// Convert metarr settings to JSON.
	metarrJSON, err := json.Marshal(c.ChanMetarrArgs)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal metarr settings: %w", err)
	}

	// Convert empty slice to JSON.
	newVideoURLs, err := json.Marshal([]string{})
	if err != nil {
		return 0, fmt.Errorf("failed to marshal empty new video URL slice: %w", err)
	}

	var id int64
	if err := cs.inTx(func(tcs *ChannelStore) (err error) {
		id, err = tcs.insertChannel(c, settingsJSON, metarrJSON, newVideoURLs)
		return err
	}); err != nil {
		return 0, err
	}

	cURLs := c.GetURLs()
	logger.Pl.S("Successfully added channel (ID: %d)\n\nName: %s\nURLs: %v\nCrawl Frequency: %d minutes\nFilters: %v\nSettings: %+v\nMetarr Operations: %+v",
		id, c.Name, cURLs, c.ChanSettings.CrawlFreq, c.ChanSettings.Filters, c.ChanSettings, c.ChanMetarrArgs)

	return id, nil
}

// insertChannel inserts the channel and its URLs, returning the new channel ID.
func (cs *ChannelStore) insertChannel(c *models.Channel, settingsJSON, metarrJSON, newVideoURLs []byte) (int64, error) {
	now := time.Now()
	query := fmt.Sprintf(
		"INSERT INTO %s (%s, %s, %s, %s, %s, %s, %s, %s, %s) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		consts.DBChannels,
		consts.QChanName,
		consts.QChanConfigFile,
		consts.QChanSettings,
		consts.QChanMetarr,
		consts.QChanNewVideoURLs,
		consts.QChanLastScan,
		consts.QChanCreatedAt,
		consts.QChanUpdatedAt,
		consts.QChanProfileID,
	)

	result, err := cs.q.Exec(
		query,
		c.Name,
		c.ChannelConfigFile,
		settingsJSON,
		metarrJSON,
		newVideoURLs,
		now,
		now,
		now,
		c.ProfileID,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to insert channel: %w", err)
	}

	// Get the new channel ID.
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last insert ID: %w", err)
	}
	cs.touch(id)

	// Insert URLs into the channel_urls table.
	logger.Pl.D(1, "Inserting %d channel URLs for channel ID %d", len(c.URLModels), id)
	for i, cu := range c.URLModels {
		logger.Pl.D(1, "Inserting URL %d: %q", i+1, cu.URL)

		// Validate URL format (skip empty URLs and the special "manual-downloads" URL).
		if cu.URL != "" && cu.URL != "manual-downloads" {
			if _, err := url.ParseRequestURI(cu.URL); err != nil {
				return 0, fmt.Errorf("invalid URL %q: %w", cu.URL, err)
			}
		}

		if cu.EncryptedPassword == "" && cu.Password != "" {
			if cu.EncryptedPassword, err = cs.PasswordMgr.Encrypt(cu.Password); err != nil {
				return 0, fmt.Errorf("failed to encrypt password for URL %q: %w", cu.URL, err)
			}
		}

		// Check if custom args exist.
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
			"INSERT INTO %s (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
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
			consts.QChanURLProfileID,
			consts.QChanURLName,
		)

		urlInsertResult, err := cs.q.Exec(
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
			cu.ProfileID,
			cu.Name,
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
	return id, nil
}

// DeleteChannel deletes a channel from the database with a given key/value.
func (cs *ChannelStore) DeleteChannel(key, val string) error {
	if err := validKey(key, consts.ValidChannelKeys); err != nil {
		return err
	}

	query := fmt.Sprintf(
		"DELETE FROM %s WHERE %s = ?",
		consts.DBChannels,
		key,
	)

	result, err := cs.q.Exec(query, val)
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

// GetChannelModel fills the channel model from the database.
func (cs *ChannelStore) GetChannelModel(key, val string, mergeURLsWithParent bool) (*models.Channel, bool, error) {
	if err := validKey(key, consts.ValidChannelKeys); err != nil {
		return nil, false, err
	}
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = ?", channelColumns, consts.DBChannels, key)

	var c models.Channel
	settingsJSON, metarrJSON, err := scanChannel(cs.q.QueryRow(query, val), &c)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, fmt.Errorf("failed to scan channel: %w", err)
	}
	if err := cs.loadChannelDetails(&c, settingsJSON, metarrJSON, mergeURLsWithParent); err != nil {
		return nil, true, err
	}
	return &c, true, nil
}

// channelColumns are the channels table columns scanChannel reads, in its order.
var channelColumns = strings.Join([]string{
	consts.QChanID,
	consts.QChanName,
	consts.QChanConfigFile,
	consts.QChanSettings,
	consts.QChanMetarr,
	consts.QChanNewVideoNotification,
	consts.QChanLastScan,
	consts.QChanCreatedAt,
	consts.QChanUpdatedAt,
	consts.QChanProfileID,
}, ", ")

// scanChannel reads a row of channelColumns into c, returning its saved settings JSON.
func scanChannel(row interface{ Scan(...any) error }, c *models.Channel) (settingsJSON, metarrJSON []byte, err error) {
	err = row.Scan(
		&c.ID,
		&c.Name,
		&c.ChannelConfigFile,
		&settingsJSON,
		&metarrJSON,
		&c.NewVideoNotification,
		&c.LastScan,
		&c.CreatedAt,
		&c.UpdatedAt,
		&c.ProfileID,
	)
	return settingsJSON, metarrJSON, err
}

// loadChannelDetails decodes a scanned channel's settings and loads its URLs, marking it if its saved settings are invalid.
func (cs *ChannelStore) loadChannelDetails(c *models.Channel, settingsJSON, metarrJSON []byte, mergeURLsWithParent bool) error {
	if err := unmarshalChannelJSON(c, settingsJSON, metarrJSON); err != nil {
		return err
	}
	loadErr := validateLoadedSettings(c) // Before the URLs are merged with them, as validation normalizes values.

	// Profile.
	var err error
	if c.Profile, err = cs.profileByID(c.ProfileID); err != nil {
		return err
	}
	if mergeURLsWithParent {
		if c.Profile != nil {
			mergeSettings(c.ChanSettings, c.Profile.ProfileSettings)
			mergeMetarrArgs(c.ChanMetarrArgs, c.Profile.ProfileMetarrArgs)
		}
		c.ChanSettings.JSONDir = cmp.Or(c.ChanSettings.JSONDir, c.ChanSettings.VideoDir) // Without a JSON directory, metadata is saved with the videos.
	}

	urlModels, err := cs.GetChannelURLModels(c, mergeURLsWithParent)
	if err != nil {
		return fmt.Errorf("failed to fetch URL models for channel: %w", err)
	}
	c.URLModels = urlModels
	if loadErr == nil {
		loadErr = profilesErr(c)
	}
	if loadErr == nil {
		loadErr = validateLoadedURLSettings(c)
	}
	if loadErr != nil {
		setLoadErr(c, loadErr)
	}
	return nil
}

// unmarshalChannelJSON decodes a channel's saved settings.
func unmarshalChannelJSON(c *models.Channel, settingsJSON, metarrJSON []byte) error {
	if len(settingsJSON) > 0 {
		if err := json.Unmarshal(settingsJSON, &c.ChanSettings); err != nil {
			return fmt.Errorf("failed to unmarshal channel settings: %w", err)
		}
	}
	if len(metarrJSON) > 0 {
		if err := json.Unmarshal(metarrJSON, &c.ChanMetarrArgs); err != nil {
			return fmt.Errorf("failed to unmarshal metarr settings: %w", err)
		}
	}
	return nil
}

// validateLoadedSettings validates a loaded channel's own settings at the DB read boundary. Paths aren't checked, so
// one unavailable path can't stop it loading.
func validateLoadedSettings(c *models.Channel) error {
	if err := validation.ValidateSettingsModel(c.ChanSettings, false); err != nil {
		return fmt.Errorf("invalid settings: %w", err)
	}
	if err := validation.ValidateMetarrArgsModel(c.ChanMetarrArgs, false); err != nil {
		return fmt.Errorf("invalid Metarr settings: %w", err)
	}
	return nil
}

// validateLoadedURLSettings validates a loaded channel's per-URL settings, skipping blank ones and those matching the channel's.
func validateLoadedURLSettings(c *models.Channel) error {
	for _, cu := range c.URLModels {
		if !models.SettingsAllZero(cu.ChanURLSettings) && !models.ChildSettingsMatchParent(c.ChanSettings, cu.ChanURLSettings) {
			if err := validation.ValidateSettingsModel(cu.ChanURLSettings, false); err != nil {
				return fmt.Errorf("invalid settings for URL %q: %w", cu.URL, err)
			}
		}
		if !models.MetarrArgsAllZero(cu.ChanURLMetarrArgs) && !models.ChildMetarrArgsMatchParent(c.ChanMetarrArgs, cu.ChanURLMetarrArgs) {
			if err := validation.ValidateMetarrArgsModel(cu.ChanURLMetarrArgs, false); err != nil {
				return fmt.Errorf("invalid Metarr settings for URL %q: %w", cu.URL, err)
			}
		}
	}
	return nil
}

// setLoadErr marks a loaded channel with invalid saved settings, so it's kept but can't run until they're fixed.
func setLoadErr(c *models.Channel, err error) {
	c.LoadErr = fmt.Errorf("channel %q has invalid saved settings, fix them on its edit page: %w", c.Name, err)
	logger.Pl.D(1, c.LoadErr.Error())
}

// GetAllChannels retrieves all channels in the database.
func (cs *ChannelStore) GetAllChannels(mergeURLsWithParent bool) (channels []*models.Channel, hasRows bool, err error) {
	query := fmt.Sprintf("SELECT %s FROM %s ORDER BY %s", channelColumns, consts.DBChannels, consts.QChanName)
	rows, err := cs.q.Query(query)
	if err != nil {
		return nil, true, fmt.Errorf("failed to query channels: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Pl.E("Failed to close rows: %v", err)
		}
	}()

	for rows.Next() {
		var c models.Channel
		settingsJSON, metarrJSON, err := scanChannel(rows, &c)
		if err != nil {
			return nil, true, fmt.Errorf("failed to scan channel: %w", err)
		}

		// A channel that can't be read is left out, and one failing validation is kept with its LoadErr.
		if err := cs.loadChannelDetails(&c, settingsJSON, metarrJSON, mergeURLsWithParent); err != nil {
			logger.Pl.E("Skipping channel %q: %v", c.Name, err)
			continue
		}
		channels = append(channels, &c)
	}
	if err := rows.Err(); err != nil {
		return nil, true, fmt.Errorf("failed while retrieving channels: %w", err)
	}

	return channels, len(channels) > 0, nil
}

// UpdateChannelMetarrArgsJSON updates args for Metarr output.
func (cs *ChannelStore) UpdateChannelMetarrArgsJSON(key, val string, updateFn func(*models.MetarrArgs) error) (rows int64, err error) {
	err = cs.inTx(func(tcs *ChannelStore) (err error) {
		rows, err = tcs.updateChannelMetarrArgsJSON(key, val, updateFn)
		return err
	})
	return rows, err
}

// updateChannelMetarrArgsJSON reads, updates and writes back the channel's MetarrArgs.
func (cs *ChannelStore) updateChannelMetarrArgsJSON(key, val string, updateFn func(*models.MetarrArgs) error) (int64, error) {
	if err := validKey(key, consts.ValidChannelKeys); err != nil {
		return 0, err
	}

	var (
		channelID   int64
		channelName string
		metarrArgs  json.RawMessage
	)
	query := fmt.Sprintf(
		"SELECT %s, %s, %s FROM %s WHERE %s = ?",
		consts.QChanID,
		consts.QChanName,
		consts.QChanMetarr,
		consts.DBChannels,
		key,
	)

	err := cs.q.QueryRow(query, val).Scan(&channelID, &channelName, &metarrArgs)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("no channel found with key %q and value '%v'", key, val)
	} else if err != nil {
		return 0, fmt.Errorf("failed to get channel settings: %w", err)
	}
	cs.touch(channelID)

	// Unmarshal current settings.
	var args models.MetarrArgs
	if err := json.Unmarshal(metarrArgs, &args); err != nil {
		return 0, fmt.Errorf("failed to unmarshal settings: %w", err)
	}

	// Apply the update.
	old := args
	if err := updateFn(&args); err != nil {
		return 0, fmt.Errorf("failed to update settings: %w", err)
	}

	// Validate updated MetarrArgs at DB write boundary. Only files and folders the update adds or changes are checked, so
	// e.g. an unchanged one being unavailable doesn't stop it.
	if err := validation.ValidateMetarrArgsModel(&args, false); err != nil {
		return 0, fmt.Errorf("%w: updated metarr config is invalid: %w", contracts.ErrInvalidChannel, err)
	}
	if err := checkFiles(settingsFiles(nil, &old), settingsFiles(nil, &args)); err != nil {
		return 0, fmt.Errorf("%w: updated metarr config is invalid: %w", contracts.ErrInvalidChannel, err)
	}
	if err := checkOutputDir(&old, &args); err != nil {
		return 0, fmt.Errorf("%w: updated metarr config is invalid: %w", contracts.ErrInvalidChannel, err)
	}

	// Marshal updated settings.
	updatedArgs, err := json.Marshal(args)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal updated settings: %w", err)
	}

	// Print the updated settings.
	logger.Pl.S("Updated MetarrArgs for channel %q (ID %d): %s", channelName, channelID, string(updatedArgs))

	// Update the database with the new settings.
	updateQuery := fmt.Sprintf(
		"UPDATE %s SET %s = ? WHERE %s = ?",
		consts.DBChannels,
		consts.QChanMetarr,
		key,
	)

	rtn, err := cs.q.Exec(updateQuery, updatedArgs, val)
	if err != nil {
		return 0, fmt.Errorf("failed to update channel settings in database: %w", err)
	}

	return rtn.RowsAffected()
}

// UpdateChannelSettingsJSON updates specific settings in the channel's settings JSON.
func (cs *ChannelStore) UpdateChannelSettingsJSON(key, val string, updateFn func(*models.Settings) error) (rows int64, err error) {
	err = cs.inTx(func(tcs *ChannelStore) (err error) {
		rows, err = tcs.updateChannelSettingsJSON(key, val, updateFn)
		return err
	})
	return rows, err
}

// updateChannelSettingsJSON reads, updates and writes back the channel's settings.
func (cs *ChannelStore) updateChannelSettingsJSON(key, val string, updateFn func(*models.Settings) error) (int64, error) {
	if err := validKey(key, consts.ValidChannelKeys); err != nil {
		return 0, err
	}

	var (
		channelID    int64
		channelName  string
		settingsJSON json.RawMessage
	)
	query := fmt.Sprintf(
		"SELECT %s, %s, %s FROM %s WHERE %s = ?",
		consts.QChanID,
		consts.QChanName,
		consts.QChanSettings,
		consts.DBChannels,
		key,
	)

	err := cs.q.QueryRow(query, val).Scan(&channelID, &channelName, &settingsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("no channel found with key %q and value '%v'", key, val)
	} else if err != nil {
		return 0, fmt.Errorf("failed to get channel settings: %w", err)
	}

	// Unmarshal current settings.
	var settings models.Settings
	if err := json.Unmarshal(settingsJSON, &settings); err != nil {
		return 0, fmt.Errorf("failed to unmarshal settings: %w", err)
	}

	// Apply the update.
	old := settings
	if err := updateFn(&settings); err != nil {
		return 0, fmt.Errorf("failed to update settings: %w", err)
	}

	// Validate updated Settings at DB write boundary. Only files and folders the update adds or changes are checked, so
	// e.g. pausing a channel works while its filter file or drive is unavailable.
	if err := validation.ValidateSettingsModel(&settings, false); err != nil {
		return 0, fmt.Errorf("%w: updated settings are invalid: %w", contracts.ErrInvalidChannel, err)
	}
	if err := checkFiles(settingsFiles(&old, nil), settingsFiles(&settings, nil)); err != nil {
		return 0, fmt.Errorf("%w: updated settings are invalid: %w", contracts.ErrInvalidChannel, err)
	}
	if err := checkDownloadDirs(&old, &settings); err != nil {
		return 0, fmt.Errorf("%w: updated settings are invalid: %w", contracts.ErrInvalidChannel, err)
	}

	// It still needs a video directory, its own or its profile's, checked once the transaction's changes are written.
	if settings.VideoDir != old.VideoDir {
		cs.touch(channelID)
	}

	// Marshal updated settings.
	updatedSettings, err := json.Marshal(settings)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal updated settings: %w", err)
	}

	// Print the updated settings.
	logger.Pl.S("Updated ChannelSettings for channel %q (ID %d): %s", channelName, channelID, string(updatedSettings))

	// Update the database with the new settings.
	updateQuery := fmt.Sprintf(
		"UPDATE %s SET %s = ? WHERE %s = ?",
		consts.DBChannels,
		consts.QChanSettings,
		key,
	)

	rtn, err := cs.q.Exec(updateQuery, updatedSettings, val)
	if err != nil {
		return 0, fmt.Errorf("failed to update channel settings in database: %w", err)
	}

	return rtn.RowsAffected()
}

// UpdateChannelValue updates a single element in the database.
func (cs *ChannelStore) UpdateChannelValue(key, val, col string, updateWithVal any) error {
	if err := validKey(key, consts.ValidChannelKeys); err != nil { // Key name check.
		return err
	}
	if err := validKey(col, consts.ValidChannelKeys); err != nil { // Column name check. Column names can't be SQL parameters, so only known ones are allowed.
		return err
	}

	if !cs.channelExists(key, val) {
		return fmt.Errorf("channel [%s=%s] does not exist", key, val)
	}
	if col == consts.QChanName && cs.channelExists(consts.QChanName, fmt.Sprint(updateWithVal)) {
		return fmt.Errorf("%w: channel with name %q already exists", contracts.ErrInvalidChannel, updateWithVal)
	}

	query := fmt.Sprintf(
		"UPDATE %s SET %s = ? WHERE %s = ?",
		consts.DBChannels,
		col,
		key,
	)

	// Print SQL query.
	if logging.Level() > 1 {
		logger.Pl.P("Executing SQL: %s with args: %v\n", query, []any{updateWithVal, val})
	}

	// Execute query.
	res, err := cs.q.Exec(query, updateWithVal, val)
	if err != nil {
		return err
	}

	// Ensure a row was updated.
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return fmt.Errorf("update failed for channel [%s=%s]: no rows affected", key, val)
	}

	if col != consts.QChanNewVideoNotification {
		logger.Pl.S("Successfully updated channel [%s=%s]: %q column was set to value %+v", key, val, col, updateWithVal)
	} else {
		logger.Pl.D(3, "Channel [%s=%s]: Set %q to %+v", key, val, consts.QChanNewVideoNotification, updateWithVal)
	}
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

	result, err := cs.q.Exec(query, now, now, channelID)
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

// GetDownloadedVideos loads already downloaded videos from the database, without metadata.
func (cs *ChannelStore) GetDownloadedVideos(c *models.Channel) (videos []*models.Video, hasRows bool, err error) {
	if c.ID == 0 {
		return nil, false, errors.New("model entered has no ID")
	}

	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = ? AND %s = 1 ORDER BY %s DESC",
		videoListColumns,
		consts.DBVideos,
		consts.QVidChanID,
		consts.QVidFinished,
		consts.QVidUpdatedAt,
	)

	rows, err := cs.q.Query(query, c.ID)
	if err != nil {
		return nil, true, fmt.Errorf("failed to query videos: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Pl.E("Failed to close rows: %v", err)
		}
	}()

	if videos, err = scanVideoList(rows); err != nil {
		return nil, true, err
	}

	logger.Pl.I("Found %d previously downloaded videos for channel %q", len(videos), c.Name)
	return videos, true, nil
}

// GetDownloadedOrIgnoredVideoURLs loads already downloaded, ignored, canceled, queued, or Available URLs from the
// database, and failed ones crawls can still retry separately.
func (cs *ChannelStore) GetDownloadedOrIgnoredVideoURLs(c *models.Channel) (urls, failedURLs []string, err error) {
	if c.ID == 0 {
		return nil, nil, errors.New("model entered has no ID")
	}

	seen := fmt.Sprintf(
		"%s = 1 OR %s = 1 OR %s IN (SELECT %s FROM %s WHERE %s IN (?, ?, ?, ?) OR (%s = ? AND %s >= ?))",
		consts.QVidFinished,
		consts.QVidIgnored,
		consts.QVidID, consts.QDLVidID, consts.DBDownloads, consts.QDLStatus,
		consts.QDLStatus, consts.QCrawlDLRetries,
	)
	query := fmt.Sprintf(
		"SELECT %s, %s FROM %s WHERE %s = ? AND (%s OR %s IN (SELECT %s FROM %s WHERE %s = ?))",
		consts.QVidURL, seen,
		consts.DBVideos,
		consts.QVidChanID,
		seen,
		consts.QVidID, consts.QDLVidID, consts.DBDownloads, consts.QDLStatus,
	)

	// Canceled videos count as seen, as the user chose not to download them, and so do failed ones out of crawl retries.
	// Queued and downloading ones are already in the download queue.
	seenArgs := []any{consts.DLStatusAvailable, consts.DLStatusCanceled, consts.DLStatusQueued, consts.DLStatusDownloading, consts.DLStatusFailed, consts.MaxCrawlDLRetries}
	args := slices.Concat(seenArgs, []any{c.ID}, seenArgs, []any{consts.DLStatusFailed})
	rows, err := cs.q.Query(query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Pl.E("Failed to close rows for channel %v: %v", c.Name, err)
		}
	}()

	for rows.Next() {
		var (
			url    string
			isSeen bool
		)
		if err := rows.Scan(&url, &isSeen); err != nil {
			return nil, nil, fmt.Errorf("failed to scan row: %w", err)
		}
		if !isSeen {
			failedURLs = append(failedURLs, url)
			continue
		}
		urls = append(urls, url)
		logger.Pl.D(2, "Found downloaded video: %s", url)
	}

	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("row iteration error: %w", err)
	}

	logger.Pl.I("Found %d previously downloaded videos for channel %q", len(urls), c.Name)
	return urls, failedURLs, nil
}

// AddCrawlDLRetries counts a crawl's download retry for each of the channel's failed videos with the given URLs.
func (cs *ChannelStore) AddCrawlDLRetries(channelID int64, videoURLs []string) error {
	query := fmt.Sprintf(
		"UPDATE %s SET %s = %s + 1 WHERE %s = ? AND %s IN (SELECT %s FROM %s WHERE %s = ? AND %s = ?)",
		consts.DBDownloads,
		consts.QCrawlDLRetries, consts.QCrawlDLRetries,
		consts.QDLStatus,
		consts.QDLVidID, consts.QVidID, consts.DBVideos, consts.QVidChanID, consts.QVidURL,
	)
	for _, u := range videoURLs {
		if _, err := cs.q.Exec(query, consts.DLStatusFailed, channelID, u); err != nil {
			return fmt.Errorf("failed to count crawl retry for video %q: %w", u, err)
		}
	}
	return nil
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
	if c.LoadErr != nil {
		fmt.Printf("%s%v%s\n", sharedconsts.ColorRed, c.LoadErr, sharedconsts.ColorReset)
	}

	cURLs := c.GetURLs()
	cURLs = slices.DeleteFunc(cURLs, func(url string) bool {
		return url == consts.ManualDownloadsCol
	})

	// Channel basic info.
	fmt.Printf("\n%sBasic Info:%s\n", sharedconsts.ColorCyan, sharedconsts.ColorReset)
	fmt.Printf("ID: %d\n", c.ID)
	fmt.Printf("Name: %s\n", c.Name)
	fmt.Printf("Config File: %s\n", c.ChannelConfigFile)
	fmt.Printf("Profile: %s\n", profileName(c.ProfileID, c.Profile))
	fmt.Printf("URLs: %+v\n", cURLs)

	// Channel settings.
	if s == nil {
		logger.Pl.P("(Settings not configured)\n")
		return
	}
	fmt.Printf("Paused: %v\n", s.Paused)
	fmt.Printf("Auto Download: %v\n", c.AutoDownloads(nil))

	fmt.Printf("\n%sChannel Settings:%s\n", sharedconsts.ColorCyan, sharedconsts.ColorReset)
	displaySettingsStruct(c.ChanSettings)

	// Metarr settings.
	fmt.Printf("\n%sMetarr Settings:%s\n", sharedconsts.ColorCyan, sharedconsts.ColorReset)
	if m == nil {
		fmt.Printf("(Metarr arguments not configured)\n")
		return
	}
	displayMetarrArgsStruct(c.ChanMetarrArgs)

	// For dev debugging.
	if logging.Level() > 0 {
		fmt.Printf("\n%sNew Video Notifications:%s\n", sharedconsts.ColorCyan, sharedconsts.ColorReset)
		fmt.Printf("Notification Displayed? %v\n", c.NewVideoNotification)
		newVideoURLs, err := cs.GetNewVideoURLs(consts.QChanName, c.Name)
		if err != nil {
			logger.Pl.E("Failed to get new video URLs: %v", err)
		} else {
			fmt.Printf("Unseen URLs: %v\n", newVideoURLs)
		}
	}

	// Notification URLs.
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

	// Auth details.
	gotAuthModels := false
	for _, cu := range c.URLModels {
		if cu.Password == "" && cu.EncryptedPassword != "" {
			cu.Password, err = cs.PasswordMgr.Decrypt(cu.EncryptedPassword)
			if err != nil {
				logger.Pl.W("Failed to decrypt password for channel %q URL %q: %v", c.Name, cu.URL, err)
			}
		}

		if cu.Username != "" || cu.LoginURL != "" || cu.Password != "" {
			fmt.Printf("Channel URL: %s, Username: %s, Password: %s, Login URL: %s\n",
				cu.URL,
				cu.Username,
				auth.StarPassword(cu.Password),
				cu.LoginURL)

			gotAuthModels = true
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
		fmt.Printf("Name: %s\n", u.Name)
		fmt.Printf("Paused? %v\n", u.ChanURLSettings.Paused)
		fmt.Printf("Auto Download? %v\n", c.AutoDownloads(u))
		fmt.Printf("Profile: %s\n", profileName(u.ProfileID, u.Profile))

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
	if s == nil {
		fmt.Printf("(Settings model is nil)\n")
		return
	}

	// Handle UseGlobalCookies pointer bool.
	var useGlobalCookies string
	if s.UseGlobalCookies != nil {
		useGlobalCookies = "False"
		if *s.UseGlobalCookies {
			useGlobalCookies = "True"
		}
	}

	fmt.Printf("Video Directory: %s\n", s.VideoDir)
	fmt.Printf("JSON Directory: %s\n", s.JSONDir)
	fmt.Printf("Crawl Frequency: %d minutes\n", s.CrawlFreq)
	fmt.Printf("Download Concurrency: %d\n", s.DownloadConcurrency)
	fmt.Printf("Cookie Source: %s\n", s.CookiesFromBrowser)
	fmt.Printf("Retries: %d\n", s.Retries)
	fmt.Printf("External Downloader: %s\n", s.ExternalDownloader)
	fmt.Printf("External Downloader Args: %s\n", s.ExternalDownloaderArgs)
	fmt.Printf("Filter Ops: %v\n", s.Filters)
	fmt.Printf("Filter File: %s\n", s.FilterFile)
	fmt.Printf("From Date: %q\n", s.FromDate)
	fmt.Printf("To Date: %q\n", s.ToDate)
	fmt.Printf("Max Filesize: %s\n", s.MaxFilesize)
	fmt.Printf("Move Ops: %v\n", s.MetaFilterMoveOps)
	fmt.Printf("Move Ops File: %s\n", s.MetaFilterMoveOpFile)
	fmt.Printf("Use Global Cookies: %v\n", useGlobalCookies)
	fmt.Printf("Yt-dlp Output Extension: %s\n", s.YtdlpOutputExt)
	fmt.Printf("Yt-dlp Extra Video Args: %s\n", s.ExtraYTDLPVideoArgs)
	fmt.Printf("Yt-dlp Extra Metadata Args: %s\n", s.ExtraYTDLPMetaArgs)
	fmt.Printf("Yt-dlp Extra Crawl Args: %s\n", s.ExtraYTDLPCrawlArgs)
}

// displayMetarrArgsStruct prints the Metarr args structure.
func displayMetarrArgsStruct(m *models.MetarrArgs) {
	fmt.Printf("Default Output Directory: %s\n", m.OutputDir)
	fmt.Printf("Output Filetype: %s\n", m.OutputExt)
	fmt.Printf("Metarr Concurrency: %d\n", m.MetarrConcurrency)
	fmt.Printf("Max CPU: %.2f\n", m.MaxCPU)
	fmt.Printf("Min Free Memory: %s\n", m.MinFreeMem)
	fmt.Printf("HW Acceleration Type: %s\n", m.TranscodeGPU)
	fmt.Printf("HW Acceleration Node: %s\n", m.TranscodeGPUNode)
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

// applyConfigChannel applies a channel's config file input to its settings, validating them.
func applyConfigChannel(c *models.Channel, cfgFile string, input *models.ChannelInput) error {
	if c.ChanSettings == nil {
		c.ChanSettings = &models.Settings{}
	}
	if c.ChanMetarrArgs == nil {
		c.ChanMetarrArgs = &models.MetarrArgs{}
	}

	// Channel config file location.
	if input.ChannelConfigFile != nil {
		if _, _, err := sharedvalidation.ValidateFile(*input.ChannelConfigFile, false, sharedtemplates.NoTemplateTags); err != nil {
			return err
		}
		c.ChannelConfigFile = *input.ChannelConfigFile
	}

	// Files and folders are checked when saved, and only those the file changes.
	if err := parsing.ApplySettingsInput(c.ChanSettings, &input.SettingsInput); err != nil {
		return fmt.Errorf("invalid settings in config file %q: %w", cfgFile, err)
	}
	if err := validation.ValidateSettingsModel(c.ChanSettings, false); err != nil {
		return fmt.Errorf("invalid settings in config file %q: %w", cfgFile, err)
	}
	if err := parsing.ApplyMetarrArgsInput(c.ChanMetarrArgs, &input.MetarrArgsInput); err != nil {
		return fmt.Errorf("invalid metarr config in config file %q: %w", cfgFile, err)
	}
	if err := validation.ValidateMetarrArgsModel(c.ChanMetarrArgs, false); err != nil {
		return fmt.Errorf("invalid metarr config in config file %q: %w", cfgFile, err)
	}
	return nil
}

// applyConfigURLSettings lays a channel config file's url-settings over its URLs' saved settings, returning the URLs
// they change.
func (cs *ChannelStore) applyConfigURLSettings(c *models.Channel, cfgFile string, urlSettings map[string]*models.URLSettingsOverride) ([]*models.ChannelURL, error) {
	if len(urlSettings) == 0 {
		return nil, nil
	}
	saved, err := cs.GetChannelURLModels(c, false)
	if err != nil {
		return nil, err
	}
	var changed []*models.ChannelURL
	for _, cu := range saved {
		override, ok := urlSettings[strings.ToLower(cu.URL)]
		if !ok {
			continue
		}
		s := cmp.Or(cu.ChanURLSettings, &models.Settings{})
		m := cmp.Or(cu.ChanURLMetarrArgs, &models.MetarrArgs{})
		settingsBeforeJSON, metarrBeforeJSON := makeSettingsMetarrArgsCopy(s, m, cu.URL)
		if override.Settings != nil {
			if err := parsing.ApplySettingsInput(s, override.Settings); err != nil {
				return nil, fmt.Errorf("invalid settings for URL %q in config file %q: %w", cu.URL, cfgFile, err)
			}
		}
		if override.Metarr != nil {
			if err := parsing.ApplyMetarrArgsInput(m, override.Metarr); err != nil {
				return nil, fmt.Errorf("invalid metarr config for URL %q in config file %q: %w", cu.URL, cfgFile, err)
			}
		}
		profileID := cu.ProfileID
		if override.Profile != nil {
			if profileID, err = cs.GetProfileID(*override.Profile); err != nil {
				return nil, fmt.Errorf("profile for URL %q in config file %q: %w", cu.URL, cfgFile, err)
			}
		}
		name := cu.Name
		if override.Name != nil {
			name = *override.Name
		}
		cu.ChanURLSettings, cu.ChanURLMetarrArgs = s, m
		if err := validateURLSettings(cu); err != nil { // Normalizes, as saving would, before comparing.
			return nil, fmt.Errorf("config file %q: %w", cfgFile, err)
		}
		settingsAfterJSON, metarrAfterJSON := makeSettingsMetarrArgsCopy(s, m, cu.URL)
		if bytes.Equal(settingsBeforeJSON, settingsAfterJSON) && bytes.Equal(metarrBeforeJSON, metarrAfterJSON) && profileID == cu.ProfileID && name == cu.Name {
			continue
		}
		cu.ProfileID = profileID
		cu.Name = name
		changed = append(changed, cu)
	}
	return changed, nil
}

// addNotifyURL sets a notification table entry for a channel with a given ID.
func (cs *ChannelStore) addNotifyURL(id int64, chanURL, notifyURL string) error {
	if notifyURL == "" {
		return errors.New("please enter a notification URL")
	}

	const querySuffix = "ON CONFLICT (channel_id, channel_url, notify_url) DO UPDATE SET updated_at = EXCLUDED.updated_at"

	now := time.Now()
	query := fmt.Sprintf(
		"INSERT INTO %s (%s, %s, %s, %s, %s) VALUES (?, ?, ?, ?, ?) %s",
		consts.DBNotifications,
		consts.QNotifyChanID,
		consts.QNotifyChanURL,
		consts.QNotifyURL,
		consts.QNotifyCreatedAt,
		consts.QNotifyUpdatedAt,
		querySuffix, // appended like Squirrel's .Suffix()
	)

	_, err := cs.q.Exec(
		query,
		id,
		chanURL,
		notifyURL,
		now,
		now,
	)
	if err != nil {
		return err
	}

	host := ""
	if u, err := url.Parse(notifyURL); err == nil {
		host = u.Host
	}
	logger.Pl.S("Added notification for %q to channel with ID: %d", host, id)
	return nil
}

// channelExists returns true if the channel exists in the database.
func (cs *ChannelStore) channelExists(key, val string) bool {
	if err := validKey(key, consts.ValidChannelKeys); err != nil {
		logger.Pl.E(err.Error())
		return false
	}

	var count int
	query := fmt.Sprintf(
		"SELECT COUNT(1) FROM %s WHERE %s = ?",
		consts.DBChannels,
		key,
	)

	if err := cs.q.QueryRow(query, val).Scan(&count); err != nil {
		logger.Pl.E("failed to check if channel exists with key=%s val=%s: %v", key, val, err)
		return false
	}
	return count > 0
}

// channelExistsID returns true if the channel ID exists in the database.
func (cs *ChannelStore) channelExistsID(id int64) bool {
	var exists bool
	query := fmt.Sprintf(
		"SELECT 1 FROM %s WHERE %s = ?",
		consts.DBChannels,
		consts.QChanID,
	)

	if err := cs.q.QueryRow(query, id).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return false
	} else if err != nil {
		logger.Pl.E("Failed to check if channel with ID %d exists", id)
		return exists
	}

	return exists
}
