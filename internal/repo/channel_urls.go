package repo

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"

	"golang.org/x/exp/constraints"
)

// AddChannelURL adds a new channel URL to the database.
func (cs *ChannelStore) AddChannelURL(channelID int64, cu *models.ChannelURL, isManual bool) (chanURLID int64, err error) {
	if !cs.channelExistsID(channelID) {
		return 0, fmt.Errorf("channel with ID %d does not exist", channelID)
	}

	var settingsJSON, metarrJSON []byte
	if cu.ChanURLSettings != nil {
		settingsJSON, err = json.Marshal(cu.ChanURLSettings)
		if err != nil {
			return 0, fmt.Errorf("failed to marshal channel URL settings: %w", err)
		}
	}

	if cu.ChanURLMetarrArgs != nil {
		metarrJSON, err = json.Marshal(cu.ChanURLMetarrArgs)
		if err != nil {
			return 0, fmt.Errorf("failed to marshal channel URL metarr args: %w", err)
		}
	}

	if cu.EncryptedPassword == "" && cu.Password != "" {
		cu.EncryptedPassword, err = cs.PasswordMgr.Encrypt(cu.Password)
		if err != nil {
			return 0, err
		}
	}

	now := time.Now()
	query := fmt.Sprintf(
		"INSERT INTO %s (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		consts.DBChannelURLs,
		consts.QChanURLChannelID,
		consts.QChanURLURL,
		consts.QChanURLUsername,
		consts.QChanURLPassword,
		consts.QChanURLLoginURL,
		consts.QChanURLIsManual,
		consts.QChanURLSettings,
		consts.QChanURLMetarr,
		consts.QChanURLLastScan,
		consts.QChanURLCreatedAt,
		consts.QChanURLUpdatedAt,
	)

	result, err := cs.DB.Exec(query,
		channelID,
		cu.URL,
		cu.Username,
		cu.EncryptedPassword,
		cu.LoginURL,
		isManual,
		settingsJSON,
		metarrJSON,
		now,
		now,
		now,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to insert channel URL: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last insert ID: %w", err)
	}

	logger.Pl.D(1, "Added channel URL %q (ID: %d, is_manual: %v) to channel ID %d", cu.URL, id, isManual, channelID)
	return id, nil
}

// UpdateChannelURLSettings updates the settings of a single ChannelURL model.
func (cs *ChannelStore) UpdateChannelURLSettings(cu *models.ChannelURL) error {
	metarrJSON, err := json.Marshal(cu.ChanURLMetarrArgs)
	if err != nil {
		return fmt.Errorf("failed to marshal metarr args: %w", err)
	}
	settingsJSON, err := json.Marshal(cu.ChanURLSettings)
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	if cu.EncryptedPassword == "" && cu.Password != "" {
		cu.EncryptedPassword, err = cs.PasswordMgr.Encrypt(cu.Password)
		if err != nil {
			return err
		}
	}

	query := fmt.Sprintf(
		"UPDATE %s SET "+
			"%s = ?, "+
			"%s = ?, "+
			"%s = ?, "+
			"%s = ?, "+
			"%s = ? "+
			"WHERE %s = ?",
		consts.DBChannelURLs,
		consts.QChanURLUsername,
		consts.QChanURLPassword,
		consts.QChanURLLoginURL,
		consts.QChanURLMetarr,
		consts.QChanURLSettings,
		consts.QChanURLID,
	)

	if _, err := cs.DB.Exec(
		query,
		cu.Username,
		cu.EncryptedPassword,
		cu.LoginURL,
		metarrJSON,
		settingsJSON,
		cu.ID,
	); err != nil {
		return err
	}

	logger.Pl.S("Updated channel URL %q:\n\nMetarr Arguments:\n%v\n\nSettings:\n%v", cu.URL, cu.ChanURLMetarrArgs, cu.ChanURLSettings)
	return nil
}

