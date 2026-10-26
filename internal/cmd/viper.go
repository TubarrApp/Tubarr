package cmd

import (
	"tubarr/internal/domain/keys"

	"github.com/spf13/cobra"
)

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

	// Debug level
	rootCmd.PersistentFlags().Int(keys.DebugLevel, -1, "Debugging level (0 - 5)")
}
