package cfg

import (
	"fmt"
	"strconv"
	"strings"
	"tubarr/internal/domain/keys"
	"tubarr/internal/models"
	"tubarr/internal/parsing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	flagTypeString      = "string"
	flagTypeInt         = "int"
	flagTypeBool        = "bool"
	flagTypeFloat64     = "float64"
	flagTypeStringSlice = "stringSlice"
)

// resetCobraFlagsAndLoadViper loads in variables from config file(s).
func resetCobraFlagsAndLoadViper(cmd *cobra.Command, v *viper.Viper) error {
	// Apply defaults for all known flags
	var errOrNil error
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if !f.Changed {
			key := f.Name
			switch f.Value.Type() {
			case flagTypeString:
				if val, ok := parsing.GetConfigValue[string](v, key); ok {
					if err := f.Value.Set(val); err != nil {
						errOrNil = err
					} else {
						f.Changed = true
					}
				}
			case flagTypeInt:
				if val, ok := parsing.GetConfigValue[int](v, key); ok {
					if err := f.Value.Set(strconv.Itoa(val)); err != nil {
						errOrNil = err
					}
				}
			case flagTypeBool:
				if val, ok := parsing.GetConfigValue[bool](v, key); ok {
					if err := f.Value.Set(strconv.FormatBool(val)); err != nil {
						errOrNil = err
					} else {
						f.Changed = true
					}
				}
			case flagTypeFloat64:
				if val, ok := parsing.GetConfigValue[float64](v, key); ok {
					if err := f.Value.Set(fmt.Sprintf("%f", val)); err != nil {
						errOrNil = err
					} else {
						f.Changed = true
					}
				}
			case flagTypeStringSlice:
				if slice, ok := parsing.GetConfigValue[[]string](v, key); ok && len(slice) > 0 {
					// Try to type assert pflag.Value to pflag.SliceValue
					if sv, ok := f.Value.(pflag.SliceValue); ok {
						if err := sv.Replace(slice); err != nil {
							errOrNil = err
						} else {
							f.Changed = true
						}
					} else {
						// Fallback: Try join on comma for types that only implement Set(string)
						if err := f.Value.Set(strings.Join(slice, ",")); err != nil {
							errOrNil = err
						} else {
							f.Changed = true
						}
					}
				}
			}
		}
	})
	return errOrNil
}

