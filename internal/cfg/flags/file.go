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
	rootCmd.PersistentFlags().String(keys.InputFileDatePfx, "", "Looks for dates in metadata to prefix the video with. (date:format [e.g. Ymd for yyyy-mm-dd])")
	if err := viper.BindPFlag(keys.InputFileDatePfx, rootCmd.PersistentFlags().Lookup(keys.InputFileDatePfx)); err != nil {
		return err
	}

	// Rename convention
	rootCmd.PersistentFlags().StringP(keys.RenameStyle, "r", "fixes-only", "Rename flag (spaces, underscores, fixes-only, or skip)")
	if err := viper.BindPFlag(keys.RenameStyle, rootCmd.PersistentFlags().Lookup(keys.RenameStyle)); err != nil {
		return err
	}

	// Replace filename suffix
	rootCmd.PersistentFlags().StringSlice(keys.FilenameReplaceSuffix, nil, "Replaces a specified suffix on filenames. (suffix:replacement)")
	if err := viper.BindPFlag(keys.FilenameReplaceSuffix, rootCmd.PersistentFlags().Lookup(keys.FilenameReplaceSuffix)); err != nil {
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
func SetFileDirFlags(cmd *cobra.Command, jsonDir, videoDir *string) {
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
