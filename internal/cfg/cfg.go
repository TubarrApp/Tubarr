// Package cfg provides configuration and command-line interface setup for Tubarr.
package cfg

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"tubarr/internal/domain/keys"
	"tubarr/internal/interfaces"
	"tubarr/internal/utils/benchmark"
	"tubarr/internal/utils/logging"
	"tubarr/internal/validation"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	benchmarking bool
	benchFiles   *benchmark.BenchFiles
	err          error
)

var rootCmd = &cobra.Command{
	Use:   "tubarr",
	Short: "Tubarr is a video downloading and metatagging tool.",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if err := validation.ValidateViperFlags(); err != nil {
			return
		}
		if viper.IsSet(keys.Benchmarking) {
			if benchFiles, err = benchmark.SetupBenchmarking(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return
			}
			benchmarking = true
		}

		// Setup channel flags from config file
		if viper.IsSet(keys.ChannelConfigFile) {
			configFile := viper.GetString(keys.ChannelConfigFile)

			cInfo, err := os.Stat(configFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed check for config file path: %v", err)
				fmt.Println()
				os.Exit(1)
			} else if cInfo.IsDir() {
				fmt.Fprintf(os.Stderr, "config file entered is a directory, should be a file")
				fmt.Println()
				os.Exit(1)
			}

			if configFile != "" {
				// load and normalize keys from any Viper-supported config file
				if err := loadConfigFile(configFile); err != nil {
					fmt.Fprintf(os.Stderr, "failed loading config file: %v\n", err)
					os.Exit(1)
				}
			}
		}
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Lookup("help").Changed {
			return nil
		}
		if len(args) == 0 {
			viper.Set(keys.CheckChannels, true)
		}
		viper.Set("execute", true)
		return nil
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if benchmarking {
			if benchFiles == nil {
				logging.E("Null benchFiles")
				return
			}
			benchmark.CloseBenchFiles(benchFiles, fmt.Sprintf("Benchmark ended at %v", time.Now().Format(time.RFC1123Z)), nil)
		}
	},
}

// InitCommands initializes all commands and their flags.
func InitCommands(ctx context.Context, s interfaces.Store) error {

	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("_", "-")) // Convert "video_directory" to "video-directory"

	if err := initProgramFlags(rootCmd); err != nil {
		return err
	}
	if err := initFileTransformers(rootCmd); err != nil {
		return err
	}
	if err := initMetaTransformers(rootCmd); err != nil {
		return err
	}
	if err := initVideoTransformers(rootCmd); err != nil {
		return err
	}

	rootCmd.AddCommand(InitChannelCmds(ctx, s))
	rootCmd.AddCommand(InitVideoCmds(s))

	return nil
}

// Execute adds all child commands to the root command and sets flags appropriately
func Execute() error {
	return rootCmd.Execute()
}