// GetChannelURLModels fetches a filled list of ChannelURL models.
func (cs *ChannelStore) GetChannelURLModels(c *models.Channel, mergeWithParent bool) ([]*models.ChannelURL, error) {
	query := fmt.Sprintf(
		"SELECT %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s "+
			"FROM %s "+
			"WHERE %s = ?",
		consts.QChanURLID,
		consts.QChanURLURL,
		consts.QChanURLUsername,
		consts.QChanURLPassword,
		consts.QChanURLLoginURL,
		consts.QChanURLIsManual,
		consts.QChanURLSettings,
		consts.QChanURLMetarr,
		consts.QChanURLLastScan,
		consts.QChanURLCreatedAt,
		consts.QChanURLUpdatedAt,
		consts.DBChannelURLs,
		consts.QChanURLChannelID,
	)

	rows, err := cs.DB.Query(query, c.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to query channel URLs: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Pl.E("Failed to close rows: %v", err)
		}
	}()

	var urlModels []*models.ChannelURL
	for rows.Next() {
		cu := &models.ChannelURL{}
		var (
			username, password, loginURL sql.NullString
			settingsJSON, metarrJSON     []byte
		)

		if err := rows.Scan(
			&cu.ID,
			&cu.URL,
			&username,
			&password,
			&loginURL,
			&cu.IsManual,
			&settingsJSON,
			&metarrJSON,
			&cu.LastScan,
			&cu.CreatedAt,
			&cu.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan channel URL: %w", err)
		}

		// Fill credentials.
		cu.Username = nullString(username)
		cu.EncryptedPassword = nullString(password)
		cu.LoginURL = nullString(loginURL)

		if cu.Password == "" && cu.EncryptedPassword != "" {
			cu.Password, err = cs.PasswordMgr.Decrypt(cu.EncryptedPassword)
			if err != nil {
				return nil, err
			}
		}

		// Unmarshal settings.
		if len(settingsJSON) > 0 {
			if err := json.Unmarshal(settingsJSON, &cu.ChanURLSettings); err != nil {
				return nil, fmt.Errorf("failed to unmarshal channel URL settings: %w", err)
			}
		}

		// Unmarshal metarr args.
		if len(metarrJSON) > 0 {
			if err := json.Unmarshal(metarrJSON, &cu.ChanURLMetarrArgs); err != nil {
				return nil, fmt.Errorf("failed to unmarshal channel URL metarr args: %w", err)
			}
		}

		urlModels = append(urlModels, cu)
	}

	// Apply fallback logic for nil settings.
	if len(urlModels) > 0 {
		for _, cu := range urlModels {

			// Merge Settings and MetarrArgs with parent.
			if mergeWithParent {
				// Settings.
				if cu.ChanURLSettings == nil {
					if c.ChanSettings != nil {
						// Create a copy of parent Settings.
						settingsCopy := *c.ChanSettings

						// Don't inherit Paused/UseGlobalCookies bools.
						settingsCopy.Paused = false
						settingsCopy.UseGlobalCookies = false

						// Set copy to ChanURLSettings.
						cu.ChanURLSettings = &settingsCopy
					} else {
						cu.ChanURLSettings = &models.Settings{}
					}
				} else {
					if changed := mergeSettings(cu.ChanURLSettings, c.ChanSettings); changed {
						logger.Pl.D(3, "Set empty channel URL (%q) settings from parent channel %q", cu.URL, c.Name)
					}
				}

				// Metarr args.
				if cu.ChanURLMetarrArgs == nil {
					if c.ChanMetarrArgs != nil {
						// Create a copy of parent MetarrArgs.
						metarrCopy := *c.ChanMetarrArgs
						cu.ChanURLMetarrArgs = &metarrCopy
					} else {
						cu.ChanURLMetarrArgs = &models.MetarrArgs{}
					}
				} else {
					if changed := mergeMetarrArgs(cu.ChanURLMetarrArgs, c.ChanMetarrArgs); changed {
						logger.Pl.D(3, "Set empty channel URL (%q) Metarr arguments from parent channel %q", cu.URL, c.Name)
					}
				}
			}

			// Initialize nil to empty.
			if cu.ChanURLSettings == nil {
				cu.ChanURLSettings = &models.Settings{}
			}
			if cu.ChanURLMetarrArgs == nil {
				cu.ChanURLMetarrArgs = &models.MetarrArgs{}
			}
		}
	}

	return urlModels, nil
}

