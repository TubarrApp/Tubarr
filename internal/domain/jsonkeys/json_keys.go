// Package jsonkeys holds constants for JSON keys used in settings and metarr args.
package jsonkeys

// JSON setting keys.
const (
	SettingsCookiesFromBrowser     = "cookies_from_browser"
	SettingsExternalDownloader     = "external_downloader"
	SettingsExternalDownloaderArgs = "external_downloader_args"
	SettingsExtraYtdlpVideoArgs    = "extra_ytdlp_video_args"
	SettingsExtraYtdlpMetaArgs     = "extra_ytdlp_meta_args"
	SettingsFilterFile             = "filter_file"
	SettingsMoveOpsFile            = "move_ops_file"
	SettingsJSONDirectory          = "json_directory"
	SettingsVideoDirectory         = "video_directory"
	SettingsMaxFilesize            = "max_filesize"
	SettingsYtdlpOutputExt         = "ytdlp_output_ext"
	SettingsFromDate               = "from_date"
	SettingsToDate                 = "to_date"
	SettingsMaxConcurrency         = "max_concurrency"
	SettingsCrawlFreq              = "crawl_freq"
	SettingsDownloadRetries        = "download_retries"
	SettingsUseGlobalCookies       = "use_global_cookies"
	SettingsFilters                = "filters"
	SettingsMoveOps                = "move_ops"
)

// Metarr args keys.
const (
	MetarrOutputExt               = "metarr_output_ext"
	MetarrFilenameOpsFile         = "metarr_filename_ops_file"
	MetarrFilteredFilenameOpsFile = "filtered_filename_ops_file"
	MetarrMetaOpsFile             = "metarr_meta_ops_file"
	MetarrFilteredMetaOpsFile     = "filtered_meta_ops_file"
	MetarrExtraFFmpegArgs         = "metarr_extra_ffmpeg_args"
	MetarrRenameStyle             = "metarr_rename_style"
	MetarrMinFreeMem              = "metarr_min_free_mem"
	MetarrGPU                     = "metarr_gpu"
	MetarrGPUDirectory            = "metarr_gpu_directory"
	MetarrOutputDirectory         = "metarr_output_directory"
	MetarrTranscodeVideoFilter    = "metarr_transcode_video_filter"
	MetarrVideoCodecs             = "metarr_video_transcode_codecs"
	MetarrAudioCodecs             = "metarr_transcode_audio_codecs"
	MetarrTranscodeQuality        = "metarr_transcode_quality"
	MetarrConcurrency             = "metarr_concurrency"
	MetarrMaxCPU                  = "metarr_max_cpu_usage"
	MetarrFilenameOps             = "metarr_filename_ops"
	MetarrFilteredFilenameOps     = "filtered_filename_ops"
	MetarrFilteredMetaOps         = "filtered_meta_ops"
	MetarrMetaOps                 = "metarr_meta_ops"
)
