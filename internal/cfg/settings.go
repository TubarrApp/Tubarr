package cfg

import (
	"fmt"
	"os"
	"strings"
	consts "tubarr/internal/domain/constants"
	keys "tubarr/internal/domain/keys"
	"tubarr/internal/models"
	logging "tubarr/internal/utils/logging"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:   "tubarr",
	Short: "Tubarr is a video and metatagging tool",
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Lookup("help").Changed {
			return nil // Stop further execution if help is invoked
		}
		viper.Set("execute", true)
		return execute()
	},
}

// init sets the initial Viper settings
func init() {

	// Files and directories
	initFilesDirs()

	// Download settings
	initDownloaderSettings()

	// Settings for actual downloads (e.g. filtering out videos from downloading)
	initDLSettings()

	// Program settings like debug level etc.
	initProgramSettings()
}

// Execute is the primary initializer of Viper
func Execute() error {

	fmt.Println()

	err := rootCmd.Execute()
	if err != nil {
		logging.E(0, "Failed to execute cobra")
		return err

	}
	return nil
}

// execute more thoroughly handles settings created in the Viper init
func execute() error {
	if viper.IsSet(keys.MetarrPreset) {
		if metarrPreset := viper.GetString(keys.MetarrPreset); metarrPreset != "" {

			if info, err := os.Stat(metarrPreset); err != nil {
				return fmt.Errorf("metarr preset does not exist")
			} else {
				if info.IsDir() {
					return fmt.Errorf("metarr preset must be a file")
				}
			}
		}
	}

	if err := verifyAndReadChannelFile(); err != nil {
		return err
	}

	if err := verifyCookieSource(); err != nil {
		return err
	}

	verifyOutputFiletype()

	verifyLogLevel()

	verifyConcurrencyLimit()

	if err := verifyFilterOps(); err != nil {
		return err
	}

	return nil
}

// verifyCookieSource verifies the cookie source is valid for yt-dlp
func verifyCookieSource() error {
	if IsSet(keys.CookieSource) {
		cookieSource := GetString(keys.CookieSource)

		switch cookieSource {
		case "brave", "chrome", "edge", "firefox", "opera", "safari", "vivaldi", "whale":
			logging.I("Using %s for cookies", cookieSource)
		default:
			return fmt.Errorf("invalid cookie source set. yt-dlp supports firefox, chrome, vivaldi, opera, edge, and brave")
		}
	}
	return nil
}

// verifyChannelFile verifies the channel file is valid
func verifyAndReadChannelFile() error {
	if viper.IsSet(keys.ChannelFile) {
		channelFile := GetString(keys.ChannelFile)

		cFile, err := os.OpenFile(channelFile, os.O_RDWR, 0644)
		if err != nil {
			return fmt.Errorf("failed to open file '%s'", channelFile)
		}
		defer cFile.Close()

		content, err := os.ReadFile(channelFile)
		if err != nil {
			logging.E(0, "Unable to read file '%s'", channelFile)
		}
		channelsCheckNew := strings.Split(string(content), "\n")
		viper.Set(keys.ChannelCheckNew, channelsCheckNew)
		return nil
	}
	return fmt.Errorf("please set the file of URLs for Tubarr to download from")
}

// verifyOutputFiletype verifies the output filetype
func verifyOutputFiletype() {
	if viper.IsSet(keys.OutputFiletype) {
		o := GetString(keys.OutputFiletype)
		if !strings.HasPrefix(o, ".") {
			o = "." + o
			viper.Set(keys.OutputFiletype, o)
			viper.Set(keys.OutputFiletype, o)
		}

		for _, ext := range consts.AllVidExtensions {
			if o == ext {
				logging.I("Outputting files as %s", o)
				return // Must return on match or overwrites with ""
			}
		}
		viper.Set(keys.OutputFiletype, "")
	}
}

// verifyLogLevel verifies logging level settings, saves into logging.Level var
func verifyLogLevel() {
	dLevel := GetInt(keys.DebugLevel)
	switch {
	case dLevel <= 0:
		logging.Level = 0
	case dLevel >= 5:
		logging.Level = 5
	default:
		logging.Level = dLevel
	}
}

// verifyFilterOps verifies the user input filters
func verifyFilterOps() error {
	if viper.IsSet(keys.FilterOpsInput) {

		filters := viper.GetStringSlice(keys.FilterOpsInput)
		var dlFilters = make([]*models.DLFilter, 0, len(filters))

		for _, filter := range filters {
			pair := strings.Split(filter, ":")
			if len(pair) != 2 {
				return fmt.Errorf("please enter filters in format 'field:contains_text'")
			}

			logging.D(1, "Filtering out field '%s' if it contains the value '%s'", pair[0], pair[1])
			dlFilters = append(dlFilters, &models.DLFilter{
				Field: pair[0],
				Omit:  pair[1],
			})
		}
		if len(dlFilters) > 0 {
			viper.Set(keys.FilterOps, dlFilters)
		}
	}
	return nil
}

// verifyConcurrencyLimit verifies the user inputted concurrency limitations
func verifyConcurrencyLimit() {
	lim := viper.GetInt(keys.ConcurrencyLimitInput)

	switch {
	case lim < 1:
		viper.Set(keys.Concurrency, 1)
	case lim > 25:
		viper.Set(keys.Concurrency, 25)
	default:
		viper.Set(keys.Concurrency, lim)
	}

}
