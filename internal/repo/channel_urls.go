package repo

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"

	"github.com/Masterminds/squirrel"
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
	query := squirrel.
		Insert(consts.DBChannelURLs).
		Columns(
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
		).
		Values(
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

	query := squirrel.Update(consts.DBChannelURLs).
		Set(consts.QChanURLUsername, cu.Username).
		Set(consts.QChanURLPassword, cu.EncryptedPassword).
		Set(consts.QChanURLLoginURL, cu.LoginURL).
		Set(consts.QChanURLMetarr, metarrJSON).
		Set(consts.QChanURLSettings, settingsJSON).
		Where(squirrel.Eq{consts.QChanURLID: cu.ID}).
		RunWith(cs.DB)

	if _, err := query.Exec(); err != nil {
		return err
	}

	logging.S("Updated channel URL %q:\n\nMetarr Arguments:\n%v\n\nSettings:\n%v", cu.URL, cu.ChanURLMetarrArgs, cu.ChanURLSettings)
	return nil
}

// GetChannelURLModels fetches a filled list of ChannelURL models.
func (cs *ChannelStore) GetChannelURLModels(c *models.Channel) ([]*models.ChannelURL, error) {
	urlQuery := squirrel.
		Select(
			consts.QChanURLID,
			consts.QChanURLURL,
			consts.QChanURLUsername,
			consts.QChanURLPassword,
			consts.QChanURLLoginURL,
			consts.QChanURLIsManual,
			consts.QChanURLSettings,
			consts.QChanMetarr,
			consts.QChanURLLastScan,
			consts.QChanURLCreatedAt,
			consts.QChanURLUpdatedAt,
		).
		From(consts.DBChannelURLs).
		Where(squirrel.Eq{consts.QChanURLChannelID: c.ID}).
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

		// Fill credentials
		cu.Username = nullString(username)
		cu.EncryptedPassword = nullString(password)
		cu.LoginURL = nullString(loginURL)

		if cu.Password == "" && cu.EncryptedPassword != "" {
			cu.Password, err = cs.PasswordMgr.Decrypt(cu.EncryptedPassword)
			if err != nil {
				return nil, err
			}
		}

		// Unmarshal settings
		if len(settingsJSON) > 0 {
			if err := json.Unmarshal(settingsJSON, &cu.ChanURLSettings); err != nil {
				return nil, fmt.Errorf("failed to unmarshal channel URL settings: %w", err)
			}
		}

		// Unmarshal metarr args
		if len(metarrJSON) > 0 {
			if err := json.Unmarshal(metarrJSON, &cu.ChanURLMetarrArgs); err != nil {
				return nil, fmt.Errorf("failed to unmarshal channel URL metarr args: %w", err)
			}
		}

		urlModels = append(urlModels, cu)
	}

	// Apply fallback logic for nil settings
	if len(urlModels) > 0 {
		for _, cu := range urlModels {
			if cu.ChanURLSettings == nil {
				if c.ChanSettings != nil {
					cu.ChanURLSettings = c.ChanSettings
				} else {
					cu.ChanURLSettings = &models.Settings{}
				}
			} else {
				if changed := mergeSettings(cu.ChanURLSettings, c.ChanSettings); changed {
					logging.D(1, "Set empty channel URL (%q) settings from parent channel %q", cu.URL, c.Name)
				}
			}

			if cu.ChanURLMetarrArgs == nil {
				if c.ChanMetarrArgs != nil {
					cu.ChanURLMetarrArgs = c.ChanMetarrArgs
				} else {
					cu.ChanURLMetarrArgs = &models.MetarrArgs{}
				}
			} else {
				if changed := mergeMetarrArgs(cu.ChanURLMetarrArgs, c.ChanMetarrArgs); changed {
					logging.D(1, "Set empty channel URL (%q) Metarr arguments from parent channel %q", cu.URL, c.Name)
				}
			}
		}
	}

	return urlModels, nil
}

