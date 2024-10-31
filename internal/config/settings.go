package config

import (
	keys "Tubarr/internal/domain/keys"
	logging "Tubarr/internal/utils/logging"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:   "metarr",
	Short: "Metarr is a video and metatagging tool",
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

	// Video directory
	rootCmd.PersistentFlags().StringP(keys.VideoDir, "v", ".", "Video directory")
	viper.BindPFlag(keys.VideoDir, rootCmd.PersistentFlags().Lookup(keys.VideoDir))

	// Metadata directory
	rootCmd.PersistentFlags().StringP(keys.MetaDir, "m", ".", "Metadata directory location")
	viper.BindPFlag(keys.MetaDir, rootCmd.PersistentFlags().Lookup(keys.MetaDir))

	// Channels to check
	rootCmd.PersistentFlags().StringP(keys.ChannelFile, "c", "", "File of channels to check for new videos")
	viper.BindPFlag(keys.ChannelFile, rootCmd.PersistentFlags().Lookup(keys.ChannelFile))

	// Cookie source
	rootCmd.PersistentFlags().String(keys.CookieSource, "", "Browser to grab cookies from for sites requiring authentication (e.g. firefox)")
	viper.BindPFlag(keys.CookieSource, rootCmd.PersistentFlags().Lookup(keys.CookieSource))

	// Metarr preset file
	rootCmd.PersistentFlags().String(keys.MetarrPreset, "", "Metarr preset file location")
	viper.BindPFlag(keys.MetarrPreset, rootCmd.PersistentFlags().Lookup(keys.MetarrPreset))

	rootCmd.PersistentFlags().String(keys.ExternalDownloader, "", "External downloader to use for yt-dlp (e.g. aria2c)")
	viper.BindPFlag(keys.ExternalDownloader, rootCmd.PersistentFlags().Lookup(keys.ExternalDownloader))

	rootCmd.PersistentFlags().String(keys.ExternalDownloaderArgs, "", "Arguments for external downloader (e.g. \"-x 16 -s 16\")")
	viper.BindPFlag(keys.ExternalDownloader, rootCmd.PersistentFlags().Lookup(keys.ExternalDownloader))
}

// Execute is the primary initializer of Viper
func Execute() error {

	fmt.Println()

	err := rootCmd.Execute()
	if err != nil {
		logging.PrintE(0, "Failed to execute cobra")
		return err

	}
	return nil
}

// execute more thoroughly handles settings created in the Viper init
func execute() error {
	if metarrPreset := viper.GetString(keys.MetarrPreset); metarrPreset != "" {

		if info, err := os.Stat(metarrPreset); err != nil {
			return fmt.Errorf("metarr preset does not exist")
		} else {
			if info.IsDir() {
				return fmt.Errorf("metarr preset must be a file")
			}
		}
	}
	channelFile := GetString(keys.ChannelFile)

	cFile, err := os.OpenFile(channelFile, os.O_RDWR, 0644)
	if err != nil {
		logging.PrintE(0, "Failed to open file '%s'", channelFile)
	}
	defer cFile.Close()

	content, err := os.ReadFile(channelFile)
	if err != nil {
		logging.PrintE(0, "Unable to read file '%s'", channelFile)
	}
	channelsCheckNew := strings.Split(string(content), "\n")
	viper.Set(keys.ChannelCheckNew, channelsCheckNew)

	if IsSet(keys.CookieSource) {
		cookieSource := GetString(keys.CookieSource)

		switch cookieSource {
		case "brave", "chrome", "edge", "firefox", "opera", "safari", "vivaldi", "whale":
			logging.PrintI("Using %s for cookies", cookieSource)
		default:
			return fmt.Errorf("invalid cookie source set. yt-dlp supports firefox, chrome, vivaldi, opera, edge, and brave")
		}
	}
	return nil
}
