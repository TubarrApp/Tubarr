// Package cfgflags handles Cobra/Viper commands.
package cfgflags

import (
	"tubarr/internal/domain/keys"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// InitProgramFlags initializes user flag settings related to the core program. E.g. logging level.
func InitProgramFlags(rootCmd *cobra.Command) error {

	// Output benchmarking files
	rootCmd.PersistentFlags().Bool(keys.Benchmarking, false, "Benchmarks the program")
	if err := viper.BindPFlag(keys.Benchmarking, rootCmd.PersistentFlags().Lookup(keys.Benchmarking)); err != nil {
		return err
	}

	// Cookies
	rootCmd.PersistentFlags().String(keys.CookieSource, "", "Cookie source for web operations (e.g. 'Firefox')")
	if err := viper.BindPFlag(keys.CookieSource, rootCmd.PersistentFlags().Lookup(keys.CookieSource)); err != nil {
		return err
	}

	// Debug level
	rootCmd.PersistentFlags().Int(keys.DebugLevel, 0, "Debugging level (0 - 5)")
	if err := viper.BindPFlag(keys.DebugLevel, rootCmd.PersistentFlags().Lookup(keys.DebugLevel)); err != nil {
		return err
	}
	return nil
}

// SetProgramRelatedFlags sets flags for the Tubarr instance.
func SetProgramRelatedFlags(cmd *cobra.Command, concurrency, crawlFreq *int, downloadArgs, downloadCmd *string, pause *bool, isUpdate bool) {

	if concurrency != nil {
		def := 0
		if !isUpdate {
			def = 1
		}
		cmd.Flags().IntVarP(concurrency, keys.Concurrency, "l", def, "Maximum concurrent videos to download/process for this instance")

	}

	if crawlFreq != nil {
		def := 0
		if !isUpdate {
			def = 30
		}
		cmd.Flags().IntVar(crawlFreq, keys.CrawlFreq, def, "New crawl frequency in minutes")
	}

	if downloadCmd != nil {
		cmd.Flags().StringVar(downloadCmd, keys.ExternalDownloader, "", "External downloader command")
	}

	if downloadArgs != nil {
		cmd.Flags().StringVar(downloadArgs, keys.ExternalDownloaderArgs, "", "External downloader arguments")
	}

	if pause != nil {
		cmd.Flags().BoolVar(pause, keys.Pause, false, "Pause/unpause this channel")
	}
}
