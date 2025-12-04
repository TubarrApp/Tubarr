// Package file contains utilities related to file operations (e.g. reading files).
package file

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/domain/vars"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedtemplates"
	"github.com/TubarrApp/gocommon/sharedvalidation"
	"github.com/spf13/viper"
)

// UpdateFromConfigFile loads in config file data.
func UpdateFromConfigFile(cs contracts.ChannelStore, c *models.Channel) {

	if c.ChannelConfigFile != "" && !c.UpdatedFromConfig {
		if err := cs.UpdateChannelFromConfig(c); err != nil {
			logger.Pl.E("failed to update from config file %q: %v", c.ChannelConfigFile, err)
		}

		c.UpdatedFromConfig = true
	}
}

// LoadConfigFile loads in the preset configuration file.
func LoadConfigFile(v *viper.Viper, file string) error {
	if _, _, err := sharedvalidation.ValidateFile(file, false, sharedtemplates.NoTemplateTags); err != nil {
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
			logger.Pl.E("failed to close file %v due to error: %v", path, err)
		}
	}()

	f := []string{}
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue // skip blank lines and comments.
		}
		f = append(f, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return f, nil
}

// ScanDirectoryForConfigFiles scans a directory for Viper-compatible config files.
//
// Returns a slice of absolute paths to valid config files.
func ScanDirectoryForConfigFiles(dirPath string) ([]string, error) {
	// Validate directory exists.
	dirInfo, err := os.Stat(dirPath)
	if err != nil {
		return nil, err
	}
	if !dirInfo.IsDir() {
		return nil, os.ErrInvalid
	}

	// Read directory contents.
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	// Viper supported extensions.
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
		// Skip directories.
		if entry.IsDir() {
			continue
		}

		// Check if file has valid extension.
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if validExts[ext] {
			fullPath := filepath.Join(dirPath, entry.Name())
			configFiles = append(configFiles, fullPath)
		}
	}

	return configFiles, nil
}

// LoadMetarrLogs loads in Metarr logs to a [][]byte.
func LoadMetarrLogs() [][]byte {
	f, err := os.Open(paths.MetarrLogFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		logger.Pl.E("Failed to open Metarr log file at %q: %v", paths.MetarrLogFilePath, err)
		return nil
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			logger.Pl.E("Could not close file %q: %v", f, err)
		}
	}()

	var lines [][]byte

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		raw := scanner.Bytes()
		line := make([]byte, len(raw)+1)
		copy(line, raw)
		line[len(raw)] = '\n'
		lines = append(lines, line)
	}

	// Enforce log limit - keep only the most recent MaxMetarrLogs entries.
	if len(lines) > vars.MaxMetarrLogs {
		lines = lines[len(lines)-vars.MaxMetarrLogs:]
	}

	return lines
}
