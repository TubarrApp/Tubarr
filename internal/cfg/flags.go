package cfg

import (
	"tubarr/internal/domain/keys"

	"github.com/spf13/viper"
)

// initAllFileTransformers initializes user flag settings for transformations applying to all files
func initAllFileTransformers() {

	// Prefix file with metafield
	rootCmd.PersistentFlags().StringSlice(keys.MFilenamePfx, nil, "Adds a specified metatag's value onto the start of the filename")
	viper.BindPFlag(keys.MFilenamePfx, rootCmd.PersistentFlags().Lookup(keys.MFilenamePfx))

	// Prefix files with date tag
	rootCmd.PersistentFlags().String(keys.InputFileDatePfx, "", "Looks for dates in metadata to prefix the video with. (date:format [e.g. Ymd for yyyy-mm-dd])")
	viper.BindPFlag(keys.InputFileDatePfx, rootCmd.PersistentFlags().Lookup(keys.InputFileDatePfx))

	// Rename convention
	rootCmd.PersistentFlags().StringP(keys.RenameStyle, "r", "fixes-only", "Rename flag (spaces, underscores, fixes-only, or skip)")
	viper.BindPFlag(keys.RenameStyle, rootCmd.PersistentFlags().Lookup(keys.RenameStyle))

	// Replace filename suffix
	rootCmd.PersistentFlags().StringSliceVar(&filenameReplaceSuffixInput, keys.InputFilenameReplaceSfx, nil, "Replaces a specified suffix on filenames. (suffix:replacement)")
	viper.BindPFlag(keys.InputFilenameReplaceSfx, rootCmd.PersistentFlags().Lookup(keys.InputFilenameReplaceSfx))

	// Output directory (can be external)
	rootCmd.PersistentFlags().StringP(keys.MoveOnComplete, "o", "", "Move files to given directory on program completion")
	viper.BindPFlag(keys.MoveOnComplete, rootCmd.PersistentFlags().Lookup(keys.MoveOnComplete))
}

// initMetaTransformers initializes user flag settings for manipulation of metadata
func initMetaTransformers() {

	// Metadata transformations
	rootCmd.PersistentFlags().StringSlice(keys.MetaOps, nil, "Metadata operations (field:operation:value) - e.g. title:set:New Title, description:prefix:Draft-, tags:append:newtag")
	viper.BindPFlag(keys.MetaOps, rootCmd.PersistentFlags().Lookup(keys.MetaOps))

	// Prefix or append description fields with dates
	rootCmd.PersistentFlags().Bool(keys.MDescDatePfx, false, "Adds the date to the start of the description field.")
	viper.BindPFlag(keys.MDescDatePfx, rootCmd.PersistentFlags().Lookup(keys.MDescDatePfx))

	rootCmd.PersistentFlags().Bool(keys.MDescDateSfx, false, "Adds the date to the end of the description field.")
	viper.BindPFlag(keys.MDescDateSfx, rootCmd.PersistentFlags().Lookup(keys.MDescDateSfx))

	rootCmd.PersistentFlags().String(keys.MetaPurge, "", "Delete metadata files (e.g. .json, .nfo) after the video is successfully processed")
	viper.BindPFlag(keys.MetaPurge, rootCmd.PersistentFlags().Lookup(keys.MetaPurge))
}

// initVideoTransformers initializes user flag settings for transformation of video files
func initVideoTransformers() {

	// Output extension type
	rootCmd.PersistentFlags().String(keys.OutputFiletypeInput, "", "File extension to output files as (mp4 works best for most media servers)")
	viper.BindPFlag(keys.OutputFiletypeInput, rootCmd.PersistentFlags().Lookup(keys.OutputFiletypeInput))
}
