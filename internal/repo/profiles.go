package repo

import (
	"cmp"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/sharedconsts"
)

// profileColumns are the profiles table columns scanProfile reads, in its order.
var profileColumns = strings.Join([]string{
	consts.QProfileID,
	consts.QProfileName,
	consts.QProfileSettings,
	consts.QProfileMetarr,
	consts.QProfileCreatedAt,
	consts.QProfileUpdatedAt,
}, ", ")

// profileUser is a channel using a profile, or one of its URLs.
type profileUser struct {
	channelID int64
	channel   string
	url       string
}

// AddProfile adds a new profile, setting p.ID.
func (cs *ChannelStore) AddProfile(p *models.Profile) error {
	if err := cs.checkProfileName(p); err != nil {
		return err
	}
	p.ProfileSettings = cmp.Or(p.ProfileSettings, &models.Settings{})
	p.ProfileMetarrArgs = cmp.Or(p.ProfileMetarrArgs, &models.MetarrArgs{})

	// Validate complete models at DB write boundary.
	if err := validation.ValidateSettingsModel(p.ProfileSettings, true); err != nil {
		return fmt.Errorf("%w: invalid settings: %w", contracts.ErrInvalidProfile, err)
	}
	if err := validation.ValidateMetarrArgsModel(p.ProfileMetarrArgs, true); err != nil {
		return fmt.Errorf("%w: invalid Metarr settings: %w", contracts.ErrInvalidProfile, err)
	}
	if err := validation.ValidateMetarrArgsCombination(p.ProfileMetarrArgs); err != nil {
		return fmt.Errorf("%w: Metarr settings: %w", contracts.ErrInvalidProfile, err)
	}
	settingsJSON, metarrJSON, err := marshalProfile(p)
	if err != nil {
		return err
	}

	now := time.Now()
	query := fmt.Sprintf(
		"INSERT INTO %s (%s, %s, %s, %s, %s) VALUES (?, ?, ?, ?, ?)",
		consts.DBProfiles,
		consts.QProfileName,
		consts.QProfileSettings,
		consts.QProfileMetarr,
		consts.QProfileCreatedAt,
		consts.QProfileUpdatedAt,
	)
	result, err := cs.q.Exec(query, p.Name, settingsJSON, metarrJSON, now, now)
	if err != nil {
		return fmt.Errorf("failed to insert profile %q: %w", p.Name, err)
	}
	if p.ID, err = result.LastInsertId(); err != nil {
		return fmt.Errorf("failed to get last insert ID: %w", err)
	}
	p.CreatedAt = now
	p.UpdatedAt = now

	logger.Pl.S("Added profile %q (ID: %d)", p.Name, p.ID)
	return nil
}

// UpdateProfile saves a profile's name and settings. The channels using it are checked with the new settings.
func (cs *ChannelStore) UpdateProfile(p *models.Profile) error {
	return cs.inTx(func(tcs *ChannelStore) error { return tcs.updateProfile(p) })
}

