package cfg

import (
	"fmt"
	"tubarr/internal/domain/keys"
	"tubarr/internal/interfaces"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:   "tubarr",
	Short: "Tubarr is a video and metatagging tool",
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Lookup("help").Changed {
			return nil // Stop further execution if help is invoked
		}
		if len(args) == 0 {
			viper.Set(keys.CheckChannels, true)
		}
		viper.Set("execute", true)
		return nil
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {

	initAllFileTransformers()
	initMetaTransformers()
	initVideoTransformers()

	verify()

	return rootCmd.Execute()
}

// InitCommands initializes all commands and their flags
func InitCommands(s interfaces.Store) {

	// Add channel commands as a subcommand of root
	rootCmd.AddCommand(initChannelCmds(s))
	rootCmd.AddCommand(initVideoCmds(s))

	// Set up any root-level flags here if needed
	rootCmd.PersistentFlags().Int("debug", 0, "Debug level (0-5)")
	viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug"))
}

// verify verifies that the user input flags are valid
func verify() error {
	if viper.IsSet(keys.OutputFiletype) {
		ext := viper.GetString(keys.OutputFiletype)
		if !verifyOutputFiletype(ext) {
			return fmt.Errorf("invalid output filetype '%s'", ext)
		}
	}

	if viper.IsSet(keys.MetaPurge) {
		purge := viper.GetString(keys.MetaPurge)
		if !verifyPurgeMetafiles(purge) {
			return fmt.Errorf("invalid meta purge type '%s'", purge)
		}
	}

	verifyConcurrencyLimit()
	return nil
}
