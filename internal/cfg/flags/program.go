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
	rootCmd.PersistentFlags().String(keys.TubarrCookieSource, "", "Cookie source for web operations (e.g. 'Firefox')")
	if err := viper.BindPFlag(keys.TubarrCookieSource, rootCmd.PersistentFlags().Lookup(keys.TubarrCookieSource)); err != nil {
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
func SetProgramRelatedFlags(cmd *cobra.Command, concurrency, crawlFreq *int, downloadArgs, downloadCmd *string, isUpdate bool) {
	if concurrency != nil {
		def := 0
		if !isUpdate {
			def = 3
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
		def := ""
		if !isUpdate {
			def = ""
		}
		cmd.Flags().StringVar(downloadCmd, keys.ExternalDownloader, def, "External downloader command")
	}
	if downloadArgs != nil {
		def := ""
		if !isUpdate {
			def = ""
		}
		cmd.Flags().StringVar(downloadArgs, keys.ExternalDownloaderArgs, def, "External downloader arguments")
	}
}
