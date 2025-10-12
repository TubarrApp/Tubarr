package repo

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"

	"github.com/Masterminds/squirrel"
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
			cu.Password,
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

	query := squirrel.Update(consts.DBChannelURLs).
		Set(consts.QChanURLUsername, cu.Username).
		Set(consts.QChanURLPassword, cu.Password).
		Set(consts.QChanURLLoginURL, cu.LoginURL).
		Set(consts.QChanURLMetarr, metarrJSON).
		Set(consts.QChanURLSettings, settingsJSON).
		Where(squirrel.Eq{consts.QChanURLID: cu.ID}).
		RunWith(cs.DB)

	if _, err := query.Exec(); err != nil {
		return err
	}

	logging.S(0, "Updated channel URL %q:\n\nMetarr Arguments:\n%v\n\nSettings:\n%v", cu.URL, cu.ChanURLMetarrArgs, cu.ChanURLSettings)
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
		cu.Password = nullString(password)
		cu.LoginURL = nullString(loginURL)

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
			}
			if cu.ChanURLMetarrArgs == nil {
				if c.ChanMetarrArgs != nil {
					cu.ChanURLMetarrArgs = c.ChanMetarrArgs
				} else {
					cu.ChanURLMetarrArgs = &models.MetarrArgs{}
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
	cu.Password = nullString(password)
	cu.LoginURL = nullString(loginURL)

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

	// Apply fallback logic if settings are nil
	if cu.ChanURLSettings == nil || cu.ChanURLMetarrArgs == nil {
		c, hasRows, err := cs.GetChannelModel(consts.QChanID, strconv.FormatInt(channelID, 10))
		if err != nil {
			return nil, true, err
		}
		if !hasRows {
			logging.D(2, "Channel with ID %d not found in database", channelID)
		}

		if cu.ChanURLSettings == nil {
			if c != nil && c.ChanSettings != nil {
				cu.ChanURLSettings = c.ChanSettings
			} else {
				cu.ChanURLSettings = &models.Settings{}
			}
		}

		if cu.ChanURLMetarrArgs == nil {
			if c != nil && c.ChanMetarrArgs != nil {
				cu.ChanURLMetarrArgs = c.ChanMetarrArgs
			} else {
				cu.ChanURLMetarrArgs = &models.MetarrArgs{}
			}
		}
	}

	return cu, true, nil
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
		cu.Password = nullString(password)
		cu.LoginURL = nullString(loginURL)

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

		// Apply fallback logic if needed
		if cu.ChanURLSettings == nil || cu.ChanURLMetarrArgs == nil {
			// Load channel if not cached or different channel
			if c == nil || c.ID != channelID {
				c, _, err = cs.GetChannelModel(consts.QChanID, strconv.FormatInt(channelID, 10))
				if err != nil {
					return nil, err
				}
			}

			// Apply settings fallback
			if cu.ChanURLSettings == nil {
				if c != nil && c.ChanSettings != nil {
					cu.ChanURLSettings = c.ChanSettings
				} else {
					cu.ChanURLSettings = &models.Settings{}
				}
			}

			// Apply metarr args fallback
			if cu.ChanURLMetarrArgs == nil {
				if c != nil && c.ChanMetarrArgs != nil {
					cu.ChanURLMetarrArgs = c.ChanMetarrArgs
				} else {
					cu.ChanURLMetarrArgs = &models.MetarrArgs{}
				}
			}
		}

		urlMap[channelID] = append(urlMap[channelID], cu)
	}
	return urlMap, nil
}