// updateProfile saves a profile's name and settings.
func (cs *ChannelStore) updateProfile(p *models.Profile) error {
	old, found, err := cs.GetProfile(consts.QProfileID, strconv.FormatInt(p.ID, 10))
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: no profile with ID %d", contracts.ErrInvalidProfile, p.ID)
	}
	if err := cs.checkProfileName(p); err != nil {
		return err
	}
	p.ProfileSettings = cmp.Or(p.ProfileSettings, &models.Settings{})
	p.ProfileMetarrArgs = cmp.Or(p.ProfileMetarrArgs, &models.MetarrArgs{})

	// Validate at DB write boundary. Only files and folders the update adds or changes are checked.
	if err := validation.ValidateSettingsModel(p.ProfileSettings, false); err != nil {
		return fmt.Errorf("%w: invalid settings: %w", contracts.ErrInvalidProfile, err)
	}
	if err := validation.ValidateMetarrArgsModel(p.ProfileMetarrArgs, false); err != nil {
		return fmt.Errorf("%w: invalid Metarr settings: %w", contracts.ErrInvalidProfile, err)
	}
	if err := validation.ValidateMetarrArgsCombination(p.ProfileMetarrArgs); err != nil {
		return fmt.Errorf("%w: Metarr settings: %w", contracts.ErrInvalidProfile, err)
	}
	if err := checkFiles(settingsFiles(old.ProfileSettings, old.ProfileMetarrArgs), settingsFiles(p.ProfileSettings, p.ProfileMetarrArgs)); err != nil {
		return fmt.Errorf("%w: %w", contracts.ErrInvalidProfile, err)
	}
	if err := checkDownloadDirs(old.ProfileSettings, p.ProfileSettings); err != nil {
		return fmt.Errorf("%w: %w", contracts.ErrInvalidProfile, err)
	}
	if err := checkOutputDir(old.ProfileMetarrArgs, p.ProfileMetarrArgs); err != nil {
		return fmt.Errorf("%w: %w", contracts.ErrInvalidProfile, err)
	}

	// Channels using it, directly or through a URL, are checked once the transaction's changes are written.
	users, err := cs.profileUsers(p.ID)
	if err != nil {
		return err
	}
	for _, u := range users {
		cs.touch(u.channelID)
	}

	settingsJSON, metarrJSON, err := marshalProfile(p)
	if err != nil {
		return err
	}
	now := time.Now()
	query := fmt.Sprintf(
		"UPDATE %s SET %s = ?, %s = ?, %s = ?, %s = ? WHERE %s = ?",
		consts.DBProfiles,
		consts.QProfileName,
		consts.QProfileSettings,
		consts.QProfileMetarr,
		consts.QProfileUpdatedAt,
		consts.QProfileID,
	)
	if _, err := cs.q.Exec(query, p.Name, settingsJSON, metarrJSON, now, p.ID); err != nil {
		return fmt.Errorf("failed to update profile %q: %w", p.Name, err)
	}
	p.UpdatedAt = now

	logger.Pl.S("Updated profile %q (ID: %d)", p.Name, p.ID)
	return nil
}

// DeleteProfile deletes the profile whose key column (its ID or name) is val, unless a channel or channel URL uses it.
func (cs *ChannelStore) DeleteProfile(key, val string) error {
	return cs.inTx(func(tcs *ChannelStore) error {
		p, found, err := tcs.GetProfile(key, val)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("%w: no profile with %s %q", contracts.ErrInvalidProfile, key, val)
		}

		// Its users must choose another profile first.
		users, err := tcs.profileUsers(p.ID)
		if err != nil {
			return err
		}
		if len(users) > 0 {
			return fmt.Errorf("%w: profile %q is used by %s, choose another profile for them first", contracts.ErrInvalidProfile, p.Name, joinProfileUsers(users))
		}

		query := fmt.Sprintf("DELETE FROM %s WHERE %s = ?", consts.DBProfiles, consts.QProfileID)
		if _, err := tcs.q.Exec(query, p.ID); err != nil {
			return fmt.Errorf("failed to delete profile %q: %w", p.Name, err)
		}

		logger.Pl.S("Deleted profile %q", p.Name)
		return nil
	})
}

// GetProfile returns the profile whose key column (its ID or name) is val.
func (cs *ChannelStore) GetProfile(key, val string) (*models.Profile, bool, error) {
	if err := validKey(key, consts.ValidProfileKeys); err != nil {
		return nil, false, err
	}
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = ?", profileColumns, consts.DBProfiles, key)

	var p models.Profile
	err := scanProfile(cs.q.QueryRow(query, val), &p)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, fmt.Errorf("failed to read profile: %w", err)
	}
	return &p, true, nil
}

// GetAllProfiles returns every profile, by name.
func (cs *ChannelStore) GetAllProfiles() (profiles []*models.Profile, err error) {
	query := fmt.Sprintf("SELECT %s FROM %s ORDER BY %s", profileColumns, consts.DBProfiles, consts.QProfileName)
	rows, err := cs.q.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query profiles: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Pl.E("Failed to close rows: %v", err)
		}
	}()

	for rows.Next() {
		var p models.Profile
		if err := scanProfile(rows, &p); err != nil {
			return nil, fmt.Errorf("failed to read profile: %w", err)
		}
		profiles = append(profiles, &p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed while retrieving profiles: %w", err)
	}
	return profiles, nil
}

// GetProfileID returns the ID of the profile with the given name, or 0 for a blank name.
func (cs *ChannelStore) GetProfileID(name string) (int64, error) {
	if name == "" {
		return 0, nil
	}
	p, found, err := cs.GetProfile(consts.QProfileName, name)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, fmt.Errorf("%w: no profile named %q", contracts.ErrInvalidChannel, name)
	}
	return p.ID, nil
}

