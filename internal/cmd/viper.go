package cmd

import (
	"tubarr/internal/domain/keys"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Transformers
// InitFileTransformers initializes user flag settings for transformations applying to all files.
func InitFileTransformers(rootCmd *cobra.Command) error {
	// Prefix file with metafield
	rootCmd.PersistentFlags().StringSlice(keys.MFilenamePfx, nil, "Adds a specified metatag's value onto the start of the filename")
	if err := viper.BindPFlag(keys.MFilenamePfx, rootCmd.PersistentFlags().Lookup(keys.MFilenamePfx)); err != nil {
		return err
	}

	// Rename convention
	rootCmd.PersistentFlags().StringP(keys.MRenameStyle, "r", "skip", "Rename flag (spaces, underscores, fixes-only, or skip)")
	if err := viper.BindPFlag(keys.MRenameStyle, rootCmd.PersistentFlags().Lookup(keys.MRenameStyle)); err != nil {
		return err
	}

	// Output directory (can be external)
	rootCmd.PersistentFlags().StringP(keys.MoveOnComplete, "o", "", "Move files to given directory on program completion")
	if err := viper.BindPFlag(keys.MoveOnComplete, rootCmd.PersistentFlags().Lookup(keys.MoveOnComplete)); err != nil {
		return err
	}

	return nil
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

// InitMetaTransformers initializes user flag settings for manipulation of metadata.
func InitMetaTransformers(rootCmd *cobra.Command) error {
	// Metadata transformations
	rootCmd.PersistentFlags().StringSlice(keys.MMetaOps, nil, "Metadata operations (field:operation:value) - e.g. title:set:New Title, description:prefix:Draft-, tags:append:newtag")
	if err := viper.BindPFlag(keys.MMetaOps, rootCmd.PersistentFlags().Lookup(keys.MMetaOps)); err != nil {
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

	rootCmd.PersistentFlags().String(keys.MMetaPurge, "", "Delete metadata files (e.g. .json, .nfo) after the video is successfully processed")
	if err := viper.BindPFlag(keys.MMetaPurge, rootCmd.PersistentFlags().Lookup(keys.MMetaPurge)); err != nil {
		return err
	}
	return nil
}

// Program
// InitProgramFlags initializes user flag settings related to the core program. E.g. logging level.
func InitProgramFlags(rootCmd *cobra.Command) error {
	// Skip initial wait
	rootCmd.PersistentFlags().BoolP(keys.SkipInitialWait, "s", false, "Skip the wait period usually applied before a crawl (helps avoid bot detection)")
	if err := viper.BindPFlag(keys.SkipInitialWait, rootCmd.PersistentFlags().Lookup(keys.SkipInitialWait)); err != nil {
		return err
	}

	// Skip wait
	rootCmd.PersistentFlags().Bool(keys.SkipAllWaits, false, "Skip all wait periods usually applied before a crawl (helps avoid bot detection)")
	if err := viper.BindPFlag(keys.SkipAllWaits, rootCmd.PersistentFlags().Lookup(keys.SkipAllWaits)); err != nil {
		return err
	}

	// Output benchmarking files
	rootCmd.PersistentFlags().Bool(keys.Benchmarking, false, "Benchmarks the program")
	if err := viper.BindPFlag(keys.Benchmarking, rootCmd.PersistentFlags().Lookup(keys.Benchmarking)); err != nil {
		return err
	}

	// Global Concurrency
	rootCmd.PersistentFlags().StringP(keys.GlobalConcurrency, "c", "", "Concurrency for this instance of Tubarr")
	if err := viper.BindPFlag(keys.GlobalConcurrency, rootCmd.PersistentFlags().Lookup(keys.GlobalConcurrency)); err != nil {
		return err
	}

	// Cookies
	rootCmd.PersistentFlags().String(keys.CookiesFromBrowser, "", "Cookie source for web operations (e.g. 'Firefox')")
	if err := viper.BindPFlag(keys.CookiesFromBrowser, rootCmd.PersistentFlags().Lookup(keys.CookiesFromBrowser)); err != nil {
		return err
	}

	// Debug level
	rootCmd.PersistentFlags().Int(keys.DebugLevel, 0, "Debugging level (0 - 5)")
	if err := viper.BindPFlag(keys.DebugLevel, rootCmd.PersistentFlags().Lookup(keys.DebugLevel)); err != nil {
		return err
	}
	return nil
}
