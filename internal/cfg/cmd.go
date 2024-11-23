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
		if err := verify(); err != nil {
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
func InitCommands(s models.Store) {

	initProgramFlags()
	initAllFileTransformers()
	initMetaTransformers()
	initVideoTransformers()

	rootCmd.AddCommand(initChannelCmds(s))
	rootCmd.AddCommand(initVideoCmds(s))
}

// Execute adds all child commands to the root command and sets flags appropriately
func Execute() error {

	return rootCmd.Execute()
}