// DisplayProfile displays a profile's settings and what uses it.
func (cs *ChannelStore) DisplayProfile(p *models.Profile) {
	fmt.Printf("\n%s[ Profile: %q ]%s\n", sharedconsts.ColorGreen, p.Name, sharedconsts.ColorReset)
	if p.LoadErr != nil {
		fmt.Printf("%s%v%s\n", sharedconsts.ColorRed, p.LoadErr, sharedconsts.ColorReset)
	}

	users, err := cs.profileUsers(p.ID)
	if err != nil {
		logger.Pl.E("Unable to fetch the users of profile %q: %v", p.Name, err)
	}
	fmt.Printf("Used by: %s\n", cmp.Or(joinProfileUsers(users), "nothing"))

	fmt.Printf("\n%sSettings:%s\n", sharedconsts.ColorCyan, sharedconsts.ColorReset)
	displaySettingsStruct(p.ProfileSettings)

	fmt.Printf("\n%sMetarr Settings:%s\n", sharedconsts.ColorCyan, sharedconsts.ColorReset)
	displayMetarrArgsStruct(p.ProfileMetarrArgs)
	fmt.Println()
}

// SetChannelProfile sets the profile of the channel whose key column (its ID or name) is val, 0 for none.
func (cs *ChannelStore) SetChannelProfile(key, val string, profileID int64) error {
	return cs.inTx(func(tcs *ChannelStore) error {
		channelID, err := tcs.GetChannelID(key, val)
		if err != nil {
			return err
		}
		if err := tcs.checkProfile(profileID); err != nil {
			return err
		}
		tcs.touch(channelID)

		query := fmt.Sprintf("UPDATE %s SET %s = ? WHERE %s = ?", consts.DBChannels, consts.QChanProfileID, consts.QChanID)
		if _, err := tcs.q.Exec(query, profileID, channelID); err != nil {
			return fmt.Errorf("failed to set the profile of channel %d: %w", channelID, err)
		}

		logger.Pl.S("Set the profile of channel [%s=%s] to ID %d", key, val, profileID)
		return nil
	})
}

// ******************************** Private ***************************************************************************************

// scanProfile reads a row of profileColumns into p, marking it if its saved settings are invalid.
func scanProfile(row interface{ Scan(...any) error }, p *models.Profile) error {
	var settingsJSON, metarrJSON []byte
	if err := row.Scan(&p.ID, &p.Name, &settingsJSON, &metarrJSON, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return err
	}

	// Decode its settings.
	p.ProfileSettings = &models.Settings{}
	p.ProfileMetarrArgs = &models.MetarrArgs{}
	if len(settingsJSON) > 0 {
		if err := json.Unmarshal(settingsJSON, p.ProfileSettings); err != nil {
			return fmt.Errorf("failed to unmarshal settings of profile %q: %w", p.Name, err)
		}
	}
	if len(metarrJSON) > 0 {
		if err := json.Unmarshal(metarrJSON, p.ProfileMetarrArgs); err != nil {
			return fmt.Errorf("failed to unmarshal Metarr settings of profile %q: %w", p.Name, err)
		}
	}

	// Validate at the DB read boundary, without checking paths, as for a channel.
	var loadErr error
	if err := validation.ValidateSettingsModel(p.ProfileSettings, false); err != nil {
		loadErr = fmt.Errorf("invalid settings: %w", err)
	} else if err := validation.ValidateMetarrArgsModel(p.ProfileMetarrArgs, false); err != nil {
		loadErr = fmt.Errorf("invalid Metarr settings: %w", err)
	}
	if loadErr != nil {
		p.LoadErr = fmt.Errorf("profile %q has invalid saved settings, fix them on the Profiles page: %w", p.Name, loadErr)
	}
	return nil
}

// marshalProfile returns a profile's settings and Metarr args as JSON.
func marshalProfile(p *models.Profile) (settingsJSON, metarrJSON []byte, err error) {
	if settingsJSON, err = json.Marshal(p.ProfileSettings); err != nil {
		return nil, nil, fmt.Errorf("failed to marshal settings of profile %q: %w", p.Name, err)
	}
	if metarrJSON, err = json.Marshal(p.ProfileMetarrArgs); err != nil {
		return nil, nil, fmt.Errorf("failed to marshal Metarr settings of profile %q: %w", p.Name, err)
	}
	return settingsJSON, metarrJSON, nil
}

