package server

import (
	"strings"
	"tubarr/internal/models"
)

// metarrArgsJSONMap returns a map of Metarr JSON keys and their values.
func metarrArgsJSONMap(mArgs *models.MetarrArgs) map[string]any {
	metarrMap := make(map[string]any)
	if mArgs.OutputDir != "" {
		metarrMap["metarr_output_directory"] = mArgs.OutputDir
	}
	if mArgs.OutputExt != "" {
		metarrMap["metarr_output_ext"] = mArgs.OutputExt
	}
	if mArgs.RenameStyle != "" {
		metarrMap["metarr_rename_style"] = mArgs.RenameStyle
	}
	if mArgs.Concurrency > 0 {
		metarrMap["metarr_concurrency"] = mArgs.Concurrency
	}
	if mArgs.MaxCPU > 0 {
		metarrMap["metarr_max_cpu_usage"] = mArgs.MaxCPU
	}
	if mArgs.MinFreeMem != "" {
		metarrMap["metarr_min_free_mem"] = mArgs.MinFreeMem
	}
	if mArgs.UseGPU != "" {
		metarrMap["metarr_gpu"] = mArgs.UseGPU
	}
	if len(mArgs.TranscodeVideoCodecs) != 0 {
		metarrMap["metarr_video_transcode_codecs"] = mArgs.TranscodeVideoCodecs
	}
	if len(mArgs.TranscodeAudioCodecs) != 0 {
		metarrMap["metarr_transcode_audio_codecs"] = mArgs.TranscodeAudioCodecs
	}
	if mArgs.TranscodeQuality != "" {
		metarrMap["metarr_transcode_quality"] = mArgs.TranscodeQuality
	}
	if mArgs.TranscodeVideoFilter != "" {
		metarrMap["metarr_transcode_video_filter"] = mArgs.TranscodeVideoFilter
	}
	if mArgs.ExtraFFmpegArgs != "" {
		metarrMap["metarr_extra_ffmpeg_args"] = mArgs.ExtraFFmpegArgs
	}
	if len(mArgs.MetaOps) > 0 {
		metarrMap["metarr_meta_ops"] = strings.Join(models.MetaOpsArrayToSlice(mArgs.MetaOps), "\n")
	}
	if len(mArgs.FilenameOps) > 0 {
		metarrMap["metarr_filename_ops"] = strings.Join(models.FilenameOpsArrayToSlice(mArgs.FilenameOps), "\n")
	}
	if len(mArgs.FilteredMetaOps) > 0 {
		filteredMetaStrings := make([]string, 0)
		for i := range mArgs.FilteredMetaOps {
			filteredMetaStrings = append(filteredMetaStrings, models.FilteredMetaOpsToSlice(mArgs.FilteredMetaOps[i])...)
		}
		metarrMap["filtered_meta_ops"] = strings.Join(filteredMetaStrings, "\n")
	}
	if len(mArgs.FilteredFilenameOps) > 0 {
		filteredFilenameStrings := make([]string, 0)
		for i := range mArgs.FilteredFilenameOps {
			filteredFilenameStrings = append(filteredFilenameStrings, models.FilteredFilenameOpsToSlice(mArgs.FilteredFilenameOps[i])...)
		}
		metarrMap["filtered_filename_ops"] = strings.Join(filteredFilenameStrings, "\n")
	}
	return metarrMap
}

// settingsJSONMap returns a map of settings JSON keys and their values.
func settingsJSONMap(settings *models.Settings) map[string]any {
	settingsMap := make(map[string]any)
	if settings.VideoDir != "" {
		settingsMap["video_directory"] = settings.VideoDir
	}
	if settings.JSONDir != "" {
		settingsMap["json_directory"] = settings.JSONDir
	}
	if settings.Concurrency > 0 {
		settingsMap["max_concurrency"] = settings.Concurrency
	}
	if settings.CrawlFreq > 0 {
		settingsMap["crawl_freq"] = settings.CrawlFreq
	}
	if settings.Retries > 0 {
		settingsMap["download_retries"] = settings.Retries
	}
	if settings.MaxFilesize != "" {
		settingsMap["max_filesize"] = settings.MaxFilesize
	}
	if settings.FromDate != "" {
		settingsMap["from_date"] = settings.FromDate
	}
	if settings.ToDate != "" {
		settingsMap["to_date"] = settings.ToDate
	}
	if settings.ChannelConfigFile != "" {
		settingsMap["channel_config_file"] = settings.ChannelConfigFile
	}
	if settings.FilterFile != "" {
		settingsMap["filter_file"] = settings.FilterFile
	}
	if len(settings.Filters) > 0 {
		settingsMap["filters"] = strings.Join(models.FiltersArrayToSlice(settings.Filters), "\n")
	}
	if settings.YtdlpOutputExt != "" {
		settingsMap["ytdlp_output_ext"] = settings.YtdlpOutputExt
	}
	if settings.CookiesFromBrowser != "" {
		settingsMap["cookies_from_browser"] = settings.CookiesFromBrowser
	}
	if settings.ExternalDownloader != "" {
		settingsMap["external_downloader"] = settings.ExternalDownloader
	}
	if settings.ExternalDownloaderArgs != "" {
		settingsMap["external_downloader_args"] = settings.ExternalDownloaderArgs
	}
	if settings.ExtraYTDLPVideoArgs != "" {
		settingsMap["extra_ytdlp_video_args"] = settings.ExtraYTDLPVideoArgs
	}
	if settings.ExtraYTDLPMetaArgs != "" {
		settingsMap["extra_ytdlp_meta_args"] = settings.ExtraYTDLPMetaArgs
	}
	if settings.MoveOpFile != "" {
		settingsMap["move_ops_file"] = settings.MoveOpFile
	}
	if len(settings.MoveOps) > 0 {
		moveOpStrings := make([]string, len(settings.MoveOps))
		for i := range settings.MoveOps {
			moveOpStrings[i] = models.MetaFilterMoveOpsToSlice(settings.MoveOps[i])
		}
		settingsMap["move_ops"] = strings.Join(moveOpStrings, "\n")
	}
	if settings.UseGlobalCookies {
		settingsMap["use_global_cookies"] = true
	}
	if settings.Paused {
		settingsMap["paused"] = true
	}
	return settingsMap
}
