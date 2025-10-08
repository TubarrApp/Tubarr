// Package file contains utilities related to file operations (e.g. reading files).
package file

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
	"tubarr/internal/validation"
)

// UpdateFromConfigFile loads in config file data.
func UpdateFromConfigFile(cs contracts.ChannelStore, c *models.Channel) {

	if c.ChanSettings.ChannelConfigFile != "" && !c.UpdatedFromConfig {
		if err := updateChannelFromConfig(cs, c); err != nil {
			logging.E("failed to update from config file %q: %v", c.ChanSettings.ChannelConfigFile, err)
		}

		c.UpdatedFromConfig = true
	}
}

// ReadFileLines loads lines from a file (one per line, ignoring '#' comment lines).
func ReadFileLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			logging.E("failed to close file %v due to error: %v", path, err)
		}
	}()

	f := []string{}
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue // skip blank lines and comments
		}
		f = append(f, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return f, nil
}

// updateChannelFromConfig updates the channel settings from a config file if it exists.
func updateChannelFromConfig(cs contracts.ChannelStore, c *models.Channel) (err error) {
	cfgFile := c.ChanSettings.ChannelConfigFile
	if cfgFile == "" {
		logging.D(2, "No config file path, nothing to apply")
		return nil
	}

	logging.I("Updating channel from config file %q...", cfgFile)
	if _, err := validation.ValidateFile(cfgFile, false); err != nil {
		return err
	}

	if err := LoadConfigFile(cfgFile); err != nil {
		return err
	}

	if err := cs.ApplyConfigChannelSettings(c); err != nil {
		return err
	}

	if err := cs.ApplyConfigMetarrSettings(c); err != nil {
		return err
	}

	_, err = cs.UpdateChannelSettingsJSON(consts.QChanID, strconv.FormatInt(c.ID, 10), func(s *models.ChannelSettings) error {
		if c.ChanSettings == nil {
			return fmt.Errorf("c.ChanSettings is nil")
		}
		*s = *c.ChanSettings
		return nil
	})
	if err != nil {
		return err
	}

	_, err = cs.UpdateChannelMetarrArgsJSON(consts.QChanID, strconv.FormatInt(c.ID, 10), func(m *models.MetarrArgs) error {
		if c.ChanMetarrArgs == nil {
			return fmt.Errorf("c.ChanMetarrArgs is nil")
		}
		*m = *c.ChanMetarrArgs
		return nil
	})
	if err != nil {
		return err
	}

	logging.S(0, "Updated channel %q from config file", c.Name)
	return nil
}
