// Package validation handles validation of user flag input.
package validation

import (
	"fmt"
	"os"
	"strings"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/utils/logging"

	"github.com/spf13/viper"
)

// ValidateDirectory validates that the directory exists, else creates it.
func ValidateDirectory(d string) error {

	logging.D(3, "Statting directory %q...", d)
	dirInfo, err := os.Stat(d)
	if err != nil {
		if os.IsNotExist(err) {
			logging.D(3, "Directory %q does not exist, creating it...", d)
			if err := os.MkdirAll(d, 0o755); err != nil {
				return fmt.Errorf("directory %q did not exist and Tubarr failed to create it: %w", d, err)
			}
		} else {
			return fmt.Errorf("failed to stat directory: %w", err)
		}
	}
	if !dirInfo.IsDir() {
		return fmt.Errorf("directory entered %q is a file", d)
	}

	return nil
}

// ValidateViperFlags verifies that the user input flags are valid, modifying them to defaults or returning bools/errors.
func ValidateViperFlags() error {
	if viper.IsSet(keys.OutputFiletype) {
		ext := strings.ToLower(viper.GetString(keys.OutputFiletype))
		if !ValidateOutputFiletype(ext) {
			return fmt.Errorf("invalid output filetype %q", ext)
		}
	}

	if viper.IsSet(keys.MetaPurge) {
		purge := viper.GetString(keys.MetaPurge)
		if !ValidatePurgeMetafiles(purge) {
			return fmt.Errorf("invalid meta purge type %q", purge)
		}
	}

	ValidateLoggingLevel()
	ValidateConcurrencyLimit()
	return nil
}

// ValidateConcurrencyLimit checks and ensures correct concurrency limit input.
func ValidateConcurrencyLimit() {
	maxConcurrentProcesses := viper.GetInt(keys.Concurrency)

	switch {
	case maxConcurrentProcesses < 1:
		maxConcurrentProcesses = 1
	default:
		fmt.Printf("Max concurrency: %d", maxConcurrentProcesses)
	}
	viper.Set(keys.Concurrency, maxConcurrentProcesses)
}

// ValidateNotificationPairs verifies that the notification pairs entered are valid.
func ValidateNotificationPairs(pairs []string) ([]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}

	for i, p := range pairs {

		if !strings.ContainsRune(p, '|') {
			return nil, fmt.Errorf("notification entry %q does not contain a '|' separator (should be in 'URL|friendly name' format", p)
		}

		entry := strings.Split(p, "|")

		switch {
		case len(entry) > 2:
			return nil, fmt.Errorf("too many entries for %q, should be in 'URL|friendly name' format", p)
		case entry[0] == "":
			return nil, fmt.Errorf("missing URL from notification entry %q, should be in 'URL|friendly name' format", p)
		}

		if entry[1] == "" {
			entry[1] = entry[0] // Use URL as name if name field is missing
		}

		entry[0] = strings.ReplaceAll(entry[0], `'`, ``)
		entry[0] = strings.ReplaceAll(entry[0], `"`, ``)
		entry[1] = strings.ReplaceAll(entry[1], `'`, ``)
		entry[1] = strings.ReplaceAll(entry[1], `"`, ``)

		pairs[i] = (entry[0] + "|" + entry[1])

		logging.D(2, "Made notification pair: %v", pairs[i])
	}

	return pairs, nil
}

// ValidateOutputFiletype verifies the output filetype is valid for FFmpeg.
func ValidateOutputFiletype(o string) bool {
	o = strings.ToLower(strings.TrimSpace(o))

	if !strings.HasPrefix(o, ".") {
		o = "." + o
		viper.Set(keys.OutputFiletype, o)
	}
	fmt.Printf("Output filetype: %s\n", o)

	valid := false
	for _, ext := range consts.AllVidExtensions {
		if o != ext {
			continue
		} else {
			valid = true
			break
		}
	}

	if valid {
		logging.I("Outputting files as %s", o)
		return true
	}
	return false
}

// ValidatePurgeMetafiles checks and sets the type of metafile purge to perform.
func ValidatePurgeMetafiles(purgeType string) bool {

	purgeType = strings.TrimSpace(purgeType)
	purgeType = strings.ToLower(purgeType)
	purgeType = strings.ReplaceAll(purgeType, ".", "")

	switch purgeType {
	case "all", "json", "nfo":
		fmt.Printf("Purge metafiles post-Metarr: %s\n", purgeType)
		return true
	}
	return false
}

// ValidateLoggingLevel checks and validates the debug level.
func ValidateLoggingLevel() {
	l := viper.GetInt(keys.DebugLevel)
	if l < 0 {
		l = 0
	}

	if l > 5 {
		l = 5
	}

	logging.Level = l
	fmt.Printf("Logging level: %d\n", logging.Level)
}
