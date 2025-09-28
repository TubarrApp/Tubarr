package cfgflags

import (
	"tubarr/internal/domain/keys"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// InitAllFileTransformers initializes user flag settings for transformations applying to all files.
func InitAllFileTransformers(rootCmd *cobra.Command) error {

	// Prefix file with metafield
	rootCmd.PersistentFlags().StringSlice(keys.MFilenamePfx, nil, "Adds a specified metatag's value onto the start of the filename")
	if err := viper.BindPFlag(keys.MFilenamePfx, rootCmd.PersistentFlags().Lookup(keys.MFilenamePfx)); err != nil {
		return err
	}

	// Prefix files with date tag
	rootCmd.PersistentFlags().String(keys.MFilenameDateTag, "", "Looks for dates in metadata to prefix the video with. (date:format [e.g. Ymd for yyyy-mm-dd])")
	if err := viper.BindPFlag(keys.MFilenameDateTag, rootCmd.PersistentFlags().Lookup(keys.MFilenameDateTag)); err != nil {
		return err
	}

	// Rename convention
	rootCmd.PersistentFlags().StringP(keys.MRenameStyle, "r", "skip", "Rename flag (spaces, underscores, fixes-only, or skip)")
	if err := viper.BindPFlag(keys.MRenameStyle, rootCmd.PersistentFlags().Lookup(keys.MRenameStyle)); err != nil {
		return err
	}

	// Replace filename suffix
	rootCmd.PersistentFlags().StringSlice(keys.MFilenameReplaceSuffix, nil, "Replaces a specified suffix on filenames. (suffix:replacement)")
	if err := viper.BindPFlag(keys.MFilenameReplaceSuffix, rootCmd.PersistentFlags().Lookup(keys.MFilenameReplaceSuffix)); err != nil {
		return err
	}

	// Output directory (can be external)
	rootCmd.PersistentFlags().StringP(keys.MoveOnComplete, "o", "", "Move files to given directory on program completion")
	if err := viper.BindPFlag(keys.MoveOnComplete, rootCmd.PersistentFlags().Lookup(keys.MoveOnComplete)); err != nil {
		return err
	}

	return nil
}

// SetFileDirFlags sets the primary video and JSON directories.
func SetFileDirFlags(cmd *cobra.Command, configFile, jsonDir, videoDir *string) {
	if configFile != nil {
		cmd.Flags().StringVar(configFile, keys.ChannelConfigFile, "", "This is where the channel config file is stored (do not use templating)")
	}
	if videoDir != nil {
		cmd.Flags().StringVar(videoDir, keys.VideoDir, "", "This is where videos will be saved (some {{}} templating commands available)")
	}
	if jsonDir != nil {
		cmd.Flags().StringVar(jsonDir, keys.JSONDir, "", "This is where JSON files will be saved (some {{}} templating commands available)")
	}
}

// InitVideoTransformers initializes user flag settings for transformation of video files.
func InitVideoTransformers(rootCmd *cobra.Command) error {

	// Output extension type
	rootCmd.PersistentFlags().String(keys.OutputFiletype, "", "File extension to output files as (mp4 works best for most media servers)")
	if err := viper.BindPFlag(keys.OutputFiletype, rootCmd.PersistentFlags().Lookup(keys.OutputFiletype)); err != nil {
		return err
	}
	return nil
}
