package cfgflags

import (
	"tubarr/internal/domain/keys"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// InitMetaTransformers initializes user flag settings for manipulation of metadata.
func InitMetaTransformers(rootCmd *cobra.Command) error {

	// Metadata transformations
	rootCmd.PersistentFlags().StringSlice(keys.MetaOps, nil, "Metadata operations (field:operation:value) - e.g. title:set:New Title, description:prefix:Draft-, tags:append:newtag")
	if err := viper.BindPFlag(keys.MetaOps, rootCmd.PersistentFlags().Lookup(keys.MetaOps)); err != nil {
		return err
	}

	// Prefix or append description fields with dates
	rootCmd.PersistentFlags().Bool(keys.MDescDatePfx, false, "Adds the date to the start of the description field.")
	if err := viper.BindPFlag(keys.MDescDatePfx, rootCmd.PersistentFlags().Lookup(keys.MDescDatePfx)); err != nil {
		return err
	}

	rootCmd.PersistentFlags().Bool(keys.MDescDateSfx, false, "Adds the date to the end of the description field.")
	if err := viper.BindPFlag(keys.MDescDateSfx, rootCmd.PersistentFlags().Lookup(keys.MDescDateSfx)); err != nil {
		return err
	}

	rootCmd.PersistentFlags().String(keys.MetaPurge, "", "Delete metadata files (e.g. .json, .nfo) after the video is successfully processed")
	if err := viper.BindPFlag(keys.MetaPurge, rootCmd.PersistentFlags().Lookup(keys.MetaPurge)); err != nil {
		return err
	}
	return nil
}