// GetChannelURLModel fetches a single ChannelURL model by channel ID and URL.
func (cs *ChannelStore) GetChannelURLModel(channelID int64, urlStr string, mergeWithParent bool) (chanURL *models.ChannelURL, hasRows bool, err error) {
	query := fmt.Sprintf(
		"SELECT %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s "+
			"FROM %s "+
			"WHERE %s = ? AND %s = ?",
		consts.QChanURLID,
		consts.QChanURLURL,
		consts.QChanURLUsername,
		consts.QChanURLPassword,
		consts.QChanURLLoginURL,
		consts.QChanURLIsManual,
		consts.QChanURLSettings,
		consts.QChanURLMetarr,
		consts.QChanURLLastScan,
		consts.QChanURLCreatedAt,
		consts.QChanURLUpdatedAt,
		consts.DBChannelURLs,
		consts.QChanURLChannelID,
		consts.QChanURLURL,
	)

	cu := &models.ChannelURL{}
	var (
		username, password, loginURL sql.NullString
		settingsJSON, metarrJSON     []byte
	)

	if err := cs.DB.QueryRow(query, channelID, urlStr).Scan(
		&cu.ID,
		&cu.URL,
		&username,
		&password,
		&loginURL,
		&cu.IsManual,
		&settingsJSON,
		&metarrJSON,
		&cu.LastScan,
		&cu.CreatedAt,
		&cu.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil // Not found, return nil without error
		}
		return nil, true, fmt.Errorf("failed to query channel URL: %w", err)
	}

	// Fill credentials
	cu.Username = nullString(username)
	cu.EncryptedPassword = nullString(password)
	cu.LoginURL = nullString(loginURL)

	if cu.Password == "" && cu.EncryptedPassword != "" {
		cu.Password, err = cs.PasswordMgr.Decrypt(cu.EncryptedPassword)
		if err != nil {
			return nil, true, err
		}
	}

	// Unmarshal settings
	if len(settingsJSON) > 0 {
		if err := json.Unmarshal(settingsJSON, &cu.ChanURLSettings); err != nil {
			return nil, true, fmt.Errorf("failed to unmarshal channel URL settings: %w", err)
		}
	}

	// Unmarshal metarr args
	if len(metarrJSON) > 0 {
		if err := json.Unmarshal(metarrJSON, &cu.ChanURLMetarrArgs); err != nil {
			return nil, true, fmt.Errorf("failed to unmarshal channel URL metarr args: %w", err)
		}
	}

	// Apply fallback logic for empty settings
	if mergeWithParent {
		c, hasRows, err := cs.GetChannelModel(consts.QChanID, strconv.FormatInt(channelID, 10), mergeWithParent)
		if err != nil {
			return nil, true, err
		}
		if !hasRows {
			logger.Pl.D(2, "Channel with ID %d not found in database", channelID)
		}

		// Handle Settings
		if cu.ChanURLSettings == nil {
			// Struct-level inheritance: use entire channel settings
			if c != nil && c.ChanSettings != nil {
				cu.ChanURLSettings = c.ChanSettings
			} else {
				cu.ChanURLSettings = &models.Settings{}
			}
		} else {
			// Field-level inheritance: merge empty fields from channel
			if c != nil && c.ChanSettings != nil {
				if changed := mergeSettings(cu.ChanURLSettings, c.ChanSettings); changed {
					logger.Pl.D(3, "Set empty channel URL (%q) settings from parent channel %q", cu.URL, c.Name)
				}
			}
		}

		// Handle MetarrArgs
		if cu.ChanURLMetarrArgs == nil {
			// Struct-level inheritance: use entire channel metarr args
			if c != nil && c.ChanMetarrArgs != nil {
				cu.ChanURLMetarrArgs = c.ChanMetarrArgs
			} else {
				cu.ChanURLMetarrArgs = &models.MetarrArgs{}
			}
		} else {
			// Field-level inheritance: merge empty fields from channel
			if c != nil && c.ChanMetarrArgs != nil {
				mergeMetarrArgs(cu.ChanURLMetarrArgs, c.ChanMetarrArgs)
			}
		}
	}

	// Initialize nil to empty
	if cu.ChanURLMetarrArgs == nil {
		cu.ChanURLMetarrArgs = &models.MetarrArgs{}
	}
	if cu.ChanURLSettings == nil {
		cu.ChanURLSettings = &models.Settings{}
	}

	return cu, true, nil
}

