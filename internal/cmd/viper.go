package cmd

import (
	"fmt"
	"tubarr/internal/domain/keys"

	"github.com/spf13/cobra"
)

// Transformers ----------------------------------------------------------------------------------------------------------

// InitFileTransformers initializes user flag settings for transformations applying to all files.
func InitFileTransformers(rootCmd *cobra.Command) error {
	// Prefix file with metafield (no longer used)
	rootCmd.PersistentFlags().StringSlice(keys.MFilenamePfx, nil, "Adds a specified metatag's value onto the start of the filename")
	if err := rootCmd.PersistentFlags().MarkDeprecated(keys.MFilenamePfx, fmt.Sprintf("use a channel filename op, e.g. --%s 'set:{{artist}} - {{fulltitle}}'", keys.MFilenameOps)); err != nil {
		return err
	}

	// Rename convention
	rootCmd.PersistentFlags().StringP(keys.MRenameStyle, "r", "skip", "Rename flag (spaces, underscores, fixes-only, or skip)")

	// Output directory (can be external)
	rootCmd.PersistentFlags().StringP(keys.MoveOnComplete, "o", "", "Move files to given directory on program completion")

	// Purge metafile (no longer used)
	rootCmd.PersistentFlags().String(keys.PurgeMetaFile, "", "Purge metadata files upon completion.")
	return rootCmd.PersistentFlags().MarkDeprecated(keys.PurgeMetaFile, fmt.Sprintf("use --%s (all, json or nfo)", keys.MMetaPurge))
}

// InitVideoTransformers initializes user flag settings for transformation of video files.
func InitVideoTransformers(rootCmd *cobra.Command) {
	// Output extension type
	rootCmd.PersistentFlags().String(keys.OutputFiletype, "", "File extension to output files as (mp4 works best for most media servers)")
}

// InitMetaTransformers initializes user flag settings for manipulation of metadata.
func InitMetaTransformers(rootCmd *cobra.Command) error {
	// Metadata transformations
	rootCmd.PersistentFlags().StringArray(keys.MMetaOps, nil, "Metadata operation (field:operation:value) - e.g. 'title:set:New Title'. Repeat the flag for each operation.")

	// Prefix or append description fields with dates (no longer used)
	rootCmd.PersistentFlags().Bool(keys.MDescDatePfx, false, "Adds the date to the start of the description field.")
	if err := rootCmd.PersistentFlags().MarkDeprecated(keys.MDescDatePfx, fmt.Sprintf("use --%s 'description:date-tag:prefix:ymd'", keys.MMetaOps)); err != nil {
		return err
	}

	rootCmd.PersistentFlags().Bool(keys.MDescDateSfx, false, "Adds the date to the end of the description field.")
	if err := rootCmd.PersistentFlags().MarkDeprecated(keys.MDescDateSfx, fmt.Sprintf("use --%s 'description:date-tag:suffix:ymd'", keys.MMetaOps)); err != nil {
		return err
	}

	rootCmd.PersistentFlags().String(keys.MMetaPurge, "", "Metadata files Metarr deletes after processing a video, for channels without their own setting: all, json or nfo")
	return nil
}

// Program ----------------------------------------------------------------------------------------------------------

// InitProgramFlags initializes user flag settings related to the core program. E.g. logging level.
func InitProgramFlags(rootCmd *cobra.Command) {
	// Skip initial wait
	rootCmd.PersistentFlags().BoolP(keys.SkipInitialWait, "s", false, "Skip the wait period usually applied before a crawl (helps avoid bot detection)")

	// Skip wait
	rootCmd.PersistentFlags().Bool(keys.SkipAllWaits, false, "Skip all wait periods usually applied before a crawl (helps avoid bot detection)")

	// Output benchmarking files
	rootCmd.PersistentFlags().Bool(keys.Benchmarking, false, "Benchmarks the program")

	// Global Concurrency
	rootCmd.PersistentFlags().StringP(keys.CrawlConcurrency, "c", "", "Concurrency for this instance of Tubarr")

	// Global Download Concurrency
	rootCmd.PersistentFlags().Int(keys.GlobalDownloadConcurrency, 0, "Maximum concurrent video downloads across all channels (0 = unlimited)")

	// Global Post-processing Concurrency
	rootCmd.PersistentFlags().Int(keys.PostProcessConcurrency, 0, "Maximum concurrent Metarr post-processing runs across all channels (0 = unlimited)")

	// Custom scrape site config file (global, applies to all channels)
	rootCmd.PersistentFlags().String(keys.ScrapeConfigFile, "", "Path to a config file (yaml/toml/json) defining custom scraping selectors for additional sites")

	// FlareSolverr instance for scrape sites set to use it (global, applies to all channels)
	rootCmd.PersistentFlags().String(keys.FlareSolverrURL, "", "URL of a FlareSolverr instance (e.g. http://localhost:8191) for scrape sites set to use it")

	// Cookies
	rootCmd.PersistentFlags().String(keys.CookiesFromBrowser, "", "Cookie source for web operations (e.g. 'Firefox')")

	// Debug level
	rootCmd.PersistentFlags().Int(keys.DebugLevel, -1, "Debugging level (0 - 5)")
}