// checkProfileName returns an error if a profile has no name, or another profile has its name.
func (cs *ChannelStore) checkProfileName(p *models.Profile) error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("%w: profiles need a name", contracts.ErrInvalidProfile)
	}
	other, found, err := cs.GetProfile(consts.QProfileName, p.Name)
	if err != nil {
		return err
	}
	if found && other.ID != p.ID {
		return fmt.Errorf("%w: a profile named %q already exists", contracts.ErrInvalidProfile, p.Name)
	}
	return nil
}

// profileByID returns the profile with the given ID, or nil for 0 or a profile that no longer exists.
func (cs *ChannelStore) profileByID(id int64) (*models.Profile, error) {
	if id == 0 {
		return nil, nil
	}
	p, _, err := cs.GetProfile(consts.QProfileID, strconv.FormatInt(id, 10))
	return p, err
}

// checkProfile returns an error if a channel or URL is being saved with a profile that doesn't exist, or whose saved
// settings are invalid. An ID of 0 (no profile) passes.
func (cs *ChannelStore) checkProfile(id int64) error {
	p, err := cs.profileByID(id)
	switch {
	case err != nil:
		return err
	case id != 0 && p == nil:
		return fmt.Errorf("%w: no profile with ID %d", contracts.ErrInvalidChannel, id)
	case p != nil && p.LoadErr != nil:
		return fmt.Errorf("%w: %w", contracts.ErrInvalidChannel, p.LoadErr)
	}
	return nil
}

// profilesErr returns why a loaded channel's profile, or one of its URLs', can't be used: it no longer exists, or its
// saved settings are invalid.
func profilesErr(c *models.Channel) error {
	if c.ProfileID != 0 && c.Profile == nil {
		return fmt.Errorf("its profile (ID %d) no longer exists, choose another", c.ProfileID)
	}
	if c.Profile != nil && c.Profile.LoadErr != nil {
		return c.Profile.LoadErr
	}
	for _, cu := range c.URLModels {
		if cu.ProfileID != 0 && cu.Profile == nil {
			return fmt.Errorf("the profile of URL %q (ID %d) no longer exists, choose another", cu.URL, cu.ProfileID)
		}
		if cu.Profile != nil && cu.Profile.LoadErr != nil {
			return fmt.Errorf("URL %q: %w", cu.URL, cu.Profile.LoadErr)
		}
	}
	return nil
}

// profileUsers returns the channels using a profile, and the channel URLs that do, by channel name.
func (cs *ChannelStore) profileUsers(id int64) (users []profileUser, err error) {
	query := fmt.Sprintf(
		"SELECT %[1]s, %[2]s, '' FROM %[3]s WHERE %[4]s = ? "+
			"UNION ALL SELECT c.%[1]s, c.%[2]s, u.%[5]s FROM %[6]s u JOIN %[3]s c ON c.%[1]s = u.%[7]s WHERE u.%[8]s = ? "+
			"ORDER BY 2, 3",
		consts.QChanID,
		consts.QChanName,
		consts.DBChannels,
		consts.QChanProfileID,
		consts.QChanURLURL,
		consts.DBChannelURLs,
		consts.QChanURLChannelID,
		consts.QChanURLProfileID,
	)
	rows, err := cs.q.Query(query, id, id)
	if err != nil {
		return nil, fmt.Errorf("failed to query the users of profile %d: %w", id, err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Pl.E("Failed to close rows: %v", err)
		}
	}()

	for rows.Next() {
		var u profileUser
		if err := rows.Scan(&u.channelID, &u.channel, &u.url); err != nil {
			return nil, fmt.Errorf("failed to read a user of profile %d: %w", id, err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// profileName names the profile with the given ID, loaded as p, for display: blank for none.
func profileName(id int64, p *models.Profile) string {
	switch {
	case id == 0:
		return ""
	case p == nil:
		return fmt.Sprintf("(ID %d, which no longer exists)", id)
	}
	return p.Name
}

// joinProfileUsers names a profile's users in one line, for messages.
func joinProfileUsers(users []profileUser) string {
	names := make([]string, 0, len(users))
	for _, u := range users {
		if u.url == "" {
			names = append(names, fmt.Sprintf("channel %q", u.channel))
		} else {
			names = append(names, fmt.Sprintf("channel %q's URL %q", u.channel, u.url))
		}
	}
	return strings.Join(names, ", ")
}