// GetChannelURLModel fetches a single ChannelURL model by channel ID and URL.
func (cs *ChannelStore) GetChannelURLModel(channelID int64, urlStr string) (chanURL *models.ChannelURL, hasRows bool, err error) {
	urlQuery := squirrel.
		Select(
			consts.QChanURLID,
			consts.QChanURLURL,
			consts.QChanURLUsername,
			consts.QChanURLPassword,
			consts.QChanURLLoginURL,
			consts.QChanURLIsManual,
			consts.QChanURLSettings,
			consts.QChanMetarr,
			consts.QChanURLLastScan,
			consts.QChanURLCreatedAt,
			consts.QChanURLUpdatedAt,
		).
		From(consts.DBChannelURLs).
		Where(squirrel.Eq{
			consts.QChanURLChannelID: channelID,
			consts.QChanURLURL:       urlStr,
		}).
		RunWith(cs.DB)

	cu := &models.ChannelURL{}
	var (
		username, password, loginURL sql.NullString
		settingsJSON, metarrJSON     []byte
	)

	err = urlQuery.QueryRow().Scan(
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
	)

	if err != nil {
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
	c, hasRows, err := cs.GetChannelModel(consts.QChanID, strconv.FormatInt(channelID, 10))
	if err != nil {
		return nil, true, err
	}
	if !hasRows {
		logging.D(2, "Channel with ID %d not found in database", channelID)
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
				logging.D(1, "Set empty channel URL (%q) settings from parent channel %q", cu.URL, c.Name)
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

	return cu, true, nil
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

	query := squirrel.
		Update(consts.DBChannelURLs).
		Set(consts.QChanURLUsername, "").
		Set(consts.QChanURLPassword, "").
		Set(consts.QChanURLLoginURL, "").
		Where(squirrel.Eq{consts.QChanURLID: cu.ID}).
		RunWith(cs.DB)

	if _, err := query.Exec(); err != nil {
		return fmt.Errorf("failed to delete auth details for channel URL ID %d: %w", cu.ID, err)
	}

	logging.S("Deleted authentication details for channel URL ID %d (%s)", cu.ID, cu.URL)
	return nil
}

// ******************************** Private ********************************

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

// getChannelURLModelsMap retrieves all ChannelURL rows for a given channel in a map by channel ID.
//
// If channelID == 0, it fetches URLs for all channels.
func (cs *ChannelStore) getChannelURLModelsMap(cID int64) (map[int64][]*models.ChannelURL, error) {
	query := squirrel.
		Select(
			consts.QChanURLID,
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
		).
		From(consts.DBChannelURLs)

	if cID != 0 {
		query = query.Where(squirrel.Eq{consts.QChanURLChannelID: cID})
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

	// Scan into models
	var (
		urlMap = make(map[int64][]*models.ChannelURL)
		c      *models.Channel
	)

	for rows.Next() {
		cu := &models.ChannelURL{}
		var username, password, loginURL sql.NullString
		var settingsJSON, metarrJSON []byte
		var channelID int64

		if err := rows.Scan(
			&cu.ID,
			&channelID,
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

		// Fill credentials
		cu.Username = nullString(username)
		cu.EncryptedPassword = nullString(password)
		cu.LoginURL = nullString(loginURL)

		if cu.Password == "" && cu.EncryptedPassword != "" {
			cu.Password, err = cs.PasswordMgr.Decrypt(cu.EncryptedPassword)
			if err != nil {
				return nil, err
			}
		}

		// Unmarshal settings
		if len(settingsJSON) > 0 {
			if err := json.Unmarshal(settingsJSON, &cu.ChanURLSettings); err != nil {
				return nil, fmt.Errorf("failed to unmarshal channel URL settings: %w", err)
			}
		}

		// Unmarshal metarr args
		if len(metarrJSON) > 0 {
			if err := json.Unmarshal(metarrJSON, &cu.ChanURLMetarrArgs); err != nil {
				return nil, fmt.Errorf("failed to unmarshal channel URL metarr args: %w", err)
			}
		}

		// Load channel if not cached or different channel
		if c == nil || c.ID != channelID {
			c, _, err = cs.GetChannelModel(consts.QChanID, strconv.FormatInt(channelID, 10))
			if err != nil {
				return nil, err
			}
		}

		// Handle Settings (struct-level or field-level)
		if cu.ChanURLSettings == nil {
			if c != nil && c.ChanSettings != nil {
				cu.ChanURLSettings = c.ChanSettings
			} else {
				cu.ChanURLSettings = &models.Settings{}
			}
		} else if c != nil && c.ChanSettings != nil {
			mergeSettings(cu.ChanURLSettings, c.ChanSettings)
		}

		// Handle MetarrArgs (struct-level or field-level)
		if cu.ChanURLMetarrArgs == nil {
			if c != nil && c.ChanMetarrArgs != nil {
				cu.ChanURLMetarrArgs = c.ChanMetarrArgs
			} else {
				cu.ChanURLMetarrArgs = &models.MetarrArgs{}
			}
		} else if c != nil && c.ChanMetarrArgs != nil {
			mergeMetarrArgs(cu.ChanURLMetarrArgs, c.ChanMetarrArgs)
		}

		urlMap[channelID] = append(urlMap[channelID], cu)
	}
	return urlMap, nil
}

// mergeSettings merges channel settings into URL settings, only updating empty fields.
//
// Returns true if any changes were made.
func mergeSettings(urlSettings, channelSettings *models.Settings) (changed bool) {
	if urlSettings == nil {
		logging.E("Dev Error: mergeSettings called with nil urlSettings - this should never happen")
		return false
	}
	if channelSettings == nil {
		return false
	}

	var c bool
	// Configuration fields
	urlSettings.ChannelConfigFile, c = mergeStringSettings(urlSettings.ChannelConfigFile, channelSettings.ChannelConfigFile)
	changed = changed || c // True if "changed" OR "c" is true (once changed is true, it remains true throughout function)

	urlSettings.Concurrency, c = mergeNumSettings(urlSettings.Concurrency, channelSettings.Concurrency, 0)
	changed = changed || c

	// Download-related operations
	urlSettings.CookieSource, c = mergeStringSettings(urlSettings.CookieSource, channelSettings.CookieSource)
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

	urlSettings.UseGlobalCookies, c = mergeBoolSettings(urlSettings.UseGlobalCookies, channelSettings.UseGlobalCookies)
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

	urlSettings.MoveOps, c = mergeSliceSettings(urlSettings.MoveOps, channelSettings.MoveOps)
	changed = changed || c

	urlSettings.MoveOpFile, c = mergeStringSettings(urlSettings.MoveOpFile, channelSettings.MoveOpFile)
	changed = changed || c

	urlSettings.FromDate, c = mergeStringSettings(urlSettings.FromDate, channelSettings.FromDate)
	changed = changed || c

	urlSettings.ToDate, c = mergeStringSettings(urlSettings.ToDate, channelSettings.ToDate)
	changed = changed || c

	// JSON and video directories
	urlSettings.MetaDir, c = mergeStringSettings(urlSettings.MetaDir, channelSettings.MetaDir)
	changed = changed || c

	urlSettings.VideoDir, c = mergeStringSettings(urlSettings.VideoDir, channelSettings.VideoDir)
	changed = changed || c

	// Channel toggles
	urlSettings.Paused, c = mergeBoolSettings(urlSettings.Paused, channelSettings.Paused)
	changed = changed || c

	// Note: BotBlocked fields are runtime state, not cascaded
	return changed
}

// mergeMetarrArgs merges channel metarr args into URL metarr args, only updating empty fields.
//
// Returns true if any changes were made.
func mergeMetarrArgs(urlMetarr, channelMetarr *models.MetarrArgs) (changed bool) {
	if urlMetarr == nil {
		logging.E("Dev Error: mergeMetarrArgs called with nil urlMetarr - this should never happen")
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

	// Metarr output directories
	urlMetarr.OutputDirMap, c = mergeMapSettings(urlMetarr.OutputDirMap, channelMetarr.OutputDirMap)
	changed = changed || c

	urlMetarr.OutputDir, c = mergeStringSettings(urlMetarr.OutputDir, channelMetarr.OutputDir)
	changed = changed || c

	urlMetarr.URLOutputDirs, c = mergeSliceSettings(urlMetarr.URLOutputDirs, channelMetarr.URLOutputDirs)
	changed = changed || c

	// Program operations
	urlMetarr.Concurrency, c = mergeNumSettings(urlMetarr.Concurrency, channelMetarr.Concurrency, 0)
	changed = changed || c

	urlMetarr.MaxCPU, c = mergeNumSettings(urlMetarr.MaxCPU, channelMetarr.MaxCPU, 0)
	changed = changed || c

	urlMetarr.MinFreeMem, c = mergeStringSettings(urlMetarr.MinFreeMem, channelMetarr.MinFreeMem)
	changed = changed || c

	// Transcoding
	urlMetarr.UseGPU, c = mergeStringSettings(urlMetarr.UseGPU, channelMetarr.UseGPU)
	changed = changed || c

	urlMetarr.GPUDir, c = mergeStringSettings(urlMetarr.GPUDir, channelMetarr.GPUDir)
	changed = changed || c

	urlMetarr.TranscodeVideoFilter, c = mergeStringSettings(urlMetarr.TranscodeVideoFilter, channelMetarr.TranscodeVideoFilter)
	changed = changed || c

	urlMetarr.TranscodeCodec, c = mergeStringSettings(urlMetarr.TranscodeCodec, channelMetarr.TranscodeCodec)
	changed = changed || c

	urlMetarr.TranscodeAudioCodec, c = mergeStringSettings(urlMetarr.TranscodeAudioCodec, channelMetarr.TranscodeAudioCodec)
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

// mergeBoolSettings checks and cascades bools to the URL model if false.
func mergeBoolSettings(urlBool, chanBool bool) (bool, bool) {
	if !urlBool && chanBool {
		return chanBool, true
	}
	return urlBool, false
}

// mergeMapSettings checks and cascades maps to the URL model if empty.
func mergeMapSettings[K comparable, V any](urlMap, chanMap map[K]V) (map[K]V, bool) {
	if len(urlMap) == 0 && len(chanMap) > 0 {
		newMap := make(map[K]V)
		maps.Copy(newMap, chanMap)
		return newMap, true
	}
	return urlMap, false
}
