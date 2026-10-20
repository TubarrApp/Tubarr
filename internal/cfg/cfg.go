// Package cfg provides configuration and command-line interface setup for Tubarr.
package cfg

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"tubarr/internal/app"
	"tubarr/internal/cmd"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/domain/vars"
	"tubarr/internal/downloads"
	"tubarr/internal/limits"
	"tubarr/internal/parsing"
	"tubarr/internal/siterules"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/benchmark"
	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// settingsStore is set during InitCommands and used to load persisted settings.
var settingsStore contracts.TubarrSettingsStore

// rootCmd is the base command for Tubarr.
var rootCmd = &cobra.Command{
	Use:           "tubarr",
	Short:         "Tubarr is a video downloading and metatagging tool.",
	SilenceErrors: true, // main logs the returned error.
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		cmd.SilenceUsage = true // Flags have parsed, so later errors aren't usage errors.

		// Load the saved log level, unless set by flag.
		if settingsStore != nil && !cmd.Flags().Changed(keys.DebugLevel) {
			if val, found, err := settingsStore.GetSetting(keys.DebugLevel); err != nil {
				logger.Pl.W("Failed to load setting %q from DB: %v", keys.DebugLevel, err)
			} else if n, err := strconv.Atoi(val); found && err == nil {
				viper.Set(keys.DebugLevel, n)
			}
		}

		if err := validation.ValidateViperFlags(); err != nil {
			return err
		}

		// Load persisted settings from DB, applying them only if the CLI flag was not explicitly set.
		if settingsStore != nil {
			intKeys := []string{keys.CrawlConcurrency, keys.GlobalDownloadConcurrency, keys.PostProcessConcurrency}
			for _, k := range intKeys {
				if !cmd.Flags().Changed(k) {
					if val, found, err := settingsStore.GetSetting(k); err != nil {
						logger.Pl.W("Failed to load setting %q from DB: %v", k, err)
					} else if found {
						if n, err := strconv.Atoi(val); err == nil {
							viper.Set(k, n)
						}
					}
				}
			}

			// Initialize domain download and crawl limits from DB.
			for k, limiter := range map[string]*limits.DomainLimiter{
				keys.DomainDownloadLimits: limits.DomainDownloads,
				keys.DomainCrawlLimits:    limits.DomainCrawls,
			} {
				if m, err := settingsStore.GetDomainLimitMap(k); err != nil {
					logger.Pl.W("Failed to load setting %q from DB: %v", k, err)
				} else {
					limiter.Init(m)
				}
			}

			// Scrape config file path and FlareSolverr URL.
			for _, k := range []string{keys.ScrapeConfigFile, keys.FlareSolverrURL} {
				if cmd.Flags().Changed(k) {
					if err := settingsStore.SetSetting(k, viper.GetString(k)); err != nil {
						logger.Pl.W("Failed to persist setting %q to DB: %v", k, err)
					}
				} else if val, found, err := settingsStore.GetSetting(k); err != nil {
					logger.Pl.W("Failed to load setting %q from DB: %v", k, err)
				} else if found {
					viper.Set(k, val)
				}
			}
		}

		// Initialize global download concurrency limit (0 = unlimited).
		downloads.InitGlobalDownloadLimit(viper.GetInt(keys.GlobalDownloadConcurrency))
		app.InitGlobalMetarrLimit(viper.GetInt(keys.PostProcessConcurrency))

		// Setup benchmarking if flag is set. A failure only skips benchmarking.
		if viper.GetBool(keys.Benchmarking) {
			if err := os.MkdirAll(paths.BenchmarkDir, sharedconsts.PermsGenericDir); err != nil {
				logger.Pl.W("Failed to make benchmark directory, not benchmarking: %v", err)
			} else if vars.BenchmarkFiles, err = benchmark.SetupBenchmarking(logger.Pl, paths.BenchmarkDir); err != nil {
				logger.Pl.W("Failed to set up benchmarking: %v", err)
			}
		}

		// Load custom scrape site selectors from a global config file, if provided.
		// Reset first so a cleared or changed path doesn't retain stale entries from a prior run.
		siterules.Reset()
		if scrapeConfigFile := viper.GetString(keys.ScrapeConfigFile); scrapeConfigFile != "" {
			if _, statErr := os.Stat(scrapeConfigFile); os.IsNotExist(statErr) {
				// File not found (e.g. on a drive not yet mounted). The setting is kept for when it's back.
				logger.Pl.W("Scrape config file %q doesn't exist, using built-in scrape rules", scrapeConfigFile)
			} else {
				sites, err := parsing.ParseScrapeSelectorsFile(scrapeConfigFile)
				if err != nil {
					return fmt.Errorf("failed loading scrape config file: %w", err)
				}
				siterules.Register(sites)
			}
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Lookup("help").Changed {
			return nil
		}
		if len(args) == 0 {
			viper.Set(keys.TerminalRunDefaultBehavior, true)
		}
		viper.Set("execute", true)
		return nil
	},
}

// InitCommands initializes all commands and their flags.
func InitCommands(ctx context.Context, s contracts.Store) error {
	settingsStore = s.TubarrSettingsStore()
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("_", "-")) // Convert jsonkeys.SettingsVideoDirectory to "video-directory".

	// Web interface flags (always registered, flags aren't parsed until Execute).
	rootCmd.PersistentFlags().Bool(keys.RunWebInterface, false, "Run Tubarr as a web interface")
	rootCmd.PersistentFlags().String(keys.WebAddress, "", "Address for the web interface to listen on (default 127.0.0.1:8827, or :8827 in Docker; env "+consts.EnvWebAddress+")")
	rootCmd.PersistentFlags().String(keys.WebAuth, "", "Web interface login, overriding the Settings page: 'required', 'local' (no login from localhost) or 'none', e.g. behind a reverse proxy with its own login (env "+consts.EnvWebAuth+")")
	rootCmd.PersistentFlags().Bool(keys.WebHTTPS, false, "Serve the web interface over HTTPS, overriding the Settings page (env "+consts.EnvWebHTTPS+")")
	rootCmd.PersistentFlags().String(keys.WebTLSCert, "", "HTTPS certificate path, PEM with or without the key, overriding the Settings page (env "+consts.EnvWebTLSCert+")")
	rootCmd.PersistentFlags().String(keys.WebTLSKey, "", "HTTPS private key path, if not in the --"+keys.WebTLSCert+" file (env "+consts.EnvWebTLSKey+")")
	if err := viper.BindPFlags(rootCmd.PersistentFlags()); err != nil {
		return err
	}

	// Only true here when set by env, skip CLI flags and commands.
	if viper.GetBool(keys.RunWebInterface) {
		return nil
	}

	// CLI flags.
	cmd.InitProgramFlags(rootCmd)
	if err := cmd.InitFileTransformers(rootCmd); err != nil {
		return err
	}
	if err := cmd.InitMetaTransformers(rootCmd); err != nil {
		return err
	}
	cmd.InitVideoTransformers(rootCmd)
	if err := viper.BindPFlags(rootCmd.PersistentFlags()); err != nil {
		return err
	}

	// Add Channel and Video function commands.
	rootCmd.AddCommand(InitChannelCmds(ctx, s))
	rootCmd.AddCommand(InitVideoCmds(s))
	rootCmd.AddCommand(InitAuthCmds(s))

	return nil
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {
	return rootCmd.Execute()
}
