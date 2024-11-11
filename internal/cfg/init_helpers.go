package cfg

import (
	keys "tubarr/internal/domain/keys"

	"github.com/spf13/viper"
)

// initFilesDirs initializes files and directories into Viper
func initFilesDirs() {
	// Video directory
	rootCmd.PersistentFlags().StringP(keys.VideoDir, "v", ".", "Video directory")
	viper.BindPFlag(keys.VideoDir, rootCmd.PersistentFlags().Lookup(keys.VideoDir))

	// Metadata directory
	rootCmd.PersistentFlags().StringP(keys.JsonDir, "j", ".", "Json directory location")
	viper.BindPFlag(keys.JsonDir, rootCmd.PersistentFlags().Lookup(keys.JsonDir))

	// Channels to check
	rootCmd.PersistentFlags().StringP(keys.ChannelFile, "c", "", "File of channels to check for new videos")
	viper.BindPFlag(keys.ChannelFile, rootCmd.PersistentFlags().Lookup(keys.ChannelFile))

	// Metarr preset file
	rootCmd.PersistentFlags().String(keys.MetarrPreset, "", "Metarr preset file location")
	viper.BindPFlag(keys.MetarrPreset, rootCmd.PersistentFlags().Lookup(keys.MetarrPreset))

	// Output filetype
	rootCmd.PersistentFlags().StringP(keys.MoveOnComplete, "o", "", "Location to move file to upon completion")
	viper.BindPFlag(keys.MoveOnComplete, rootCmd.PersistentFlags().Lookup(keys.MoveOnComplete))

	// Filetype to output
	rootCmd.PersistentFlags().String(keys.OutputFiletype, "", "Filetype to output as")
	viper.BindPFlag(keys.OutputFiletype, rootCmd.PersistentFlags().Lookup(keys.OutputFiletype))
}

// initDownloaderSettings initializes downloader settings
func initDownloaderSettings() {
	// Cookie source
	rootCmd.PersistentFlags().String(keys.CookieSource, "", "Browser to grab cookies from for sites requiring authentication (e.g. firefox)")
	viper.BindPFlag(keys.CookieSource, rootCmd.PersistentFlags().Lookup(keys.CookieSource))

	rootCmd.PersistentFlags().String(keys.CookiePath, "", "Specify a custom cookie file to use")
	viper.BindPFlag(keys.CookiePath, rootCmd.PersistentFlags().Lookup(keys.CookiePath))

	// External downloader (e.g. aria2c)
	rootCmd.PersistentFlags().String(keys.ExternalDownloader, "", "External downloader to use for yt-dlp (e.g. aria2c)")
	viper.BindPFlag(keys.ExternalDownloader, rootCmd.PersistentFlags().Lookup(keys.ExternalDownloader))

	rootCmd.PersistentFlags().String(keys.ExternalDownloaderArgs, "", "Arguments for external downloader (e.g. \"-x 16 -s 16\")")
	viper.BindPFlag(keys.ExternalDownloader, rootCmd.PersistentFlags().Lookup(keys.ExternalDownloader))
}

func initDLSettings() {
	// Filtering
	rootCmd.PersistentFlags().String(keys.FilterOpsInput, "", "Filters in or out downloads based on metadata (e.g. 'title:omit:frogs','title:contains:lions')")
	viper.BindPFlag(keys.FilterOpsInput, rootCmd.PersistentFlags().Lookup(keys.FilterOpsInput))

	// Concurrency
	rootCmd.PersistentFlags().IntP(keys.ConcurrencyLimitInput, "l", 3, "Concurrency limit, too high might cause rate limiting")
	viper.BindPFlag(keys.ConcurrencyLimitInput, rootCmd.PersistentFlags().Lookup(keys.ConcurrencyLimitInput))
}

// initProgramSettings initializes program settings into Viper
func initProgramSettings() {
	// Debugging
	rootCmd.PersistentFlags().IntP(keys.DebugLevel, "d", 0, "Set the logging level")
	viper.BindPFlag(keys.DebugLevel, rootCmd.PersistentFlags().Lookup(keys.DebugLevel))
}