// DeleteChannelURL deletes a channel URL from the database.
func (cs *ChannelStore) DeleteChannelURL(channelURLID int64) error {
	cuIDStr := strconv.FormatInt(channelURLID, 10)
	if !cs.channelURLExists(consts.QChanURLID, cuIDStr) {
		return fmt.Errorf("channel URL with ID %d does not exist in the database", channelURLID)
	}

	query := fmt.Sprintf(
		"DELETE FROM %s WHERE %s = ?",
		consts.DBChannelURLs,
		consts.QChanURLID,
	)

	if _, err := cs.DB.Exec(query, channelURLID); err != nil {
		return fmt.Errorf("failed to delete channel URL ID %d: %w", channelURLID, err)
	}

	logger.Pl.S("Deleted channel URL ID %d", channelURLID)
	return nil
}

// DeleteChannelURLAuth strips a Channel URL of authentication details when decryption key is empty.
func (cs *ChannelStore) DeleteChannelURLAuth(cu *models.ChannelURL) error {
	cuIDStr := strconv.FormatInt(cu.ID, 10)
	if !cs.channelURLExists(consts.QChanURLChannelID, cuIDStr) {
		return fmt.Errorf("channel URL with ID %d does not exist in the database", cu.ID)
	}

	cu.Username = ""
	cu.Password = ""
	cu.EncryptedPassword = ""
	cu.LoginURL = ""

	query := fmt.Sprintf(
		"UPDATE %s SET "+
			"%s = ?, "+
			"%s = ?, "+
			"%s = ? "+
			"WHERE %s = ?",
		consts.DBChannelURLs,
		consts.QChanURLUsername,
		consts.QChanURLPassword,
		consts.QChanURLLoginURL,
		consts.QChanURLID,
	)

	if _, err := cs.DB.Exec(query, "", "", "", cu.ID); err != nil {
		return fmt.Errorf("failed to delete auth details for channel URL ID %d: %w", cu.ID, err)
	}

	logger.Pl.S("Deleted authentication details for channel URL ID %d (%s)", cu.ID, cu.URL)
	return nil
}

// ******************************** Private ***************************************************************************************

// channelURLExists returns true if the channel URL exists in the database.
func (cs *ChannelStore) channelURLExists(key, val string) bool {
	if _, ok := consts.ValidChannelURLKeys[key]; !ok {
		logger.Pl.E("key %q is not valid for table. Valid keys: %v", key, consts.ValidChannelURLKeys)
		return false
	}

	var count int
	query := fmt.Sprintf(
		"SELECT COUNT(1) FROM %s WHERE %s = ?",
		consts.DBChannelURLs,
		key,
	)

	if err := cs.DB.QueryRow(query, val).Scan(&count); err != nil {
		logger.Pl.E("failed to check if channel URL exists with key=%s val=%s: %v", key, val, err)
		return false
	}

	return count > 0
}

