package cfg

import (
	"fmt"
	"strings"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/utils/logging"

	"github.com/spf13/viper"
)

// viperFlags verifies that the user input flags are valid, modifying them to defaults or returning bools/errors.
func viperFlags() error {
	if viper.IsSet(keys.OutputFiletype) {
		ext := viper.GetString(keys.OutputFiletype)
		if !verifyOutputFiletype(ext) {
			return fmt.Errorf("invalid output filetype %q", ext)
		}
	}

	if viper.IsSet(keys.MetaPurge) {
		purge := viper.GetString(keys.MetaPurge)
		if !verifyPurgeMetafiles(purge) {
			return fmt.Errorf("invalid meta purge type %q", purge)
		}
	}

	verifyLoggingLevel()
	verifyConcurrencyLimit()
	return nil
}

// verifyConcurrencyLimit checks and ensures correct concurrency limit input.
func verifyConcurrencyLimit() {
	maxConcurrentProcesses := viper.GetInt(keys.Concurrency)

	switch {
	case maxConcurrentProcesses < 1:
		maxConcurrentProcesses = 1
	default:
		fmt.Printf("Max concurrency: %d", maxConcurrentProcesses)
	}
	viper.Set(keys.Concurrency, maxConcurrentProcesses)
}

// verifyOutputFiletype verifies the output filetype is valid for FFmpeg.
func verifyOutputFiletype(o string) bool {
	o = strings.TrimSpace(o)

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

// verifyPurgeMetafiles checks and sets the type of metafile purge to perform.
func verifyPurgeMetafiles(purgeType string) bool {

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

// verifyLoggingLevel checks and validates the debug level.
func verifyLoggingLevel() {
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
