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

	// Debug level
	rootCmd.PersistentFlags().Int(keys.DebugLevel, 0, "Debugging level (0 - 5)")
	if err := viper.BindPFlag(keys.DebugLevel, rootCmd.PersistentFlags().Lookup(keys.DebugLevel)); err != nil {
		return err
	}
	return nil
}

// SetProgramRelatedFlags sets flags for the Tubarr instance.
func SetProgramRelatedFlags(cmd *cobra.Command, concurrency, crawlFreq *int, downloadArgs, downloadCmd *string) {
	if concurrency != nil {
		cmd.Flags().IntVarP(concurrency, keys.Concurrency, "l", 0, "Maximum concurrent videos to download/process for this instance")
	}
	if crawlFreq != nil {
		cmd.Flags().IntVar(crawlFreq, keys.CrawlFreq, 30, "New crawl frequency in minutes")
	}
	if downloadCmd != nil {
		cmd.Flags().StringVar(downloadCmd, "downloader", "", "External downloader command")
	}
	if downloadArgs != nil {
		cmd.Flags().StringVar(downloadArgs, "downloader-args", "", "External downloader arguments")
	}
}