// mergeSettings merges channel settings into URL settings, only updating empty fields.
//
// Returns true if any changes were made.
func mergeSettings(urlSettings, channelSettings *models.Settings) (changed bool) {
	if urlSettings == nil {
		logger.Pl.E("Dev Error: mergeSettings called with nil urlSettings - this should never happen")
		return false
	}
	if channelSettings == nil {
		return false
	}

	var c bool
	// Configuration fields
	urlSettings.Concurrency, c = mergeNumSettings(urlSettings.Concurrency, channelSettings.Concurrency, 0)
	changed = changed || c

	// Download-related operations
	urlSettings.CookiesFromBrowser, c = mergeStringSettings(urlSettings.CookiesFromBrowser, channelSettings.CookiesFromBrowser)
	changed = changed || c

	urlSettings.CrawlFreq, c = mergeNumSettings(urlSettings.CrawlFreq, channelSettings.CrawlFreq, -1)
	changed = changed || c

	urlSettings.ExternalDownloader, c = mergeStringSettings(urlSettings.ExternalDownloader, channelSettings.ExternalDownloader)
	changed = changed || c

	urlSettings.ExternalDownloaderArgs, c = mergeStringSettings(urlSettings.ExternalDownloaderArgs, channelSettings.ExternalDownloaderArgs)
	changed = changed || c

	urlSettings.MaxFilesize, c = mergeStringSettings(urlSettings.MaxFilesize, channelSettings.MaxFilesize)
	changed = changed || c

	urlSettings.Retries, c = mergeNumSettings(urlSettings.Retries, channelSettings.Retries, 0)
	changed = changed || c

	urlSettings.YtdlpOutputExt, c = mergeStringSettings(urlSettings.YtdlpOutputExt, channelSettings.YtdlpOutputExt)
	changed = changed || c

	// Custom args
	urlSettings.ExtraYTDLPVideoArgs, c = mergeStringSettings(urlSettings.ExtraYTDLPVideoArgs, channelSettings.ExtraYTDLPVideoArgs)
	changed = changed || c

	urlSettings.ExtraYTDLPMetaArgs, c = mergeStringSettings(urlSettings.ExtraYTDLPMetaArgs, channelSettings.ExtraYTDLPMetaArgs)
	changed = changed || c

	// Metadata operations
	urlSettings.Filters, c = mergeSliceSettings(urlSettings.Filters, channelSettings.Filters)
	changed = changed || c

	urlSettings.FilterFile, c = mergeStringSettings(urlSettings.FilterFile, channelSettings.FilterFile)
	changed = changed || c

	urlSettings.MetaFilterMoveOps, c = mergeSliceSettings(urlSettings.MetaFilterMoveOps, channelSettings.MetaFilterMoveOps)
	changed = changed || c

	urlSettings.MetaFilterMoveOpFile, c = mergeStringSettings(urlSettings.MetaFilterMoveOpFile, channelSettings.MetaFilterMoveOpFile)
	changed = changed || c

	urlSettings.FromDate, c = mergeStringSettings(urlSettings.FromDate, channelSettings.FromDate)
	changed = changed || c

	urlSettings.ToDate, c = mergeStringSettings(urlSettings.ToDate, channelSettings.ToDate)
	changed = changed || c

	// JSON and video directories
	urlSettings.JSONDir, c = mergeStringSettings(urlSettings.JSONDir, channelSettings.JSONDir)
	changed = changed || c

	urlSettings.VideoDir, c = mergeStringSettings(urlSettings.VideoDir, channelSettings.VideoDir)
	changed = changed || c

	// Note: BotBlocked fields are per-URL, not cascaded
	return changed
}

