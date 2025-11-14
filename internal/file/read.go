// Package file contains utilities related to file operations (e.g. reading files).
package file

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"tubarr/internal/contracts"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
	"tubarr/internal/validation"

	"github.com/spf13/viper"
)

// UpdateFromConfigFile loads in config file data.
func UpdateFromConfigFile(cs contracts.ChannelStore, c *models.Channel) {

	if c.ConfigFile != "" && !c.UpdatedFromConfig {
		if err := cs.UpdateChannelFromConfig(c); err != nil {
			logging.E("failed to update from config file %q: %v", c.ConfigFile, err)
		}

		c.UpdatedFromConfig = true
	}
}

// LoadConfigFile loads in the preset configuration file.
func LoadConfigFile(v *viper.Viper, file string) error {
	if _, err := validation.ValidateFile(file, false); err != nil {
		return err
	}

	v.SetConfigFile(file)
	if err := v.ReadInConfig(); err != nil {
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

// ScanDirectoryForConfigFiles scans a directory for Viper-compatible config files.
// Returns a slice of absolute paths to valid config files.
func ScanDirectoryForConfigFiles(dirPath string) ([]string, error) {
	// Validate directory exists
	dirInfo, err := os.Stat(dirPath)
	if err != nil {
		return nil, err
	}
	if !dirInfo.IsDir() {
		return nil, os.ErrInvalid
	}

	// Read directory contents
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	// Viper supported extensions
	validExts := map[string]bool{
		".yaml":       true,
		".yml":        true,
		".toml":       true,
		".json":       true,
		".hcl":        true,
		".tfvars":     true,
		".properties": true,
		".props":      true,
		".prop":       true,
		".ini":        true,
	}

	var configFiles []string
	for _, entry := range entries {
		// Skip directories
		if entry.IsDir() {
			continue
		}

		// Check if file has valid extension
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if validExts[ext] {
			fullPath := filepath.Join(dirPath, entry.Name())
			configFiles = append(configFiles, fullPath)
		}
	}

	return configFiles, nil
}
