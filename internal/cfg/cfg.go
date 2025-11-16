// Package cfg provides configuration and command-line interface setup for Tubarr.
package cfg

import (
	"context"
	"fmt"
	"os"
	"strings"
	"tubarr/internal/cmd"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/domain/vars"
	"tubarr/internal/file"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/benchmark"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:   "tubarr",
	Short: "Tubarr is a video downloading and metatagging tool.",
	PersistentPreRun: func(_ *cobra.Command, _ []string) {
		if err := validation.ValidateViperFlags(); err != nil {
			return
		}

		// Setup benchmarking if flag is set
		if viper.GetBool(keys.Benchmarking) {
			var err error
			vars.BenchmarkFiles, err = benchmark.SetupBenchmarking(logger.Pl, paths.BenchmarkDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to setup benchmarking: %v\n", err)
				return
			}
		}

		// Setup channel flags from config file
		if viper.IsSet(keys.ConfigFile) {
			configFile := viper.GetString(keys.ConfigFile)
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
				v := viper.New()
				if err := file.LoadConfigFile(v, configFile); err != nil {
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
			viper.Set(keys.TerminalRunDefaultBehavior, true)
		}
		viper.Set("execute", true)
		return nil
	},
}

// InitCommands initializes all commands and their flags.
func InitCommands(ctx context.Context, s contracts.Store) error {
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("_", "-")) // Convert "video_directory" to "video-directory"

	// Web version vs. terminal version toggle
	rootCmd.PersistentFlags().Bool(keys.RunWebInterface, false, "Run Tubarr as a web interface")
	if err := viper.BindPFlag(keys.RunWebInterface, rootCmd.PersistentFlags().Lookup(keys.RunWebInterface)); err != nil {
		return err
	}
	if viper.IsSet(keys.RunWebInterface) {
		return nil
	}

	if err := cmd.InitProgramFlags(rootCmd); err != nil {
		return err
	}
	if err := cmd.InitFileTransformers(rootCmd); err != nil {
		return err
	}
	if err := cmd.InitMetaTransformers(rootCmd); err != nil {
		return err
	}
	if err := cmd.InitVideoTransformers(rootCmd); err != nil {
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
