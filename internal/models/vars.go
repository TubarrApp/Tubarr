package models

// ChannelInput is a channel as given in a config file or flags, its viper tags naming each key. A nil field wasn't given.
type ChannelInput struct {
	// Channel identifiers.
	Name              *string   `viper:"channel-name"`
	URLs              *[]string `viper:"channel-urls"`
	ChannelConfigFile *string   `viper:"channel-config-file"`
	Profile           *string   `viper:"profile"`

	// Authentication details.
	Username    *string   `viper:"auth-username"`
	Password    *string   `viper:"auth-password"`
	LoginURL    *string   `viper:"auth-url"`
	AuthDetails *[]string `viper:"auth-details"`

	// Notification details.
	Notification *[]string `viper:"notify"`

	// Run an ignore crawl once added.
	IgnoreRun *bool `viper:"ignore-run"`

	// Per-URL settings overrides (no viper tag - handled manually).
	URLSettings map[string]*URLSettingsOverride

	// The channel's own settings.
	SettingsInput
	MetarrArgsInput
}

// SettingsInput is Settings as given, its viper tags naming each key. A nil field wasn't given.
type SettingsInput struct {
	// Directory paths.
	VideoDir *string `viper:"video-directory"`
	JSONDir  *string `viper:"json-directory"`

	// Download settings.
	CookiesFromBrowser        *string   `viper:"cookies-from-browser"`
	ExternalDownloader        *string   `viper:"external-downloader"`
	ExternalDownloaderArgs    *string   `viper:"external-downloader-args"`
	MaxFilesize               *string   `viper:"max-filesize"`
	YTDLPOutputExt            *string   `viper:"ytdlp-output-extension"`
	FromDate                  *string   `viper:"from-date"`
	ToDate                    *string   `viper:"to-date"`
	YTDLPPreferredVideoCodecs *[]string `viper:"ytdlp-preferred-video-codecs"`
	YTDLPPreferredAudioCodecs *[]string `viper:"ytdlp-preferred-audio-codecs"`
	UseGlobalCookies          *bool     `viper:"use-global-cookies"`
	AutoDownload              *bool     `viper:"auto-download"`

	// Filter and move operations, and their files.
	DLFilters    *[]string `viper:"filter-ops"`
	DLFilterFile *string   `viper:"filter-ops-file"`
	MoveOps      *[]string `viper:"move-ops"`
	MoveOpFile   *string   `viper:"move-ops-file"`

	// Extra arguments.
	ExtraYTDLPVideoArgs *string `viper:"extra-ytdlp-video-args"`
	ExtraYTDLPMetaArgs  *string `viper:"extra-ytdlp-meta-args"`
	ExtraYTDLPCrawlArgs *string `viper:"extra-ytdlp-crawl-args"`

	// Concurrency and performance settings.
	CrawlFreq           *int `viper:"crawl-freq"`
	DownloadConcurrency *int `viper:"download-concurrency"`
	Retries             *int `viper:"dl-retries"`

	// Pause toggle.
	Pause *bool `viper:"pause-toggle"`
}

// MetarrArgsInput is MetarrArgs as given, its viper tags naming each key. A nil field wasn't given.
type MetarrArgsInput struct {
	// Output settings.
	OutDir             *string `viper:"metarr-output-directory"`
	MetaPurge          *string `viper:"metarr-purge-metafile"`
	MetarrExt          *string `viper:"metarr-output-ext"`
	RenameStyle        *string `viper:"metarr-rename-style"`
	PurgeOriginalVideo *bool   `viper:"metarr-purge-original-video"`

	// Op files.
	MetaOpsFile             *string `viper:"metarr-meta-ops-file"`
	FilteredMetaOpsFile     *string `viper:"metarr-filtered-meta-ops-file"`
	FilenameOpsFile         *string `viper:"metarr-filename-ops-file"`
	FilteredFilenameOpsFile *string `viper:"metarr-filtered-filename-ops-file"`

	// Ops.
	MetaOps             *[]string `viper:"metarr-meta-ops"`
	FilenameOps         *[]string `viper:"metarr-filename-ops"`
	FilteredMetaOps     *[]string `viper:"metarr-filtered-meta-ops"`
	FilteredFilenameOps *[]string `viper:"metarr-filtered-filename-ops"`

	// Transcoding.
	TranscodeGPU        *string   `viper:"metarr-transcode-gpu"`
	TranscodeGPUNode    *string   `viper:"metarr-transcode-gpu-node"`
	TranscodeQuality    *string   `viper:"metarr-transcode-quality"`
	TranscodeVideoCodec *[]string `viper:"metarr-transcode-video-codecs"`
	TranscodeAudioCodec *[]string `viper:"metarr-transcode-audio-codecs"`

	// Resources and limits.
	MinFreeMem        *string  `viper:"metarr-min-free-mem"`
	MaxCPU            *float64 `viper:"metarr-max-cpu"`
	MetarrConcurrency *int     `viper:"metarr-concurrency"`

	// Miscellaneous.
	ExtraFFmpegArgs *string `viper:"metarr-extra-ffmpeg-args"`
}

// URLSettingsOverride contains per-URL setting overrides.
type URLSettingsOverride struct {
	Name     *string
	Profile  *string
	Settings *SettingsInput
	Metarr   *MetarrArgsInput
}
