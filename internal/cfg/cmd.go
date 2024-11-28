// Package cfg provides configuration and command-line interface setup for Tubarr.
package cfg

import (
	"fmt"
	"os"
	"time"
	"tubarr/internal/domain/keys"
	"tubarr/internal/models"
	"tubarr/internal/utils/benchmark"
	"tubarr/internal/utils/logging"

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
		if err := viperFlags(); err != nil {
			return
		}
		if viper.IsSet(keys.Benchmarking) {
			if benchFiles, err = benchmark.SetupBenchmarking(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return
			}
			benchmarking = true
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
				logging.E(0, "Null benchFiles")
				return
			}
			benchmark.CloseBenchFiles(benchFiles, fmt.Sprintf("Benchmark ended at %v", time.Now().Format(time.RFC1123Z)), nil)
		}
	},
}

// InitCommands initializes all commands and their flags.
func InitCommands(s models.Store) error {

	if err := initProgramFlags(); err != nil {
		return err
	}
	if err := initAllFileTransformers(); err != nil {
		return err
	}
	if err := initMetaTransformers(); err != nil {
		return err
	}
	if err := initVideoTransformers(); err != nil {
		return err
	}

	rootCmd.AddCommand(initChannelCmds(s))
	rootCmd.AddCommand(initVideoCmds(s))
	return nil
}

// Execute adds all child commands to the root command and sets flags appropriately
func Execute() error {
	return rootCmd.Execute()
}
