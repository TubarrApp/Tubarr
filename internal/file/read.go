// Package file contains utilities related to file operations (e.g. reading files).
package file

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/domain/vars"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedfiles"
	"github.com/TubarrApp/gocommon/sharedtemplates"
	"github.com/TubarrApp/gocommon/sharedvalidation"
	"github.com/spf13/viper"
)

// Info returns FileInfo for a given filepath.
func Info(f string) (fInfo os.FileInfo, err error) {
	if fInfo, err = os.Stat(f); err != nil {
		return nil, fmt.Errorf("could not grab file info for path %q: %w", f, err)
	}
	return fInfo, nil
}

// Size returns size in bytes for a given filepath.
func Size(f string) (s int64) {
	fInfo, err := Info(f)
	if err != nil {
		logger.Pl.E("Failed to grab file size: %v", err)
		return 0
	}
	return fInfo.Size()
}

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
	return readLines(path, strings.TrimSpace)
}

// ReadOpLines is ReadFileLines for operation files, keeping trailing spaces since an
// operation's value may end in one.
func ReadOpLines(path string) ([]string, error) {
	return readLines(path, TrimOpLine)
}

// TrimOpLine strips leading whitespace and a trailing '\r', keeping trailing spaces.
func TrimOpLine(s string) string {
	return strings.TrimLeft(strings.TrimSuffix(s, "\r"), " \t")
}

// readLines loads non-blank, non-comment lines from a file, tidied by clean.
func readLines(path string, clean func(string) string) ([]string, error) {
	// Open file at path.
	f, err := sharedfiles.OpenRegularInRoot(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := f.Close(); err != nil {
			logger.Pl.E("failed to close file %v due to error: %v", path, err)
		}
	}()

	// Initialize slice and scanner.
	fLines := []string{}
	scanner := bufio.NewScanner(f)

	// Scan lines, ignore comment lines (defined by a line beginning with '#').
	for scanner.Scan() {
		line := clean(scanner.Text())
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue // skip blank lines and comments.
		}
		fLines = append(fLines, line)
	}

	// Check scanner errors.
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return fLines, nil
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

	var configFiles []string
	for _, entry := range entries {
		// Skip directories.
		if entry.IsDir() {
			logger.Pl.D(5, "Skipping entry %q in directory %q during config file scan: is directory", entry.Name(), dirPath)
			continue
		}

		// Check if file has valid extension.
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if _, exists := consts.ViperValidConfigExts[ext]; exists {
			fullPath := filepath.Join(dirPath, entry.Name())
			configFiles = append(configFiles, fullPath)
		}
	}

	return configFiles, nil
}

// LoadMetarrLogs loads in Metarr logs to a [][]byte.
func LoadMetarrLogs() [][]byte {
	// Open file at path.
	f, err := sharedfiles.OpenRegularInRoot(paths.MetarrLogFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		logger.Pl.E("Failed to open Metarr log file at %q: %v", paths.MetarrLogFilePath, err)
		return nil
	}
	defer func() {
		if err := f.Close(); err != nil {
			logger.Pl.E("failed to close file %v due to error: %v", paths.MetarrLogFilePath, err)
		}
	}()

	// Initialize slice and scanner.
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
