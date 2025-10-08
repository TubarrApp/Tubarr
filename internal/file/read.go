// Package file contains utilities related to file operations (e.g. reading files).
package file

import (
	"bufio"
	"os"
	"strings"
	"tubarr/internal/contracts"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
	"tubarr/internal/validation"

	"github.com/spf13/viper"
)

// UpdateFromConfigFile loads in config file data.
func UpdateFromConfigFile(cs contracts.ChannelStore, c *models.Channel) {

	if c.ChanSettings.ChannelConfigFile != "" && !c.UpdatedFromConfig {
		if err := cs.UpdateChannelFromConfig(c); err != nil {
			logging.E("failed to update from config file %q: %v", c.ChanSettings.ChannelConfigFile, err)
		}

		c.UpdatedFromConfig = true
	}
}

// LoadConfigFile loads in the preset configuration file.
func LoadConfigFile(file string) error {
	if _, err := validation.ValidateFile(file, false); err != nil {
		return err
	}

	viper.SetConfigFile(file)
	if err := viper.ReadInConfig(); err != nil {
		return err
	}

	return nil
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