// mergeFlagsIntoInput merges flag values into the pointer struct.
func mergeFlagsIntoInput(cmd *cobra.Command, f *models.ChannelFlagValues, in *models.ChannelInputPtrs) {
	if cmd.Flags().Changed(keys.Name) {
		in.Name = &f.Name
	}
	if cmd.Flags().Changed(keys.URL) {
		in.URLs = &f.URLs
	}
	if cmd.Flags().Changed(keys.ConfigFile) {
		in.ChannelConfigFile = &f.ChannelConfigFile
	}
	if cmd.Flags().Changed(keys.VideoDir) {
		in.VideoDir = &f.VideoDir
	}
	if cmd.Flags().Changed(keys.JSONDir) {
		in.JSONDir = &f.JSONDir
	}
	if cmd.Flags().Changed(keys.MOutputDir) {
		in.OutDir = &f.OutDir
	}
	if cmd.Flags().Changed(keys.TranscodeGPUDir) {
		in.GPUDir = &f.GPUDir
	}
	if cmd.Flags().Changed(keys.FilterOpsFile) {
		in.DLFilterFile = &f.DLFilterFile
	}
	if cmd.Flags().Changed(keys.MoveOpsFile) {
		in.MoveOpFile = &f.MoveOpFile
	}
	if cmd.Flags().Changed(keys.MMetaOpsFile) {
		in.MetaOpsFile = &f.MetaOpsFile
	}
	if cmd.Flags().Changed(keys.MFilteredMetaOpsFile) {
		in.FilteredMetaOpsFile = &f.FilteredMetaOpsFile
	}
	if cmd.Flags().Changed(keys.MFilenameOpsFile) {
		in.FilenameOpsFile = &f.FilenameOpsFile
	}
	if cmd.Flags().Changed(keys.MFilteredFilenameOpsFile) {
		in.FilteredFilenameOpsFile = &f.FilteredFilenameOpsFile
	}
	if cmd.Flags().Changed(keys.AuthUsername) {
		in.Username = &f.Username
	}
	if cmd.Flags().Changed(keys.AuthPassword) {
		in.Password = &f.Password
	}
	if cmd.Flags().Changed(keys.AuthURL) {
		in.LoginURL = &f.LoginURL
	}
	if cmd.Flags().Changed(keys.AuthDetails) {
		in.AuthDetails = &f.AuthDetails
	}
	if cmd.Flags().Changed(keys.NotifyPair) {
		in.Notification = &f.Notification
	}
	if cmd.Flags().Changed(keys.CookiesFromBrowser) {
		in.CookiesFromBrowser = &f.CookiesFromBrowser
	}
	if cmd.Flags().Changed(keys.ExternalDownloader) {
		in.ExternalDownloader = &f.ExternalDownloader
	}
	if cmd.Flags().Changed(keys.ExternalDownloaderArgs) {
		in.ExternalDownloaderArgs = &f.ExternalDownloaderArgs
	}
	if cmd.Flags().Changed(keys.MaxFilesize) {
		in.MaxFilesize = &f.MaxFilesize
	}
	if cmd.Flags().Changed(keys.YtdlpOutputExt) {
		in.YTDLPOutputExt = &f.YTDLPOutputExt
	}
	if cmd.Flags().Changed(keys.FromDate) {
		in.FromDate = &f.FromDate
	}
	if cmd.Flags().Changed(keys.ToDate) {
		in.ToDate = &f.ToDate
	}
	if cmd.Flags().Changed(keys.UseGlobalCookies) {
		in.UseGlobalCookies = &f.UseGlobalCookies
	}
	if cmd.Flags().Changed(keys.FilterOpsInput) {
		in.DLFilters = &f.DLFilters
	}
	if cmd.Flags().Changed(keys.MoveOps) {
		in.MoveOps = &f.MoveOps
	}
	if cmd.Flags().Changed(keys.MMetaOps) {
		in.MetaOps = &f.MetaOps
	}
	if cmd.Flags().Changed(keys.MFilenameOps) {
		in.FilenameOps = &f.FilenameOps
	}
	if cmd.Flags().Changed(keys.MFilteredMetaOps) {
		in.FilteredMetaOps = &f.FilteredMetaOps
	}
	if cmd.Flags().Changed(keys.MFilteredFilenameOps) {
		in.FilteredFilenameOps = &f.FilteredFilenameOps
	}
	if cmd.Flags().Changed(keys.MURLOutputDirs) {
		in.URLOutputDirs = &f.URLOutputDirs
	}
	if cmd.Flags().Changed(keys.MOutputExt) {
		in.MetarrExt = &f.MetarrExt
	}
	if cmd.Flags().Changed(keys.MRenameStyle) {
		in.RenameStyle = &f.RenameStyle
	}
	if cmd.Flags().Changed(keys.MMinFreeMem) {
		in.MinFreeMem = &f.MinFreeMem
	}
	if cmd.Flags().Changed(keys.TranscodeGPU) {
		in.TranscodeGPU = &f.TranscodeGPU
	}
	if cmd.Flags().Changed(keys.TranscodeQuality) {
		in.TranscodeQuality = &f.TranscodeQuality
	}
	if cmd.Flags().Changed(keys.TranscodeVideoFilter) {
		in.TranscodeVideoFilter = &f.TranscodeVideoFilter
	}
	if cmd.Flags().Changed(keys.TranscodeCodec) {
		in.VideoCodec = &f.VideoCodec
	}
	if cmd.Flags().Changed(keys.TranscodeAudioCodec) {
		in.AudioCodec = &f.AudioCodec
	}
	if cmd.Flags().Changed(keys.ExtraYTDLPVideoArgs) {
		in.ExtraYTDLPVideoArgs = &f.ExtraYTDLPVideoArgs
	}
	if cmd.Flags().Changed(keys.ExtraYTDLPMetaArgs) {
		in.ExtraYTDLPMetaArgs = &f.ExtraYTDLPMetaArgs
	}
	if cmd.Flags().Changed(keys.MExtraFFmpegArgs) {
		in.ExtraFFmpegArgs = &f.ExtraFFmpegArgs
	}
	if cmd.Flags().Changed(keys.CrawlFreq) {
		in.CrawlFreq = &f.CrawlFreq
	}
	if cmd.Flags().Changed(keys.ChanOrURLConcurrencyLimit) {
		in.Concurrency = &f.Concurrency
	}
	if cmd.Flags().Changed(keys.MConcurrency) {
		in.MetarrConcurrency = &f.MetarrConcurrency
	}
	if cmd.Flags().Changed(keys.DLRetries) {
		in.Retries = &f.Retries
	}
	if cmd.Flags().Changed(keys.MMaxCPU) {
		in.MaxCPU = &f.MaxCPU
	}
	if cmd.Flags().Changed(keys.Pause) {
		in.Pause = &f.Pause
	}
	if cmd.Flags().Changed(keys.IgnoreRun) {
		in.IgnoreRun = &f.IgnoreRun
	}
}
