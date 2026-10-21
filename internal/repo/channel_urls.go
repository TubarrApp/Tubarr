package repo

import (
	"cmp"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/validation"
)

// AddChannelURL adds a new channel URL to the database.
func (cs *ChannelStore) AddChannelURL(channelID int64, cu *models.ChannelURL, isManual bool) (chanURLID int64, err error) {
	err = cs.inTx(func(tcs *ChannelStore) (err error) {
		chanURLID, err = tcs.addChannelURL(channelID, cu, isManual)
		return err
	})
	return chanURLID, err
}

// addChannelURL adds a new channel URL to the database.
func (cs *ChannelStore) addChannelURL(channelID int64, cu *models.ChannelURL, isManual bool) (chanURLID int64, err error) {
	if !cs.channelExistsID(channelID) {
		return 0, fmt.Errorf("channel with ID %d does not exist", channelID)
	}
	cs.touch(channelID)
	if !isManual {
		if err := validation.ValidateChannelURL(cu.URL); err != nil {
			return 0, err
		}
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

	result, err := cs.q.Exec(query,
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

// RenameChannelURL changes a channel URL's address in place, keeping its videos linked.
func (cs *ChannelStore) RenameChannelURL(channelURLID int64, newURL string) error {
	return cs.inTx(func(tcs *ChannelStore) error {
		if _, err := tcs.q.Exec(
			fmt.Sprintf("UPDATE %s SET %s = ?, %s = ? WHERE %s = ?", consts.DBChannelURLs, consts.QChanURLURL, consts.QChanURLUpdatedAt, consts.QChanURLID),
			newURL, time.Now(), channelURLID,
		); err != nil {
			return fmt.Errorf("failed to rename channel URL %d to %q: %w", channelURLID, newURL, err)
		}
		if _, err := tcs.q.Exec(
			fmt.Sprintf("UPDATE %s SET %s = ? WHERE %s = ?", consts.DBVideos, consts.QVidChanURL, consts.QVidChanURLID),
			newURL, channelURLID,
		); err != nil {
			return fmt.Errorf("failed to update videos of renamed channel URL %q: %w", newURL, err)
		}
		return nil
	})
}

// UpdateChannelURLSettings updates the settings of a single ChannelURL model.
func (cs *ChannelStore) UpdateChannelURLSettings(cu *models.ChannelURL) error {
	return cs.inTx(func(tcs *ChannelStore) error { return tcs.updateChannelURLSettings(cu) })
}

// updateChannelURLSettings updates the settings of a single ChannelURL model.
func (cs *ChannelStore) updateChannelURLSettings(cu *models.ChannelURL) error {
	// Only files and folders this save adds are checked, so e.g. saving the channel works while an unchanged one's drive
	// is unmounted.
	var (
		old                    *models.Settings
		oldMetarr              *models.MetarrArgs
		oldJSON, oldMetarrJSON sql.NullString
		channelID              int64
	)
	oldQuery := fmt.Sprintf("SELECT %s, %s, %s FROM %s WHERE %s = ?", consts.QChanURLChannelID, consts.QChanURLSettings,
		consts.QChanURLMetarr, consts.DBChannelURLs, consts.QChanURLID)
	if err := cs.q.QueryRow(oldQuery, cu.ID).Scan(&channelID, &oldJSON, &oldMetarrJSON); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("failed to read settings of channel URL %q: %w", cu.URL, err)
	}
	cs.touch(channelID)
	if oldJSON.Valid && oldJSON.String != "" {
		if err := json.Unmarshal([]byte(oldJSON.String), &old); err != nil {
			return fmt.Errorf("failed to read settings of channel URL %q: %w", cu.URL, err)
		}
	}
	if oldMetarrJSON.Valid && oldMetarrJSON.String != "" {
		if err := json.Unmarshal([]byte(oldMetarrJSON.String), &oldMetarr); err != nil {
			return fmt.Errorf("failed to read Metarr settings of channel URL %q: %w", cu.URL, err)
		}
	}
	if err := validateURLSettings(cu); err != nil {
		return fmt.Errorf("%w: %w", contracts.ErrInvalidChannel, err)
	}
	if err := checkFiles(settingsFiles(old, oldMetarr), settingsFiles(cu.ChanURLSettings, cu.ChanURLMetarrArgs)); err != nil {
		return fmt.Errorf("%w: %w", contracts.ErrInvalidChannel, err)
	}
	if err := checkDownloadDirs(old, cu.ChanURLSettings); err != nil {
		return fmt.Errorf("%w: %w", contracts.ErrInvalidChannel, err)
	}
	if err := checkOutputDir(oldMetarr, cu.ChanURLMetarrArgs); err != nil {
		return fmt.Errorf("%w: %w", contracts.ErrInvalidChannel, err)
	}
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

	if _, err := cs.q.Exec(
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

// UpdateChannelURLsLastScan records that channel URLs were just crawled, which starts their crawl frequency again.
func (cs *ChannelStore) UpdateChannelURLsLastScan(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	args := make([]any, 0, len(ids)+1)
	args = append(args, time.Now())
	for _, id := range ids {
		args = append(args, id)
	}
	query := fmt.Sprintf(
		"UPDATE %s SET %s = ? WHERE %s IN (%s)",
		consts.DBChannelURLs,
		consts.QChanURLLastScan,
		consts.QChanURLID,
		placeholders(len(ids)),
	)
	if _, err := cs.q.Exec(query, args...); err != nil {
		return fmt.Errorf("failed to update channel URL last scan times: %w", err)
	}
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

	rows, err := cs.q.Query(query, c.ID)
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
			if cu.Password, err = cs.PasswordMgr.Decrypt(cu.EncryptedPassword); err != nil {
				logger.Pl.W("Failed to decrypt password for channel URL %q: %v", cu.URL, err)
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
	if rows.Err() != nil {
		return nil, fmt.Errorf("error occurred getting channel URL models: %w", rows.Err())
	}

	for _, cu := range urlModels {
		if mergeWithParent {
			inheritChannelSettings(cu, c)
		}
		if cu.ChanURLSettings == nil {
			cu.ChanURLSettings = &models.Settings{}
		}
		if cu.ChanURLMetarrArgs == nil {
			cu.ChanURLMetarrArgs = &models.MetarrArgs{}
		}
	}
	return urlModels, nil
}

// inheritChannelSettings fills a channel URL's unset settings from its channel's: a copy of them all if it has none of
// its own, else field by field. Pausing is per URL, so never inherited.
func inheritChannelSettings(cu *models.ChannelURL, c *models.Channel) {
	switch {
	case cu.ChanURLSettings != nil:
		mergeSettings(cu.ChanURLSettings, c.ChanSettings)
	case c.ChanSettings != nil:
		s := *c.ChanSettings
		s.Paused = false
		cu.ChanURLSettings = &s
	}
	switch {
	case cu.ChanURLMetarrArgs != nil:
		mergeMetarrArgs(cu.ChanURLMetarrArgs, c.ChanMetarrArgs)
	case c.ChanMetarrArgs != nil:
		m := *c.ChanMetarrArgs
		cu.ChanURLMetarrArgs = &m
	}
}

// GetChannelURLModel fetches a single ChannelURL model by channel ID and URL.
func (cs *ChannelStore) GetChannelURLModel(channelID int64, urlStr string, mergeWithParent bool) (chanURL *models.ChannelURL, hasRows bool, err error) {
	c, found, err := cs.GetChannelModel(consts.QChanID, strconv.FormatInt(channelID, 10), mergeWithParent)
	if err != nil || !found {
		return nil, found, err
	}
	for _, cu := range c.URLModels {
		if cu.URL == urlStr {
			return cu, true, nil
		}
	}
	return nil, false, nil
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

	if _, err := cs.q.Exec(query, channelURLID); err != nil {
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

	if _, err := cs.q.Exec(query, "", "", "", cu.ID); err != nil {
		return fmt.Errorf("failed to delete auth details for channel URL ID %d: %w", cu.ID, err)
	}

	logger.Pl.S("Deleted authentication details for channel URL ID %d (%s)", cu.ID, cu.URL)
	return nil
}

// ******************************** Private ***************************************************************************************

// channelURLExists returns true if the channel URL exists in the database.
func (cs *ChannelStore) channelURLExists(key, val string) bool {
	if err := validKey(key, consts.ValidChannelURLKeys); err != nil {
		logger.Pl.E("%v", err)
		return false
	}

	var count int
	query := fmt.Sprintf(
		"SELECT COUNT(1) FROM %s WHERE %s = ?",
		consts.DBChannelURLs,
		key,
	)

	if err := cs.q.QueryRow(query, val).Scan(&count); err != nil {
		logger.Pl.E("failed to check if channel URL exists with key=%s val=%s: %v", key, val, err)
		return false
	}

	return count > 0
}

// mergeSettings fills a channel URL's unset settings from the channel's. Pausing is per URL, so never inherited.
func mergeSettings(u, ch *models.Settings) {
	if ch == nil {
		return
	}
	u.Concurrency = cmp.Or(u.Concurrency, ch.Concurrency)
	u.CookiesFromBrowser = cmp.Or(u.CookiesFromBrowser, ch.CookiesFromBrowser)
	u.CrawlFreq = cmp.Or(u.CrawlFreq, ch.CrawlFreq) // A blank field saves as 0.
	u.ExternalDownloader = cmp.Or(u.ExternalDownloader, ch.ExternalDownloader)
	u.ExternalDownloaderArgs = cmp.Or(u.ExternalDownloaderArgs, ch.ExternalDownloaderArgs)
	u.MaxFilesize = cmp.Or(u.MaxFilesize, ch.MaxFilesize)
	u.Retries = cmp.Or(u.Retries, ch.Retries)
	u.UseGlobalCookies = inheritBool(u.UseGlobalCookies, ch.UseGlobalCookies)
	u.YtdlpOutputExt = cmp.Or(u.YtdlpOutputExt, ch.YtdlpOutputExt)
	u.YTDLPPreferredVideoCodecs = inheritSlice(u.YTDLPPreferredVideoCodecs, ch.YTDLPPreferredVideoCodecs)
	u.YTDLPPreferredAudioCodecs = inheritSlice(u.YTDLPPreferredAudioCodecs, ch.YTDLPPreferredAudioCodecs)
	u.ExtraYTDLPVideoArgs = cmp.Or(u.ExtraYTDLPVideoArgs, ch.ExtraYTDLPVideoArgs)
	u.ExtraYTDLPMetaArgs = cmp.Or(u.ExtraYTDLPMetaArgs, ch.ExtraYTDLPMetaArgs)
	u.ExtraYTDLPCrawlArgs = cmp.Or(u.ExtraYTDLPCrawlArgs, ch.ExtraYTDLPCrawlArgs)
	u.Filters = inheritSlice(u.Filters, ch.Filters)
	u.FilterFile = cmp.Or(u.FilterFile, ch.FilterFile)
	u.MetaFilterMoveOps = inheritSlice(u.MetaFilterMoveOps, ch.MetaFilterMoveOps)
	u.MetaFilterMoveOpFile = cmp.Or(u.MetaFilterMoveOpFile, ch.MetaFilterMoveOpFile)
	u.FromDate = cmp.Or(u.FromDate, ch.FromDate)
	u.ToDate = cmp.Or(u.ToDate, ch.ToDate)
	u.JSONDir = cmp.Or(u.JSONDir, ch.JSONDir)
	u.VideoDir = cmp.Or(u.VideoDir, ch.VideoDir)
	u.AutoDownload = inheritBool(u.AutoDownload, ch.AutoDownload)
}

// mergeMetarrArgs fills a channel URL's unset Metarr args from the channel's.
func mergeMetarrArgs(u, ch *models.MetarrArgs) {
	if ch == nil {
		return
	}
	u.OutputExt = cmp.Or(u.OutputExt, ch.OutputExt)
	u.FilenameOps = inheritSlice(u.FilenameOps, ch.FilenameOps)
	u.FilenameOpsFile = cmp.Or(u.FilenameOpsFile, ch.FilenameOpsFile)
	u.FilteredFilenameOps = inheritSlice(u.FilteredFilenameOps, ch.FilteredFilenameOps)
	u.FilteredFilenameOpsFile = cmp.Or(u.FilteredFilenameOpsFile, ch.FilteredFilenameOpsFile)
	u.RenameStyle = cmp.Or(u.RenameStyle, ch.RenameStyle)
	u.MetaOps = inheritSlice(u.MetaOps, ch.MetaOps)
	u.MetaOpsFile = cmp.Or(u.MetaOpsFile, ch.MetaOpsFile)
	u.FilteredMetaOps = inheritSlice(u.FilteredMetaOps, ch.FilteredMetaOps)
	u.FilteredMetaOpsFile = cmp.Or(u.FilteredMetaOpsFile, ch.FilteredMetaOpsFile)
	u.OutputDir = cmp.Or(u.OutputDir, ch.OutputDir)
	u.MetaPurge = cmp.Or(u.MetaPurge, ch.MetaPurge)
	u.Concurrency = cmp.Or(u.Concurrency, ch.Concurrency)
	u.MaxCPU = cmp.Or(u.MaxCPU, ch.MaxCPU)
	u.MinFreeMem = cmp.Or(u.MinFreeMem, ch.MinFreeMem)
	u.TranscodeGPU = cmp.Or(u.TranscodeGPU, ch.TranscodeGPU)
	u.TranscodeGPUNode = cmp.Or(u.TranscodeGPUNode, ch.TranscodeGPUNode)
	u.TranscodeVideoFilter = cmp.Or(u.TranscodeVideoFilter, ch.TranscodeVideoFilter)
	u.TranscodeVideoCodecs = inheritSlice(u.TranscodeVideoCodecs, ch.TranscodeVideoCodecs)
	u.TranscodeAudioCodecs = inheritSlice(u.TranscodeAudioCodecs, ch.TranscodeAudioCodecs)
	u.TranscodeQuality = cmp.Or(u.TranscodeQuality, ch.TranscodeQuality)
	u.ExtraFFmpegArgs = cmp.Or(u.ExtraFFmpegArgs, ch.ExtraFFmpegArgs)
}

// inheritSlice returns s, or a copy of parent if s is empty.
func inheritSlice[T any](s, parent []T) []T {
	if len(s) == 0 && len(parent) > 0 {
		return slices.Clone(parent)
	}
	return s
}

// inheritBool returns b, or a copy of parent if b is unset.
func inheritBool(b, parent *bool) *bool {
	if b == nil && parent != nil {
		v := *parent
		return &v
	}
	return b
}
