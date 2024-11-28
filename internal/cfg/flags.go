package cfg

import (
	"tubarr/internal/domain/keys"

	"github.com/spf13/viper"
)

// initAllFileTransformers initializes user flag settings for transformations applying to all files.
func initAllFileTransformers() error {

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
	rootCmd.PersistentFlags().StringSliceVar(&filenameReplaceSuffixInput, keys.InputFilenameReplaceSfx, nil, "Replaces a specified suffix on filenames. (suffix:replacement)")
	if err := viper.BindPFlag(keys.InputFilenameReplaceSfx, rootCmd.PersistentFlags().Lookup(keys.InputFilenameReplaceSfx)); err != nil {
		return err
	}

	// Output directory (can be external)
	rootCmd.PersistentFlags().StringP(keys.MoveOnComplete, "o", "", "Move files to given directory on program completion")
	if err := viper.BindPFlag(keys.MoveOnComplete, rootCmd.PersistentFlags().Lookup(keys.MoveOnComplete)); err != nil {
		return err
	}

	return nil
}

// initMetaTransformers initializes user flag settings for manipulation of metadata.
func initMetaTransformers() error {

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

// initVideoTransformers initializes user flag settings for transformation of video files.
func initVideoTransformers() error {

	// Output extension type
	rootCmd.PersistentFlags().String(keys.OutputFiletypeInput, "", "File extension to output files as (mp4 works best for most media servers)")
	if err := viper.BindPFlag(keys.OutputFiletypeInput, rootCmd.PersistentFlags().Lookup(keys.OutputFiletypeInput)); err != nil {
		return err
	}
	return nil
}

// initProgramFlags initializes user flag settings related to the core program. E.g. logging level.
func initProgramFlags() error {

	// Output benchmarking files
	rootCmd.PersistentFlags().Bool(keys.Benchmarking, false, "Benchmarks the program")
	if err := viper.BindPFlag(keys.Benchmarking, rootCmd.PersistentFlags().Lookup(keys.Benchmarking)); err != nil {
		return err
	}

	// Debug level
	rootCmd.PersistentFlags().Int(keys.DebugLevel, 0, "Debugging level (0 - 5)")
	if err := viper.BindPFlag(keys.DebugLevel, rootCmd.PersistentFlags().Lookup(keys.DebugLevel)); err != nil {
		return err
	}
	return nil
}