// mergeMetarrArgs merges channel metarr args into URL metarr args, only updating empty fields.
//
// Returns true if any changes were made.
func mergeMetarrArgs(urlMetarr, channelMetarr *models.MetarrArgs) (changed bool) {
	if urlMetarr == nil {
		logger.Pl.E("Dev Error: mergeMetarrArgs called with nil urlMetarr - this should never happen")
		return false
	}
	if channelMetarr == nil {
		return false
	}

	var c bool
	// Metarr file operations
	urlMetarr.OutputExt, c = mergeStringSettings(urlMetarr.OutputExt, channelMetarr.OutputExt)
	changed = changed || c

	urlMetarr.RenameStyle, c = mergeStringSettings(urlMetarr.RenameStyle, channelMetarr.RenameStyle)
	changed = changed || c

	urlMetarr.FilenameOps, c = mergeSliceSettings(urlMetarr.FilenameOps, channelMetarr.FilenameOps)
	changed = changed || c

	urlMetarr.FilteredFilenameOps, c = mergeSliceSettings(urlMetarr.FilteredFilenameOps, channelMetarr.FilteredFilenameOps)
	changed = changed || c

	// Metarr metadata operations
	urlMetarr.MetaOps, c = mergeSliceSettings(urlMetarr.MetaOps, channelMetarr.MetaOps)
	changed = changed || c

	urlMetarr.OutputDir, c = mergeStringSettings(urlMetarr.OutputDir, channelMetarr.OutputDir)
	changed = changed || c

	// Program operations
	urlMetarr.Concurrency, c = mergeNumSettings(urlMetarr.Concurrency, channelMetarr.Concurrency, 0)
	changed = changed || c

	urlMetarr.MaxCPU, c = mergeNumSettings(urlMetarr.MaxCPU, channelMetarr.MaxCPU, 0)
	changed = changed || c

	urlMetarr.MinFreeMem, c = mergeStringSettings(urlMetarr.MinFreeMem, channelMetarr.MinFreeMem)
	changed = changed || c

	// Transcoding
	urlMetarr.TranscodeGPU, c = mergeStringSettings(urlMetarr.TranscodeGPU, channelMetarr.TranscodeGPU)
	changed = changed || c

	urlMetarr.TranscodeVideoFilter, c = mergeStringSettings(urlMetarr.TranscodeVideoFilter, channelMetarr.TranscodeVideoFilter)
	changed = changed || c

	urlMetarr.TranscodeVideoCodecs, c = mergeSliceSettings(urlMetarr.TranscodeVideoCodecs, channelMetarr.TranscodeVideoCodecs)
	changed = changed || c

	urlMetarr.TranscodeAudioCodecs, c = mergeSliceSettings(urlMetarr.TranscodeAudioCodecs, channelMetarr.TranscodeAudioCodecs)
	changed = changed || c

	urlMetarr.TranscodeQuality, c = mergeStringSettings(urlMetarr.TranscodeQuality, channelMetarr.TranscodeQuality)
	changed = changed || c

	urlMetarr.ExtraFFmpegArgs, c = mergeStringSettings(urlMetarr.ExtraFFmpegArgs, channelMetarr.ExtraFFmpegArgs)
	changed = changed || c

	return changed
}

// mergeStringSettings checks and cascades strings to the URL model if empty.
func mergeStringSettings(urlStr, chanStr string) (newURLStr string, changed bool) {
	if urlStr == "" && chanStr != "" {
		return chanStr, true
	}
	return urlStr, false
}

// mergeSliceSettings checks and cascades slices to the URL model if empty.
func mergeSliceSettings[T any](urlSlice, chanSlice []T) ([]T, bool) {
	if len(urlSlice) == 0 && len(chanSlice) > 0 {
		newSlice := make([]T, len(chanSlice))
		copy(newSlice, chanSlice)
		return newSlice, true
	}
	return urlSlice, false
}

// mergeNumSettings checks and cascades numbers to the URL model if empty.
func mergeNumSettings[T constraints.Integer | constraints.Float](urlNum, chanNum, defaultNum T) (T, bool) {
	if urlNum == defaultNum && chanNum != defaultNum {
		return chanNum, true
	}
	return urlNum, false
}
