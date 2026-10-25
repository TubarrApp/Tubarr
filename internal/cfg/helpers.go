package cfg

import (
	"fmt"
	"strconv"
	"tubarr/internal/cmd"
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
	flagTypeStringArray = "stringArray"
)

// resetCobraFlagsAndLoadViper sets each flag not given on the command line from its key in the config file, if set.
func resetCobraFlagsAndLoadViper(cmd *cobra.Command, v *viper.Viper) error {
	var errOrNil error
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Changed {
			return
		}
		set := func(err error) {
			if err != nil {
				errOrNil = err
				return
			}
			f.Changed = true
		}
		switch f.Value.Type() {
		case flagTypeString:
			if val, ok := parsing.GetConfigValue[string](v, f.Name); ok {
				set(f.Value.Set(val))
			}
		case flagTypeInt:
			if val, ok := parsing.GetConfigValue[int](v, f.Name); ok {
				set(f.Value.Set(strconv.Itoa(val)))
			}
		case flagTypeBool:
			if val, ok := parsing.GetConfigValue[bool](v, f.Name); ok {
				set(f.Value.Set(strconv.FormatBool(val)))
			}
		case flagTypeFloat64:
			if val, ok := parsing.GetConfigValue[float64](v, f.Name); ok {
				set(f.Value.Set(fmt.Sprintf("%f", val)))
			}
		case flagTypeStringSlice, flagTypeStringArray: // pflag's slice values all implement SliceValue.
			if slice, ok := parsing.GetConfigValue[[]string](v, f.Name); ok && len(slice) > 0 {
				set(f.Value.(pflag.SliceValue).Replace(slice))
			}
		}
	})
	return errOrNil
}

// setChannelSettingsFlags registers the flags for a channel's or URL's settings (bar its directories), read into f.
func setChannelSettingsFlags(c *cobra.Command, f *models.ChannelFlagValues) {
	cmd.SetProgramRelatedFlags(c, &f.DownloadConcurrency, &f.CrawlFreq, &f.ExternalDownloaderArgs, &f.ExternalDownloader,
		&f.MoveOpFile, &f.MoveOps, &f.Pause)

	cmd.SetDownloadFlags(c, &f.Retries, &f.UseGlobalCookies, &f.YTDLPOutputExt, &f.FromDate, &f.ToDate,
		&f.CookiesFromBrowser, &f.MaxFilesize, &f.DLFilterFile, &f.DLFilters,
		&f.YTDLPPreferredVideoCodecs, &f.YTDLPPreferredAudioCodecs)

	cmd.SetMetarrFlags(c, &f.MaxCPU, &f.MetarrConcurrency, &f.MetarrExt, &f.ExtraFFmpegArgs, &f.MinFreeMem,
		&f.OutDir, &f.MetaPurge, &f.RenameStyle, &f.MetaOpsFile, &f.FilteredMetaOpsFile, &f.FilenameOpsFile,
		&f.FilteredFilenameOpsFile, &f.FilenameOps, &f.FilteredFilenameOps, &f.MetaOps, &f.FilteredMetaOps)

	cmd.SetTranscodeFlags(c, &f.TranscodeGPU, &f.TranscodeGPUNode, &f.TranscodeQuality,
		&f.TranscodeVideoCodec, &f.TranscodeAudioCodec)

	cmd.SetCustomYDLPArgFlags(c, &f.ExtraYTDLPVideoArgs, &f.ExtraYTDLPMetaArgs, &f.ExtraYTDLPCrawlArgs)

	c.Flags().BoolVar(&f.AutoDownload, keys.AutoDownload, false, "Download new videos automatically (otherwise they wait in Available, see 'list-available' or the web UI)")
}

// mergeFlagsIntoInput merges flag values into the pointer struct.
func mergeFlagsIntoInput(cmd *cobra.Command, f *models.ChannelFlagValues, in *models.ChannelInput) {
	if cmd.Flags().Changed(keys.Name) {
		in.Name = &f.Name
	}
	if cmd.Flags().Changed(keys.URL) {
		in.URLs = &f.URLs
	}
	if cmd.Flags().Changed(keys.ChannelConfigFile) {
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
	if cmd.Flags().Changed(keys.Notify) {
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
	if cmd.Flags().Changed(keys.AutoDownload) {
		in.AutoDownload = &f.AutoDownload
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
	if cmd.Flags().Changed(keys.MOutputExt) {
		in.MetarrExt = &f.MetarrExt
	}
	if cmd.Flags().Changed(keys.MRenameStyle) {
		in.RenameStyle = &f.RenameStyle
	}
	if cmd.Flags().Changed(keys.MMinFreeMem) {
		in.MinFreeMem = &f.MinFreeMem
	}
	if cmd.Flags().Changed(keys.MMetaPurge) {
		in.MetaPurge = &f.MetaPurge
	}
	if cmd.Flags().Changed(keys.TranscodeGPU) {
		in.TranscodeGPU = &f.TranscodeGPU
	}
	if cmd.Flags().Changed(keys.TranscodeGPUNode) {
		in.TranscodeGPUNode = &f.TranscodeGPUNode
	}
	if cmd.Flags().Changed(keys.YTDLPPreferredVideoCodecs) {
		in.YTDLPPreferredVideoCodecs = &f.YTDLPPreferredVideoCodecs
	}
	if cmd.Flags().Changed(keys.YTDLPPreferredAudioCodecs) {
		in.YTDLPPreferredAudioCodecs = &f.YTDLPPreferredAudioCodecs
	}
	if cmd.Flags().Changed(keys.TranscodeQuality) {
		in.TranscodeQuality = &f.TranscodeQuality
	}
	if cmd.Flags().Changed(keys.TranscodeCodec) {
		in.TranscodeVideoCodec = &f.TranscodeVideoCodec
	}
	if cmd.Flags().Changed(keys.TranscodeAudioCodec) {
		in.TranscodeAudioCodec = &f.TranscodeAudioCodec
	}
	if cmd.Flags().Changed(keys.ExtraYTDLPVideoArgs) {
		in.ExtraYTDLPVideoArgs = &f.ExtraYTDLPVideoArgs
	}
	if cmd.Flags().Changed(keys.ExtraYTDLPMetaArgs) {
		in.ExtraYTDLPMetaArgs = &f.ExtraYTDLPMetaArgs
	}
	if cmd.Flags().Changed(keys.ExtraYTDLPCrawlArgs) {
		in.ExtraYTDLPCrawlArgs = &f.ExtraYTDLPCrawlArgs
	}
	if cmd.Flags().Changed(keys.MExtraFFmpegArgs) {
		in.ExtraFFmpegArgs = &f.ExtraFFmpegArgs
	}
	if cmd.Flags().Changed(keys.CrawlFreq) {
		in.CrawlFreq = &f.CrawlFreq
	}
	if cmd.Flags().Changed(keys.DownloadConcurrency) {
		in.DownloadConcurrency = &f.DownloadConcurrency
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
