// Package cfg provides configuration and command-line interface setup for Tubarr.
package cfg

import (
	"tubarr/internal/domain/keys"
	"tubarr/internal/models"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:   "tubarr",
	Short: "Tubarr is a video and metatagging tool",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if err := viperFlags(); err != nil {
			return
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
